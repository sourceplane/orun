package statebackend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

// ErrClaimTargetNotFound is returned by Claim when the coordination endpoint
// reports 404 for the run/job. In the distributed-matrix pattern every job
// independently InitRuns the shared run and then claims; InitRun lands via the
// REST run store while the claim hits the native coordination shard, so the
// first concurrent claimers can race the run becoming visible to coordination
// (read-after-write gap on the hosted backend). The caller treats this as a
// transient "run not ready yet" and retries with backoff rather than failing
// the job (coordination-api.md §3).
var ErrClaimTargetNotFound = errors.New("coordination run/job not found")

// Coordination HTTP client (NC3 — coordination-api.md §3). The CLI's transport
// for the conditional-append verbs: it posts claim/heartbeat/complete to the
// scoped state endpoints and decodes the responses into the NC2 driver types.
// The server (DO-sharded hosted or plain-Postgres OSS) is the authority; this is
// a thin, well-typed round-trip the runner loop drives via ActionForClaim /
// ActionForHeartbeat.

// TokenSource resolves the bearer for each request. It mirrors
// remotestate.TokenSource so the CI OIDC-exchange source (and any other source)
// can authenticate coordination verbs without this package importing remotestate.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// CoordClient talks the coordination verbs against a scoped base URL, e.g.
// https://host/v1/organizations/{org}/projects/{proj}/state.
type CoordClient struct {
	HTTP    *http.Client
	BaseURL string // scoped base, no trailing slash
	Token   string // static bearer (used when TokenSource is nil)
	// TokenSource, when set, resolves the bearer per request (takes precedence
	// over Token). This is how the CI golden path authenticates: an OIDC token
	// exchanged for a short-lived workflow token.
	TokenSource TokenSource
	// RetryAttempts bounds how many times a verb is asked in all when it fails
	// in transit or the server answers 429 / 5xx. Zero means the default.
	RetryAttempts int
}

// A VERB THAT FAILED IN TRANSIT IS ASKED AGAIN.
//
// A product's CI lane claims its job over the network, and one reset
// connection on that claim ended the lane:
//
//	✕ claiming job integrations-worker.dev.verify-deploy: Post "…/jobs/…:claim":
//	  read tcp 10.1.0.194:44618->104.21.44.152:443: read: connection reset by peer
//
// before a single step had run — a failure with nothing wrong in the product,
// which then failed the phase's landing and stopped an unattended build.
//
// Every coordination verb is safe to ask twice, because the server makes each
// one idempotent by construction: a re-claim by the holder of a live lease
// answers the same lease; a heartbeat renews what it renews; a complete for a
// job already in that terminal state is a no-op; the frontier and the log are
// reads. So a transport failure — reset, timeout, EOF, anything below HTTP —
// and a 429 or 5xx are retried with exponential backoff and jitter, honouring
// a Retry-After hint, up to coordRetryAttempts in all. A 4xx is an answer,
// never retried; the caller's own context ending is the caller giving up.
const (
	coordRetryAttempts = 5
	coordRetryBase     = 1 * time.Second
	coordRetryMax      = 15 * time.Second
)

// coordRetryWait is the wait before retry `attempt` (1-based), given a
// Retry-After hint in seconds when the server sent one. A variable so tests
// do not sleep.
var coordRetryWait = func(attempt int, retryAfter string) time.Duration {
	if secs, err := strconv.Atoi(retryAfter); err == nil && secs > 0 {
		d := time.Duration(secs)*time.Second + time.Duration(rand.Float64()*float64(coordRetryBase))
		if d > coordRetryMax {
			d = coordRetryMax
		}
		return d
	}
	exp := float64(coordRetryBase) * math.Pow(2, float64(attempt-1))
	if exp > float64(coordRetryMax) {
		exp = float64(coordRetryMax)
	}
	return time.Duration(exp + rand.Float64()*float64(coordRetryBase))
}

// transientStatus is a status worth asking again: the server or a hop in
// front of it could not answer this time.
func transientStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

func (c *CoordClient) attempts() int {
	if c.RetryAttempts > 0 {
		return c.RetryAttempts
	}
	return coordRetryAttempts
}

// do sends the verb, asking again on a transport failure or a transient
// status (see above). The response returned is unread; on the last attempt a
// transient status comes back as the response it was, so the caller reports
// it the way it reports any other unexpected status.
func (c *CoordClient) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var payload []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		payload = b
	}
	attempts := c.attempts()
	for attempt := 1; ; attempt++ {
		resp, err := c.once(ctx, method, path, payload)
		last := attempt >= attempts
		if err != nil {
			if last || ctx.Err() != nil {
				return nil, err
			}
			if werr := waitRetry(ctx, coordRetryWait(attempt, "")); werr != nil {
				return nil, err
			}
			continue
		}
		if !transientStatus(resp.StatusCode) || last {
			return resp, nil
		}
		hint := resp.Header.Get("Retry-After")
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		if werr := waitRetry(ctx, coordRetryWait(attempt, hint)); werr != nil {
			return nil, fmt.Errorf("%s %s: status %d, and the retry was cut short: %w", method, path, resp.StatusCode, werr)
		}
	}
}

