// Package forge is the small GitHub surface the bootstrap actions need beyond
// the provenance pen: does this repository exist, and is this workflow run
// finished (orun-bootstrap-engine BE-O5c).
//
// It is deliberately NOT in internal/provenance. That package is the pen — the
// gesture that binds a PR to a task — and creating a repository or watching a
// convergence is neither. Keeping them apart means the pen's surface stays a
// statement about provenance rather than a drawer of GitHub calls.
package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Client talks to the GitHub API.
type Client struct {
	// Token is the credential. Empty is refused rather than attempted
	// anonymously: every call here either creates something or reads a private
	// repository, and an anonymous 404 is indistinguishable from "absent".
	Token string
	// TokenFn, when set, supplies the credential for EACH request, and Token is
	// the fallback. A build's convergence watch can outlive a minted token by
	// an hour; asking per request is what keeps it authenticated.
	TokenFn func() string
	// APIBase overrides https://api.github.com (tests).
	APIBase string
	HTTP    *http.Client
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, out any) (int, error) {
	base := c.APIBase
	if base == "" {
		base = "https://api.github.com"
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token())
	req.Header.Set("Accept", "application/vnd.github+json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 && out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, fmt.Errorf("decoding %s: %w", path, err)
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var ghErr struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &ghErr)
		detail := ghErr.Message
		if detail == "" {
			detail = strings.TrimSpace(string(raw))
			if len(detail) > 200 {
				detail = detail[:200] + "…"
			}
		}
		return resp.StatusCode, fmt.Errorf("GitHub answered %d for %s: %s", resp.StatusCode, path, detail)
	}
	return resp.StatusCode, nil
}

// RepoResult reports what EnsureRepo did.
type RepoResult struct {
	FullName string
	CloneURL string
	Created  bool
}

// EnsureRepo finds or creates a repository under an owner.
//
// Find-or-create, for the same reason every other bootstrap write is: phase 01
// is re-run like any other phase, and the second run must find the repository
// the first one made rather than fail on a name collision.
//
// An organization and a user take DIFFERENT endpoints, and the difference is
// not cosmetic: GitHub reads the owner of `POST /user/repos` off the token's
// own identity, so creating in a personal account works only for that account's
// own token. Naming the owner explicitly keeps that failure legible.
func (c *Client) EnsureRepo(ctx context.Context, owner, name string, private bool, inOrg bool) (*RepoResult, error) {
	if c.token() == "" {
		return nil, fmt.Errorf("forge: a GitHub credential is required (GITHUB_TOKEN / GH_TOKEN)")
	}
	if owner == "" || name == "" {
		return nil, fmt.Errorf("forge: owner and name are required")
	}
	var existing struct {
		FullName string `json:"full_name"`
		CloneURL string `json:"clone_url"`
	}
	status, err := c.do(ctx, http.MethodGet, "/repos/"+owner+"/"+name, nil, &existing)
	if err == nil {
		return &RepoResult{FullName: existing.FullName, CloneURL: existing.CloneURL, Created: false}, nil
	}
	if status != http.StatusNotFound {
		return nil, err
	}

	path := "/user/repos"
	if inOrg {
		path = "/orgs/" + owner + "/repos"
	}
	payload, _ := json.Marshal(map[string]any{"name": name, "private": private})
	var created struct {
		FullName string `json:"full_name"`
		CloneURL string `json:"clone_url"`
	}
	if _, err := c.do(ctx, http.MethodPost, path, payload, &created); err != nil {
		return nil, err
	}
	return &RepoResult{FullName: created.FullName, CloneURL: created.CloneURL, Created: true}, nil
}

// Run is one workflow run.
type Run struct {
	ID         int64  `json:"id"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HeadSHA    string `json:"head_sha"`
	HTMLURL    string `json:"html_url"`
}

// LatestRun returns the newest workflow run on a branch, or nil when there is
// none. No run is a real answer, not an error: a repository whose CI has not
// landed yet has nothing to converge.
func (c *Client) LatestRun(ctx context.Context, owner, repo, branch string) (*Run, error) {
	var payload struct {
		Runs []Run `json:"workflow_runs"`
	}
	path := fmt.Sprintf("/repos/%s/%s/actions/runs?branch=%s&per_page=1", owner, repo, branch)
	if _, err := c.do(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	if len(payload.Runs) == 0 {
		return nil, nil
	}
	return &payload.Runs[0], nil
}

// RunForCommit returns the newest workflow run on a branch for one commit, or
// nil when GitHub has not registered one yet. A push's run appears a few
// seconds after the push, so "none" here means "not yet", not "never".
func (c *Client) RunForCommit(ctx context.Context, owner, repo, branch, sha string) (*Run, error) {
	var payload struct {
		Runs []Run `json:"workflow_runs"`
	}
	path := fmt.Sprintf("/repos/%s/%s/actions/runs?branch=%s&head_sha=%s&per_page=1", owner, repo, branch, sha)
	if _, err := c.do(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	if len(payload.Runs) == 0 {
		return nil, nil
	}
	return &payload.Runs[0], nil
}

// GetRun reads one run by id.
func (c *Client) GetRun(ctx context.Context, owner, repo string, id int64) (*Run, error) {
	var run Run
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d", owner, repo, id)
	if _, err := c.do(ctx, http.MethodGet, path, nil, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

// RerunFailed asks GitHub to re-open a run's failed jobs.
//
// Best effort by design: GitHub refuses this for a run with no retriable failed
// jobs, for a token without `actions: write`, and for a run past its retention.
// The caller has already established the run failed; losing the diagnosis
// because the retry could not be issued is the part that costs someone an
// afternoon.
func (c *Client) RerunFailed(ctx context.Context, owner, repo string, id int64) error {
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/rerun-failed-jobs", owner, repo, id)
	_, err := c.do(ctx, http.MethodPost, path, []byte("{}"), nil)
	return err
}

// FailedJobs names the jobs that did not pass, for a diagnosis a person can act
// on. "The convergence failed" is not one.
func (c *Client) FailedJobs(ctx context.Context, owner, repo string, id int64) ([]string, error) {
	var payload struct {
		Jobs []struct {
			Name       string `json:"name"`
			Conclusion string `json:"conclusion"`
		} `json:"jobs"`
	}
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/jobs?per_page=100", owner, repo, id)
	if _, err := c.do(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	var failed []string
	for _, j := range payload.Jobs {
		switch j.Conclusion {
		case "", "success", "neutral", "skipped":
		default:
			failed = append(failed, fmt.Sprintf("%s (%s)", j.Name, j.Conclusion))
		}
	}
	sort.Strings(failed)
	return failed, nil
}

// token is the credential for one request: TokenFn's answer, else Token.
func (c *Client) token() string {
	if c.TokenFn != nil {
		if t := c.TokenFn(); t != "" {
			return t
		}
	}
	return c.Token
}
