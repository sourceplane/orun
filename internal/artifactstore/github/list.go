package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/sourceplane/orun/internal/artifactstore"
	"github.com/sourceplane/orun/internal/runbundle"
)

// WorkflowRun represents a GitHub Actions workflow run.
type WorkflowRun struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	HeadSHA    string `json:"head_sha"`
	HeadBranch string `json:"head_branch"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	RunNumber  int64  `json:"run_number"`
	Event      string `json:"event"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
	HTMLURL    string `json:"html_url"`
	Attempts   int64  `json:"run_attempt"`
}

// ListRunOptions filters workflow run listings.
type ListRunOptions struct {
	Branch   string
	SHA      string
	Event    string
	Status   string
	PerPage  int
}

// GitHubArtifact represents a GitHub Actions artifact.
type GitHubArtifact struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	SizeInBytes int64  `json:"size_in_bytes"`
	CreatedAt   string `json:"created_at"`
	ExpiresAt   string `json:"expires_at"`
	WorkflowRun struct {
		ID int64 `json:"id"`
	} `json:"workflow_run"`
}

// ListWorkflowRuns lists workflow runs for the repository.
// GET /repos/{owner}/{repo}/actions/runs
func (c *Client) ListWorkflowRuns(ctx context.Context, opts ListRunOptions) ([]WorkflowRun, error) {
	u := c.apiURL(fmt.Sprintf("/repos/%s/actions/runs", c.repo))
	q := url.Values{}

	if opts.Branch != "" {
		q.Set("branch", opts.Branch)
	}
	if opts.SHA != "" {
		q.Set("head_sha", opts.SHA)
	}
	if opts.Event != "" {
		q.Set("event", opts.Event)
	}
	if opts.Status != "" {
		q.Set("status", opts.Status)
	}
	perPage := opts.PerPage
	if perPage < 1 || perPage > 100 {
		perPage = 10
	}
	q.Set("per_page", strconv.Itoa(perPage))

	if len(q) > 0 {
		u += "?" + q.Encode()
	}

	var result struct {
		WorkflowRuns []WorkflowRun `json:"workflow_runs"`
	}
	if err := c.getJSON(ctx, u, &result); err != nil {
		return nil, err
	}

	return result.WorkflowRuns, nil
}

// ListArtifacts lists all artifacts for a workflow run.
// GET /repos/{owner}/{repo}/actions/runs/{run_id}/artifacts
func (c *Client) ListArtifacts(ctx context.Context, runID int64) ([]artifactstore.RemoteShard, error) {
	u := c.apiURL(fmt.Sprintf("/repos/%s/actions/runs/%d/artifacts", c.repo, runID))

	var result struct {
		Artifacts []GitHubArtifact `json:"artifacts"`
	}
	if err := c.getJSON(ctx, u, &result); err != nil {
		return nil, err
	}

	shards := make([]artifactstore.RemoteShard, 0, len(result.Artifacts))
	for _, a := range result.Artifacts {
		parsed := parseTime(a.CreatedAt)
		expires := parseTime(a.ExpiresAt)

		shards = append(shards, artifactstore.RemoteShard{
			Name:      a.Name,
			ID:        strconv.FormatInt(a.ID, 10),
			SizeBytes: a.SizeInBytes,
			CreatedAt: parsed,
			ExpiresAt: expires,
			Parsed:    runbundle.ParseShardName(a.Name),
			SourceMeta: map[string]string{
				"runId": strconv.FormatInt(runID, 10),
			},
		})
	}

	return shards, nil
}

// ListOrunArtifacts filters artifacts to only those matching the Orun naming scheme.
func (c *Client) ListOrunArtifacts(ctx context.Context, runID int64) ([]artifactstore.RemoteShard, error) {
	all, err := c.ListArtifacts(ctx, runID)
	if err != nil {
		return nil, err
	}

	orun := make([]artifactstore.RemoteShard, 0, len(all))
	for _, a := range all {
		if a.Parsed != nil {
			orun = append(orun, a)
		}
	}
	return orun, nil
}

// getJSON performs a GET request and decodes the JSON response.
func (c *Client) getJSON(ctx context.Context, url string, target interface{}) error {
	req, err := c.newRequest(ctx, url)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	resp, err := c.doRequest(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// parseTime parses an RFC3339 time string, returning zero time on error.
func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}