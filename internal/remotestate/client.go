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
)

// APIError represents a decoded backend error envelope.
type APIError struct {
	Message string `json:"error"`
	Code    string `json:"code"`
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s (code: %s)", e.Message, e.Code)
	}
	return e.Message
}

// IsAuth reports whether the error is an authentication/authorization failure.
func (e *APIError) IsAuth() bool {
	return e.Code == "UNAUTHORIZED" || e.Code == "FORBIDDEN"
}

// Client is an HTTP client for the orun-backend REST API.
type Client struct {
	baseURL    string
	tokenSrc   TokenSource
	userAgent  string
	httpClient *http.Client
	// logClient uses a longer timeout for log uploads.
	logClient *http.Client
}

// NewClient creates a Client for baseURL using the given TokenSource and CLI version.
func NewClient(baseURL, version string, tokenSrc TokenSource) *Client {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   connectTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}
	return &Client{
		baseURL:   strings.TrimRight(baseURL, "/"),
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

// ── API request/response types ────────────────────────────────────────────────

// CreateRunRequest is the body for POST /v1/runs.
type CreateRunRequest struct {
	Plan        *BackendPlan `json:"plan"`
	RunID       string       `json:"runId,omitempty"`
	NamespaceID string       `json:"namespaceId,omitempty"`
	DryRun      bool         `json:"dryRun,omitempty"`
	TriggerType string       `json:"triggerType,omitempty"`
	Actor       string       `json:"actor,omitempty"`
}

// RunResponse is the backend Run object returned by /v1/runs/*.
type RunResponse struct {
	RunID        string  `json:"runId"`
	Status       string  `json:"status"`
	PlanChecksum string  `json:"planChecksum"`
	TriggerType  string  `json:"triggerType"`
	Actor        *string `json:"actor"`
	CreatedAt    string  `json:"createdAt"`
	UpdatedAt    string  `json:"updatedAt"`
	FinishedAt   *string `json:"finishedAt"`
	JobTotal     int     `json:"jobTotal"`
	JobDone      int     `json:"jobDone"`
	JobFailed    int     `json:"jobFailed"`
	DryRun       bool    `json:"dryRun"`
}

// JobResponse is the backend Job object.
type JobResponse struct {
	JobID       string   `json:"jobId"`
	RunID       string   `json:"runId"`
	Component   string   `json:"component"`
	Status      string   `json:"status"`
	Deps        []string `json:"deps"`
	RunnerID    *string  `json:"runnerId"`
	StartedAt   *string  `json:"startedAt"`
	FinishedAt  *string  `json:"finishedAt"`
	LastError   *string  `json:"lastError"`
	HeartbeatAt *string  `json:"heartbeatAt"`
	LogRef      *string  `json:"logRef"`
}

// ListJobsResponse wraps the job list from GET /v1/runs/{runID}/jobs.
type ListJobsResponse struct {
	Jobs []JobResponse `json:"jobs"`
}

// RunnableResponse is from GET /v1/runs/{runID}/runnable.
type RunnableResponse struct {
	JobIDs []string `json:"jobs"`
}

// ClaimJobRequest is the body for POST /v1/runs/{runID}/jobs/{jobID}/claim.
type ClaimJobRequest struct {
	RunnerID string `json:"runnerId"`
}

// ClaimJobResponse represents the extended coordinator claim result.
type ClaimJobResponse struct {
	Claimed       bool   `json:"claimed"`
	Takeover      bool   `json:"takeover,omitempty"`
	CurrentStatus string `json:"currentStatus,omitempty"`
	DepsWaiting   bool   `json:"depsWaiting,omitempty"`
	DepsBlocked   bool   `json:"depsBlocked,omitempty"`
}

// HeartbeatRequest is the body for POST /v1/runs/{runID}/jobs/{jobID}/heartbeat.
type HeartbeatRequest struct {
	RunnerID string `json:"runnerId"`
}

// HeartbeatResponse is returned by the heartbeat endpoint.
type HeartbeatResponse struct {
	OK bool `json:"ok"`
}

// UpdateJobRequest is the body for POST /v1/runs/{runID}/jobs/{jobID}/update.
type UpdateJobRequest struct {
	RunnerID string `json:"runnerId"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
}

// LogUploadResponse is returned by POST /v1/runs/{runID}/logs/{jobID}.
type LogUploadResponse struct {
	OK     bool   `json:"ok"`
	LogRef string `json:"logRef"`
}

// ── API methods ───────────────────────────────────────────────────────────────

// CreateRun calls POST /v1/runs. Idempotent: if the run already exists the
// backend verifies the plan checksum and returns the existing run.
func (c *Client) CreateRun(ctx context.Context, req CreateRunRequest) (*RunResponse, error) {
	var resp RunResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v1/runs", req, &resp, true); err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}
	return &resp, nil
}

// GetRun calls GET /v1/runs/{runID}.
func (c *Client) GetRun(ctx context.Context, runID string) (*RunResponse, error) {
	var resp RunResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/runs/"+urlSegment(runID), nil, &resp, true); err != nil {
		return nil, fmt.Errorf("get run: %w", err)
	}
	return &resp, nil
}

// ListJobs calls GET /v1/runs/{runID}/jobs.
func (c *Client) ListJobs(ctx context.Context, runID string) ([]JobResponse, error) {
	var resp ListJobsResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/runs/"+urlSegment(runID)+"/jobs", nil, &resp, true); err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	return resp.Jobs, nil
}

// GetRunnable calls GET /v1/runs/{runID}/runnable.
func (c *Client) GetRunnable(ctx context.Context, runID string) ([]string, error) {
	var resp RunnableResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/runs/"+urlSegment(runID)+"/runnable", nil, &resp, true); err != nil {
		return nil, fmt.Errorf("get runnable: %w", err)
	}
	return resp.JobIDs, nil
}

// ClaimJob calls POST /v1/runs/{runID}/jobs/{jobID}/claim.
// This operation is not retried on 5xx because partial state may exist.
func (c *Client) ClaimJob(ctx context.Context, runID, jobID, runnerID string) (*ClaimJobResponse, error) {
	var resp ClaimJobResponse
	path := "/v1/runs/" + urlSegment(runID) + "/jobs/" + urlSegment(jobID) + "/claim"
	if err := c.doJSON(ctx, http.MethodPost, path, ClaimJobRequest{RunnerID: runnerID}, &resp, false); err != nil {
		return nil, fmt.Errorf("claim job %s: %w", jobID, err)
	}
	return &resp, nil
}

// Heartbeat calls POST /v1/runs/{runID}/jobs/{jobID}/heartbeat.
func (c *Client) Heartbeat(ctx context.Context, runID, jobID, runnerID string) error {
	var resp HeartbeatResponse
	path := "/v1/runs/" + urlSegment(runID) + "/jobs/" + urlSegment(jobID) + "/heartbeat"
	if err := c.doJSON(ctx, http.MethodPost, path, HeartbeatRequest{RunnerID: runnerID}, &resp, false); err != nil {
		return fmt.Errorf("heartbeat job %s: %w", jobID, err)
	}
	return nil
}

// UpdateJob calls POST /v1/runs/{runID}/jobs/{jobID}/update.
// Not retried because the backend is idempotent by run+job+runner identity
// and a duplicate terminal update is harmless.
func (c *Client) UpdateJob(ctx context.Context, runID, jobID, runnerID, status, errText string) error {
	path := "/v1/runs/" + urlSegment(runID) + "/jobs/" + urlSegment(jobID) + "/update"
	req := UpdateJobRequest{RunnerID: runnerID, Status: status}
	if errText != "" {
		req.Error = errText
	}
	if err := c.doJSON(ctx, http.MethodPost, path, req, nil, false); err != nil {
		return fmt.Errorf("update job %s: %w", jobID, err)
	}
	return nil
}

// UploadLog calls POST /v1/runs/{runID}/logs/{jobID} with the log content as
// plain text. Uses the longer log upload timeout.
func (c *Client) UploadLog(ctx context.Context, runID, jobID, content string) error {
	path := "/v1/runs/" + urlSegment(runID) + "/logs/" + urlSegment(jobID)
	token, err := c.tokenSrc.Token(ctx)
	if err != nil {
		return fmt.Errorf("resolving auth token: %w", err)
	}
	body := strings.NewReader(content)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("building log upload request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")

	resp, err := c.logClient.Do(req)
	if err != nil {
		return fmt.Errorf("log upload request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return c.parseError(resp)
	}
	return nil
}

// GetLog calls GET /v1/runs/{runID}/logs/{jobID} and returns the log as a string.
func (c *Client) GetLog(ctx context.Context, runID, jobID string) (string, error) {
	path := "/v1/runs/" + urlSegment(runID) + "/logs/" + urlSegment(jobID)
	token, err := c.tokenSrc.Token(ctx)
	if err != nil {
		return "", fmt.Errorf("resolving auth token: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return "", fmt.Errorf("building log fetch request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("log fetch request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode >= 400 {
		return "", c.parseError(resp)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading log response: %w", err)
	}
	return string(data), nil
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

		err := c.doJSONOnce(ctx, method, path, body, out)
		if err == nil {
			return nil
		}
		apiErr, isAPI := err.(*APIError)
		if isAPI && apiErr.IsAuth() {
			return fmt.Errorf("authentication failed: %w\n"+
				"hint: in GitHub Actions add `id-token: write` to workflow permissions; "+
				"outside GitHub Actions set ORUN_TOKEN", err)
		}
		// Only retry on server errors (5xx), not on client errors (4xx).
		if !isRetryable(err) {
			return err
		}
		lastErr = err
	}
	return lastErr
}

func (c *Client) doJSONOnce(ctx context.Context, method, path string, body, out interface{}) error {
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
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.parseError(resp)
	}

	if out != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}
	}
	return nil
}

func (c *Client) parseError(resp *http.Response) error {
	data, _ := io.ReadAll(resp.Body)
	var apiErr APIError
	if len(data) > 0 {
		if err := json.Unmarshal(data, &apiErr); err == nil && apiErr.Message != "" {
			return &apiErr
		}
	}
	return &APIError{
		Message: fmt.Sprintf("server returned status %d", resp.StatusCode),
		Code:    httpStatusCode(resp.StatusCode),
	}
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
		return apiErr.Code == "INTERNAL_ERROR" || apiErr.Code == "RATE_LIMITED"
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
