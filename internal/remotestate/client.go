package remotestate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultReadTimeout = 30 * time.Second
	logUploadTimeout   = 60 * time.Second
	connectTimeout     = 5 * time.Second
	maxRetryAttempts   = 3
	retryBaseDelay     = 2 * time.Second
	retryMaxDelay      = 10 * time.Second

	// ContractVersion is the wire-contract major this client speaks. It is
	// sent as the Orun-Contract-Version header on every request; the server
	// rejects unknown majors with 409 contract_version_unsupported (see the
	// vendored state-api-contract.md §0).
	ContractVersion = "1"

	// contractVersionHeader is the request header carrying ContractVersion.
	contractVersionHeader = "Orun-Contract-Version"

	// defaultScopeSegment is the single-tenant scope used by the OSS
	// `orun backend` server. The contract serves the same scoped paths with a
	// fixed _local/_local org/project, so one client codepath drives both.
	defaultScopeSegment = "_local"

	// contractVersionUnsupportedCode is the platform error code returned when
	// the server cannot satisfy the requested contract major.
	contractVersionUnsupportedCode = "contract_version_unsupported"
)

// Scope is the org/project tenancy scope for state API calls. Both fields
// default to the OSS single-tenant "_local" segment when empty, so existing
// single-tenant flows keep working without an org/project.
type Scope struct {
	OrgID     string
	ProjectID string
}

// orgSegment returns the org path segment, defaulting to the single-tenant
// scope when unset.
func (s Scope) orgSegment() string {
	if v := strings.TrimSpace(s.OrgID); v != "" {
		return v
	}
	return defaultScopeSegment
}

// projectSegment returns the project path segment, defaulting to the
// single-tenant scope when unset.
func (s Scope) projectSegment() string {
	if v := strings.TrimSpace(s.ProjectID); v != "" {
		return v
	}
	return defaultScopeSegment
}

// APIError represents a decoded backend error envelope. It accommodates both
// the platform's nested envelope ({ error: { code, message, details?,
// requestId } }) and the OSS backend's flat envelope ({ error, code }).
type APIError struct {
	Message   string `json:"-"`
	Code      string `json:"-"`
	RequestID string `json:"-"`
	// Details carries optional structured detail from the platform envelope.
	Details json.RawMessage `json:"-"`
	// Status is the HTTP status code, when known.
	Status int `json:"-"`
}

func (e *APIError) Error() string {
	parts := e.Message
	if e.Code != "" {
		parts = fmt.Sprintf("%s (code: %s)", parts, e.Code)
	}
	if e.RequestID != "" {
		parts = fmt.Sprintf("%s [requestId: %s]", parts, e.RequestID)
	}
	return parts
}

// IsAuth reports whether the error is an authentication/authorization failure.
// It recognises both the legacy uppercase OSS codes and the platform's
// lowercase codes.
func (e *APIError) IsAuth() bool {
	switch strings.ToLower(e.Code) {
	case "unauthorized", "forbidden":
		return true
	default:
		return false
	}
}

// IsContractVersionUnsupported reports whether the server rejected this
// client's contract major (409 contract_version_unsupported per the contract).
func (e *APIError) IsContractVersionUnsupported() bool {
	return strings.EqualFold(e.Code, contractVersionUnsupportedCode)
}

// IsLeaseLost reports whether the server rejected a heartbeat/update/log-append
// because the runner's lease lapsed or was reassigned (409 lease_lost). The
// runner treats this as "stop work on this job — someone else owns it now".
func (e *APIError) IsLeaseLost() bool {
	return strings.EqualFold(e.Code, "lease_lost")
}

// Client is an HTTP client for the orun state API (Orun Cloud and the OSS
// single-tenant backend speak the same contract).
type Client struct {
	baseURL    string
	scope      Scope
	tokenSrc   TokenSource
	userAgent  string
	httpClient *http.Client
	// logClient uses a longer timeout for log uploads.
	logClient *http.Client
}

// NewClient creates a Client for baseURL using the given TokenSource and CLI
// version, with the default single-tenant (_local/_local) scope.
func NewClient(baseURL, version string, tokenSrc TokenSource) *Client {
	return NewClientWithScope(baseURL, version, tokenSrc, Scope{})
}