func waitRetry(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// once sends the verb exactly once. The body is rebuilt per call, so a retry
// never sends a reader another attempt already drained.
func (c *CoordClient) once(ctx context.Context, method, path string, payload []byte) (*http.Response, error) {
	var r io.Reader
	if payload != nil {
		r = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, r)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Orun-Contract-Version", "2")
	bearer := c.Token
	if c.TokenSource != nil {
		t, err := c.TokenSource.Token(ctx)
		if err != nil {
			return nil, fmt.Errorf("resolve auth token: %w", err)
		}
		bearer = t
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	return hc.Do(req)
}

type claimResponse struct {
	Claimed                  bool   `json:"claimed"`
	Reason                   string `json:"reason"`
	Cached                   bool   `json:"cached"`
	LeaseEpoch               int    `json:"leaseEpoch"`
	LeaseExpiresAt           string `json:"leaseExpiresAt"`
	Attempt                  int    `json:"attempt"`
	LeaseSeconds             int    `json:"leaseSeconds"`
	HeartbeatIntervalSeconds int    `json:"heartbeatIntervalSeconds"`
	Result                   struct {
		Digest string `json:"digest"`
	} `json:"result"`
}

// ClaimRequest carries the runner id and the optional memoization hints. The
// client supplies only the key — hermetic marks the job memoizable and
// jobInputHash is the content key the server resolves a prior result by; the
// server (not the client) decides the cache hit and its digest.
type ClaimRequest struct {
	RunnerID     string
	Hermetic     bool
	JobInputHash string
	// Retry re-opens a terminally-FAILED (or timed-out) job as a fresh claim —
	// the `orun run --job X --retry` shape a CI rerun uses to resume one
	// execution. Succeeded jobs stay terminal server-side regardless.
	Retry bool
}

// Claim posts a :claim and decodes the outcome into the driver's ClaimOutcome.
func (c *CoordClient) Claim(ctx context.Context, runID, jobID string, req ClaimRequest) (ClaimOutcome, error) {
	body := map[string]any{"runnerId": req.RunnerID}
	if req.Hermetic {
		body["hermetic"] = true
	}
	if req.JobInputHash != "" {
		body["jobInputHash"] = req.JobInputHash
	}
	if req.Retry {
		body["retry"] = true
	}
	resp, err := c.do(ctx, http.MethodPost,
		fmt.Sprintf("/runs/%s/jobs/%s:claim", runID, jobID), body)
	if err != nil {
		return ClaimOutcome{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		// The run isn't visible to coordination yet (sibling matrix jobs are
		// still initializing it). Transient — let the caller retry.
		return ClaimOutcome{}, ErrClaimTargetNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return ClaimOutcome{}, fmt.Errorf("claim %s: unexpected status %d", jobID, resp.StatusCode)
	}
	var cr claimResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return ClaimOutcome{}, err
	}
	switch {
	case cr.Cached:
		return ClaimOutcome{Kind: OutcomeCached, ResultDigest: cr.Result.Digest}, nil
	case cr.Claimed:
		return ClaimOutcome{
			Kind:                     OutcomeClaimed,
			LeaseEpoch:               cr.LeaseEpoch,
			LeaseExpiresAt:           cr.LeaseExpiresAt,
			Attempt:                  cr.Attempt,
			LeaseSeconds:             cr.LeaseSeconds,
			HeartbeatIntervalSeconds: cr.HeartbeatIntervalSeconds,
		}, nil
	default:
		return ClaimOutcome{Kind: OutcomeRejected, Reason: cr.Reason}, nil
	}
}

type frontierResponse struct {
	Jobs []string `json:"jobs"`
}

// Frontier reads GET …/runs/{runId}/frontier — the currently-runnable job ids
// (the server's projection of the fold's frontier). The runner uses it as the
// local schedule; the conditional :claim remains the authority.
func (c *CoordClient) Frontier(ctx context.Context, runID string) ([]string, error) {
	resp, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/runs/%s/frontier", runID), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("frontier %s: unexpected status %d", runID, resp.StatusCode)
	}
	var fr frontierResponse
	if err := json.NewDecoder(resp.Body).Decode(&fr); err != nil {
		return nil, err
	}
	return fr.Jobs, nil
}

// Heartbeat posts a :heartbeat. A 409 means the lease was lost (stop the job).
func (c *CoordClient) Heartbeat(ctx context.Context, runID, jobID, runnerID string, leaseEpoch int) (leaseLost bool, err error) {
	resp, err := c.do(ctx, http.MethodPost,
		fmt.Sprintf("/runs/%s/jobs/%s:heartbeat", runID, jobID),
		map[string]any{"runnerId": runnerID, "leaseEpoch": leaseEpoch})
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return false, nil
	case http.StatusConflict:
		return true, nil
	default:
		return false, fmt.Errorf("heartbeat %s: unexpected status %d", jobID, resp.StatusCode)
	}
}

// CompleteRequest is the terminal transition the runner reports.
type CompleteRequest struct {
	RunnerID     string `json:"runnerId"`
	LeaseEpoch   int    `json:"leaseEpoch"`
	Outcome      string `json:"outcome"` // "succeeded" | "failed"
	ResultDigest string `json:"resultDigest,omitempty"`
	// JobInputHash, set on a hermetic success, is the memo key the server indexes
	// (jobInputHash → resultDigest) so a later run with the same inputs is cached.
	JobInputHash string `json:"jobInputHash,omitempty"`
	ErrorText    string `json:"errorText,omitempty"`
}

// Complete posts a :complete. A 409 means the lease was lost.
func (c *CoordClient) Complete(ctx context.Context, runID, jobID string, req CompleteRequest) (leaseLost bool, err error) {
	resp, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/runs/%s/jobs/%s:complete", runID, jobID), req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return false, nil
	case http.StatusConflict:
		return true, nil
	default:
		return false, fmt.Errorf("complete %s: unexpected status %d", jobID, resp.StatusCode)
	}
}
