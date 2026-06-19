package statebackend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

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
}

func (c *CoordClient) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, r)
	if err != nil {
		return nil, err
	}
	if body != nil {
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
	Claimed        bool   `json:"claimed"`
	Reason         string `json:"reason"`
	Cached         bool   `json:"cached"`
	LeaseEpoch     int    `json:"leaseEpoch"`
	LeaseExpiresAt string `json:"leaseExpiresAt"`
	Result         struct {
		Digest string `json:"digest"`
	} `json:"result"`
}

// Claim posts a :claim and decodes the outcome into the driver's ClaimOutcome.
func (c *CoordClient) Claim(ctx context.Context, runID, jobID, runnerID string) (ClaimOutcome, error) {
	resp, err := c.do(ctx, http.MethodPost,
		fmt.Sprintf("/runs/%s/jobs/%s:claim", runID, jobID),
		map[string]string{"runnerId": runnerID})
	if err != nil {
		return ClaimOutcome{}, err
	}
	defer resp.Body.Close()
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
		return ClaimOutcome{Kind: OutcomeClaimed, LeaseEpoch: cr.LeaseEpoch, LeaseExpiresAt: cr.LeaseExpiresAt}, nil
	default:
		return ClaimOutcome{Kind: OutcomeRejected, Reason: cr.Reason}, nil
	}
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