// NewClientWithScope creates a Client bound to a specific org/project scope.
// An empty Scope (or empty fields) defaults to the OSS single-tenant
// _local/_local scope.
func NewClientWithScope(baseURL, version string, tokenSrc TokenSource, scope Scope) *Client {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   connectTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}
	return &Client{
		baseURL:   strings.TrimRight(baseURL, "/"),
		scope:     scope,
		tokenSrc:  tokenSrc,
		userAgent: "orun-cli/" + version,
		httpClient: &http.Client{
			Timeout:   defaultReadTimeout,
			Transport: transport,
		},
		logClient: &http.Client{
			Timeout:   logUploadTimeout,
			Transport: transport,
		},
	}
}

// Scope returns the org/project scope this client is bound to.
func (c *Client) Scope() Scope { return c.scope }

// statePath builds a scoped state path. The suffix is appended verbatim after
// the contract base path /v1/organizations/{orgId}/projects/{projectId}/state.
// suffix must begin with "/" (e.g. "/runs/abc").
func (c *Client) statePath(suffix string) string {
	return "/v1/organizations/" + urlSegment(c.scope.orgSegment()) +
		"/projects/" + urlSegment(c.scope.projectSegment()) +
		"/state" + suffix
}

// ── API request/response types (v1 contract §2) ────────────────────────────────

// GitProvenance is the git context captured at run-create time.
type GitProvenance struct {
	Commit string `json:"commit"`
	Ref    string `json:"ref"`
	Dirty  bool   `json:"dirty"`
}

// PlanJobInput is one node of the plan DAG sent in the create body. The plan
// blob itself lives in the object plane (referenced by planDigest); these light
// nodes let the platform persist run_jobs.
type PlanJobInput struct {
	JobID     string   `json:"jobId"`
	Component string   `json:"component,omitempty"`
	Deps      []string `json:"deps"`
}

// CreateRunRequest is the body for POST …/state/runs (contract §2.1). The plan
// is referenced by digest (the blob is uploaded to the object plane first), not
// sent inline.
type CreateRunRequest struct {
	RunID       string            `json:"runId"`
	PlanDigest  string            `json:"planDigest"`
	Source      string            `json:"source"` // "cli" | "ci"
	Environment string            `json:"environment,omitempty"`
	Git         GitProvenance     `json:"git"`
	Labels      map[string]string `json:"labels,omitempty"`
	Jobs        []PlanJobInput    `json:"jobs,omitempty"`
}

// ActorRef is the projected actor that created a run/job.
type ActorRef struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	DisplayName string `json:"displayName,omitempty"`
}

// RunJobCounts are the per-status job tallies on a run projection.
type RunJobCounts struct {
	Queued    int `json:"queued"`
	Running   int `json:"running"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}

// Run is the safe run projection (contract §2.1).
type Run struct {
	RunID       string        `json:"runId"`
	OrgID       string        `json:"orgId"`
	ProjectID   string        `json:"projectId"`
	Environment *string       `json:"environment"`
	Status      string        `json:"status"`
	PlanDigest  string        `json:"planDigest"`
	Source      string        `json:"source"`
	Git         GitProvenance `json:"git"`
	CreatedBy   ActorRef      `json:"createdBy"`
	CreatedAt   string        `json:"createdAt"`
	StartedAt   *string       `json:"startedAt"`
	FinishedAt  *string       `json:"finishedAt"`
	JobCounts   RunJobCounts  `json:"jobCounts"`
}

// RunJob is the safe projection of a job in a run's plan DAG (contract §2).
type RunJob struct {
	RunID          string   `json:"runId"`
	JobID          string   `json:"jobId"`
	OrgID          string   `json:"orgId"`
	ProjectID      string   `json:"projectId"`
	Component      *string  `json:"component"`
	Deps           []string `json:"deps"`
	Status         string   `json:"status"`
	RunnerID       *string  `json:"runnerId"`
	LeaseExpiresAt *string  `json:"leaseExpiresAt"`
	Attempt        int      `json:"attempt"`
	ErrorText      *string  `json:"errorText"`
	StartedAt      *string  `json:"startedAt"`
	FinishedAt     *string  `json:"finishedAt"`
}

// StateCursor is the opaque list-pagination cursor (createdAt|id).
type StateCursor struct {
	CreatedAt string `json:"createdAt"`
	ID        string `json:"id"`
}

// ── Response envelopes (the {data,meta} wrapper is unwrapped first) ─────────────

type runEnvelope struct {
	Run Run `json:"run"`
}

type listRunsResponse struct {
	Runs       []Run        `json:"runs"`
	NextCursor *StateCursor `json:"nextCursor"`
}

type listJobsResponse struct {
	Jobs []RunJob `json:"jobs"`
}

// ClaimJobRequest is the body for the claim endpoint.
type ClaimJobRequest struct {
	RunnerID string `json:"runnerId"`
}

// JobClaim is the structured claim outcome (contract §2.2). Exactly one runner
// wins; the loser learns why via Reason.
type JobClaim struct {
	Claimed                  bool   `json:"claimed"`
	LeaseExpiresAt           string `json:"leaseExpiresAt,omitempty"`
	Attempt                  int    `json:"attempt,omitempty"`
	LeaseSeconds             int    `json:"leaseSeconds,omitempty"`
	HeartbeatIntervalSeconds int    `json:"heartbeatIntervalSeconds,omitempty"`
	Reason                   string `json:"reason,omitempty"` // already_claimed | deps_not_ready | terminal
}

type claimJobResponse struct {
	Claim JobClaim `json:"claim"`
}

// HeartbeatRequest is the body for the heartbeat endpoint.
type HeartbeatRequest struct {
	RunnerID string `json:"runnerId"`
}

// HeartbeatInfo carries the extended lease and the server-supplied tunables; the
// client never hardcodes the lease/heartbeat intervals (contract §2.2).
type HeartbeatInfo struct {
	LeaseExpiresAt           string `json:"leaseExpiresAt"`
	LeaseSeconds             int    `json:"leaseSeconds,omitempty"`
	HeartbeatIntervalSeconds int    `json:"heartbeatIntervalSeconds,omitempty"`
}

// UpdateJobRequest is the body for the update endpoint.
type UpdateJobRequest struct {
	RunnerID  string `json:"runnerId"`
	Status    string `json:"status"` // "succeeded" | "failed"
	ErrorText string `json:"errorText,omitempty"`
}

// AppendLogRequest is the body for a log-chunk append.
type AppendLogRequest struct {
	RunnerID string `json:"runnerId"`
	Content  string `json:"content"`
}

type appendLogResponse struct {
	Seq int `json:"seq"`
}

// ReadLogResult is the assembled-read response with the live-tail cursor.
type ReadLogResult struct {
	Content  string `json:"content"`
	NextSeq  int    `json:"nextSeq"`
	Complete bool   `json:"complete"`
}

// ── API methods (v1 contract §2) ───────────────────────────────────────────────

// CreateRun calls POST …/state/runs. Idempotent on the client-minted run ULID:
// a replayed runId returns the existing run (200) rather than a conflict. The
// plan blob must already exist in the object plane (referenced by planDigest);
// the server returns 412 object_missing otherwise.
func (c *Client) CreateRun(ctx context.Context, req CreateRunRequest) (*Run, error) {
	var resp runEnvelope
	if err := c.doJSON(ctx, http.MethodPost, c.statePath("/runs"), req, &resp, true); err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}
	return &resp.Run, nil
}

// GetRun calls GET …/state/runs/{runID}.
func (c *Client) GetRun(ctx context.Context, runID string) (*Run, error) {
	var resp runEnvelope
	if err := c.doJSON(ctx, http.MethodGet, c.statePath("/runs/"+urlSegment(runID)), nil, &resp, true); err != nil {
		return nil, fmt.Errorf("get run: %w", err)
	}
	return &resp.Run, nil
}

// ListRunsOptions filters a run listing. Empty fields are omitted.
type ListRunsOptions struct {
	Environment string
	Status      string
	Cursor      string
}

// ListRuns calls GET …/state/runs with optional environment/status/cursor
// filters, returning the page of runs and the next-page cursor (empty when
// exhausted).
func (c *Client) ListRuns(ctx context.Context, opts ListRunsOptions) ([]Run, string, error) {
	q := url.Values{}
	if opts.Environment != "" {
		q.Set("environment", opts.Environment)
	}
	if opts.Status != "" {
		q.Set("status", opts.Status)
	}
	if opts.Cursor != "" {
		q.Set("cursor", opts.Cursor)
	}
	path := c.statePath("/runs")
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var resp listRunsResponse
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp, true); err != nil {
		return nil, "", fmt.Errorf("list runs: %w", err)
	}
	cursor := ""
	if resp.NextCursor != nil {
		cursor = resp.NextCursor.CreatedAt + "|" + resp.NextCursor.ID
	}
	return resp.Runs, cursor, nil
}

// ListJobs calls GET …/state/runs/{runID}/jobs.
func (c *Client) ListJobs(ctx context.Context, runID string) ([]RunJob, error) {
	var resp listJobsResponse
	if err := c.doJSON(ctx, http.MethodGet, c.statePath("/runs/"+urlSegment(runID)+"/jobs"), nil, &resp, true); err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	return resp.Jobs, nil
}

// ListRunnable calls GET …/state/runs/{runID}/runnable, the queued frontier
// (jobs whose deps are all succeeded). The server returns full job objects.
func (c *Client) ListRunnable(ctx context.Context, runID string) ([]RunJob, error) {
	var resp listJobsResponse
	if err := c.doJSON(ctx, http.MethodGet, c.statePath("/runs/"+urlSegment(runID)+"/runnable"), nil, &resp, true); err != nil {
		return nil, fmt.Errorf("get runnable: %w", err)
	}
	return resp.Jobs, nil
}

// ClaimJob calls POST …/state/runs/{runID}/jobs/{jobID}/claim and returns the
// structured claim outcome. Not retried on 5xx because partial state may exist.
func (c *Client) ClaimJob(ctx context.Context, runID, jobID, runnerID string) (*JobClaim, error) {
	var resp claimJobResponse
	path := c.statePath("/runs/" + urlSegment(runID) + "/jobs/" + urlSegment(jobID) + "/claim")
	if err := c.doJSON(ctx, http.MethodPost, path, ClaimJobRequest{RunnerID: runnerID}, &resp, false); err != nil {
		return nil, fmt.Errorf("claim job %s: %w", jobID, err)
	}
	return &resp.Claim, nil
}

// Heartbeat calls POST …/state/runs/{runID}/jobs/{jobID}/heartbeat. A
// 409 lease_lost is returned as an *APIError (IsLeaseLost) so the runner can
// stop the job: someone else owns it now.
func (c *Client) Heartbeat(ctx context.Context, runID, jobID, runnerID string) (*HeartbeatInfo, error) {
	var resp HeartbeatInfo
	path := c.statePath("/runs/" + urlSegment(runID) + "/jobs/" + urlSegment(jobID) + "/heartbeat")
	if err := c.doJSON(ctx, http.MethodPost, path, HeartbeatRequest{RunnerID: runnerID}, &resp, false); err != nil {
		return nil, fmt.Errorf("heartbeat job %s: %w", jobID, err)
	}
	return &resp, nil
}

// UpdateJob calls POST …/state/runs/{runID}/jobs/{jobID}/update with a terminal
// status ("succeeded" | "failed"). Idempotent and terminal-sticky server-side;
// a 409 lease_lost surfaces as an *APIError. Not retried at this layer.
func (c *Client) UpdateJob(ctx context.Context, runID, jobID, runnerID, status, errText string) error {
	path := c.statePath("/runs/" + urlSegment(runID) + "/jobs/" + urlSegment(jobID) + "/update")
	req := UpdateJobRequest{RunnerID: runnerID, Status: status, ErrorText: errText}
	if err := c.doJSON(ctx, http.MethodPost, path, req, nil, false); err != nil {
		return fmt.Errorf("update job %s: %w", jobID, err)
	}
	return nil
}

// AppendLog calls POST …/state/runs/{runID}/logs/{jobID} with a single log
// chunk (≤ 1 MiB) under the runner's live lease, returning the assigned seq.
// A 409 lease_lost surfaces as an *APIError. Uses the longer log timeout.
func (c *Client) AppendLog(ctx context.Context, runID, jobID, runnerID, content string) (int, error) {
	path := c.statePath("/runs/" + urlSegment(runID) + "/logs/" + urlSegment(jobID))
	var resp appendLogResponse
	if err := c.doJSONWith(ctx, c.logClient, http.MethodPost, path,
		AppendLogRequest{RunnerID: runnerID, Content: content}, &resp); err != nil {
		return 0, fmt.Errorf("append log %s: %w", jobID, err)
	}
	return resp.Seq, nil
}

// ReadLog calls GET …/state/runs/{runID}/logs/{jobID}?fromSeq= and returns the
// assembled content from fromSeq onward plus the live-tail cursor (nextSeq) and
// whether the job is complete (no more chunks coming).
func (c *Client) ReadLog(ctx context.Context, runID, jobID string, fromSeq int) (*ReadLogResult, error) {
	path := c.statePath("/runs/" + urlSegment(runID) + "/logs/" + urlSegment(jobID))
	if fromSeq > 0 {
		path += "?fromSeq=" + strconv.Itoa(fromSeq)
	}
	var resp ReadLogResult
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp, true); err != nil {
		return nil, fmt.Errorf("read log %s: %w", jobID, err)
	}
	return &resp, nil
}

// ── internal helpers ──────────────────────────────────────────────────────────

// doJSON executes a JSON API call. When retryable is true, idempotent 5xx
// responses are retried with exponential back-off.
func (c *Client) doJSON(ctx context.Context, method, path string, body, out interface{}, retryable bool) error {
	var lastErr error
	attempts := 1
	if retryable {
		attempts = maxRetryAttempts
	}
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			delay := backoff(attempt)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		err := c.doJSONOnce(ctx, c.httpClient, method, path, body, out)
		if err == nil {
			return nil
		}
		if mapped := c.mapAPIError(err); mapped != err {
			return mapped
		}
		// Only retry on server errors (5xx), not on client errors (4xx).
		if !isRetryable(err) {
			return err
		}
		lastErr = err
	}
	return lastErr
}

// doJSONWith executes a single (non-retried) JSON call using a specific HTTP
// client — e.g. the longer-timeout log client — while applying the same
// contract-/auth-error rendering as doJSON.
func (c *Client) doJSONWith(ctx context.Context, httpClient *http.Client, method, path string, body, out interface{}) error {
	if err := c.doJSONOnce(ctx, httpClient, method, path, body, out); err != nil {
		return c.mapAPIError(err)
	}
	return nil
}

// mapAPIError renders the two errors that must fail loud and actionable
// regardless of the call: an unsupported contract major and an auth failure.
// Any other error is returned unchanged (and may be retried by the caller).
func (c *Client) mapAPIError(err error) error {
	apiErr, isAPI := err.(*APIError)
	if isAPI && apiErr.IsContractVersionUnsupported() {
		return renderContractVersionError(apiErr)
	}
	if isAPI && apiErr.IsAuth() {
		return fmt.Errorf("authentication failed: %w\n"+
			"hint: in GitHub Actions add `id-token: write` to workflow permissions; "+
			"outside GitHub Actions set ORUN_TOKEN", err)
	}
	return err
}

// renderContractVersionError turns a 409 contract_version_unsupported response
// into an actionable CLI error. The platform includes the supported range in
// the envelope details/message; this surfaces it so version skew fails loud.
func renderContractVersionError(apiErr *APIError) error {
	detail := strings.TrimSpace(apiErr.Message)
	supported := ""
	if len(apiErr.Details) > 0 {
		var d struct {
			Supported    string   `json:"supported"`
			SupportedMin string   `json:"supportedMin"`
			SupportedMax string   `json:"supportedMax"`
			Versions     []string `json:"versions"`
		}
		if json.Unmarshal(apiErr.Details, &d) == nil {
			switch {
			case d.Supported != "":
				supported = d.Supported
			case d.SupportedMin != "" || d.SupportedMax != "":
				supported = strings.TrimSpace(d.SupportedMin + "–" + d.SupportedMax)
			case len(d.Versions) > 0:
				supported = strings.Join(d.Versions, ", ")
			}
		}
	}
	msg := fmt.Sprintf(
		"this orun is too old or too new for the backend: it speaks contract version %s",
		ContractVersion,
	)
	if supported != "" {
		msg += fmt.Sprintf(", but the backend supports %s", supported)
	}
	if detail != "" && !strings.EqualFold(detail, "contract_version_unsupported") {
		msg += fmt.Sprintf(" (%s)", detail)
	}
	msg += "; upgrade or downgrade orun to match the backend"
	if apiErr.RequestID != "" {
		msg += fmt.Sprintf(" [requestId: %s]", apiErr.RequestID)
	}
	return fmt.Errorf("%s: %w", msg, apiErr)
}

func (c *Client) doJSONOnce(ctx context.Context, httpClient *http.Client, method, path string, body, out interface{}) error {
	token, err := c.tokenSrc.Token(ctx)
	if err != nil {
		return fmt.Errorf("resolving auth token: %w", err)
	}

	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshalling request body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set(contractVersionHeader, ContractVersion)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.parseError(resp)
	}

	if out != nil && resp.StatusCode != http.StatusNoContent {
		data, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("reading response: %w", readErr)
		}
		if err := decodeSuccessBody(data, out); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}
	}
	return nil
}

// decodeSuccessBody decodes a 2xx response body into out, unwrapping the
// platform success envelope ({ "data": <payload>, "meta": {...} }) when
// present. The platform always wraps; the OSS single-tenant backend (OC6)
// returns flat bodies with no "data" key, so a missing envelope is tolerated
// by falling back to a flat decode. This is the fix for the OC0 latent bug
// where wrapped run/object responses decoded to zero-values.
func decodeSuccessBody(data []byte, out interface{}) error {
	if len(data) == 0 || out == nil {
		return nil
	}
	// Peek for a top-level "data" key. We unwrap only when the envelope is an
	// object carrying "data"; a bare array/scalar body or a flat OSS object is
	// decoded as-is.
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(data, &env) == nil && len(env.Data) > 0 {
		return json.Unmarshal(env.Data, out)
	}
	return json.Unmarshal(data, out)
}

func (c *Client) parseError(resp *http.Response) error {
	data, _ := io.ReadAll(resp.Body)
	if apiErr := decodeErrorEnvelope(data, resp.StatusCode); apiErr != nil {
		return apiErr
	}
	return &APIError{
		Message: fmt.Sprintf("server returned status %d", resp.StatusCode),
		Code:    httpStatusCode(resp.StatusCode),
		Status:  resp.StatusCode,
	}
}

// decodeErrorEnvelope parses both the platform's nested envelope
// ({ error: { code, message, details?, requestId } }) and the OSS backend's
// flat envelope ({ error, code }). It returns nil if the body carries no
// recognizable error so the caller can fall back to a status-derived message.
func decodeErrorEnvelope(data []byte, status int) *APIError {
	if len(data) == 0 {
		return nil
	}

	// Nested platform shape first: { "error": { ... } }.
	var nested struct {
		Error *struct {
			Code      string          `json:"code"`
			Message   string          `json:"message"`
			Details   json.RawMessage `json:"details"`
			RequestID string          `json:"requestId"`
		} `json:"error"`
		// requestId may also appear at the top level depending on the edge.
		RequestID string `json:"requestId"`
	}
	if json.Unmarshal(data, &nested) == nil && nested.Error != nil &&
		(nested.Error.Message != "" || nested.Error.Code != "") {
		reqID := nested.Error.RequestID
		if reqID == "" {
			reqID = nested.RequestID
		}
		return &APIError{
			Message:   nested.Error.Message,
			Code:      nested.Error.Code,
			RequestID: reqID,
			Details:   nested.Error.Details,
			Status:    status,
		}
	}

	// Flat OSS shape: { "error": "msg", "code": "CODE", "requestId": "…" }.
	var flat struct {
		Error     string `json:"error"`
		Code      string `json:"code"`
		RequestID string `json:"requestId"`
	}
	if json.Unmarshal(data, &flat) == nil && flat.Error != "" {
		return &APIError{
			Message:   flat.Error,
			Code:      flat.Code,
			RequestID: flat.RequestID,
			Status:    status,
		}
	}
	return nil
}

func httpStatusCode(status int) string {
	switch status {
	case 401:
		return "UNAUTHORIZED"
	case 403:
		return "FORBIDDEN"
	case 404:
		return "NOT_FOUND"
	case 409:
		return "CONFLICT"
	case 429:
		return "RATE_LIMITED"
	case 400:
		return "INVALID_REQUEST"
	default:
		return "INTERNAL_ERROR"
	}
}

// isRetryable returns true for network errors, 5xx responses, and rate limits.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	apiErr, ok := err.(*APIError)
	if ok {
		// Retry on the legacy OSS codes and on any 5xx / 429 status. A 409
		// contract_version_unsupported is never retried (handled before this).
		if apiErr.Code == "INTERNAL_ERROR" || apiErr.Code == "RATE_LIMITED" {
			return true
		}
		return apiErr.Status >= 500 || apiErr.Status == http.StatusTooManyRequests
	}
	// Network-level errors are retryable.
	return true
}

// backoff computes an exponential backoff duration with jitter.
func backoff(attempt int) time.Duration {
	base := float64(retryBaseDelay)
	exp := base * math.Pow(2, float64(attempt-1))
	if exp > float64(retryMaxDelay) {
		exp = float64(retryMaxDelay)
	}
	jitter := rand.Float64() * float64(retryBaseDelay)
	return time.Duration(exp + jitter)
}

// urlSegment escapes a path segment to be safe in a URL path.
// We only need to handle the most common special characters.
func urlSegment(s string) string {
	s = strings.ReplaceAll(s, "/", "%2F")
	s = strings.ReplaceAll(s, ":", "%3A")
	s = strings.ReplaceAll(s, " ", "%20")
	return s
}
