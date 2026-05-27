package github

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// ResolveOpts configures how to resolve a workflow run.
type ResolveOpts struct {
	RunID    int64  // explicit GitHub run ID
	ExecID   string // parse gh-{run_id}-{attempt}-{sha}
	SHA      string // commit SHA
	Branch   string // branch filter (default: current)
	Failed   bool   // latest failed run
	Workflow string // workflow filename filter
}

// ResolveRun resolves a workflow run based on the provided options.
// Resolution algorithm:
//  1. RunID: fetch run directly by ID.
//  2. ExecID: parse gh-{run_id}-{attempt}-{plan_sha}, fetch run.
//  3. SHA: list runs for SHA, pick latest.
//  4. Failed: list runs with status=failure, pick latest.
//  5. Default: latest run for current branch (empty branch = no filter).
func ResolveRun(ctx context.Context, c *Client, opts ResolveOpts) (*WorkflowRun, error) {
	switch {
	case opts.RunID > 0:
		return c.getWorkflowRun(ctx, opts.RunID)

	case opts.ExecID != "":
		// Parse gh-{run_id}-{attempt}-{plan_sha}
		parts := strings.SplitN(opts.ExecID, "-", 4)
		if len(parts) < 4 || parts[0] != "gh" {
			return nil, fmt.Errorf("invalid exec-id format: %q (expected gh-{run_id}-{attempt}-{sha})", opts.ExecID)
		}
		runID, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid run ID in exec-id %q: %w", opts.ExecID, err)
		}
		return c.getWorkflowRun(ctx, runID)

	case opts.SHA != "":
		runs, err := c.ListWorkflowRuns(ctx, ListRunOptions{
			SHA:     opts.SHA,
			PerPage: 1,
		})
		if err != nil {
			return nil, fmt.Errorf("list runs for SHA %s: %w", opts.SHA, err)
		}
		if len(runs) == 0 {
			return nil, fmt.Errorf("no runs found for SHA %s", opts.SHA)
		}
		return &runs[0], nil

	case opts.Failed:
		runs, err := c.ListWorkflowRuns(ctx, ListRunOptions{
			Branch:  opts.Branch,
			Status:  "failure",
			PerPage: 1,
		})
		if err != nil {
			return nil, fmt.Errorf("list failed runs: %w", err)
		}
		if len(runs) == 0 {
			return nil, fmt.Errorf("no failed runs found")
		}
		return &runs[0], nil

	default:
		runOpts := ListRunOptions{PerPage: 1}
		if opts.Branch != "" {
			runOpts.Branch = opts.Branch
		}
		runs, err := c.ListWorkflowRuns(ctx, runOpts)
		if err != nil {
			return nil, fmt.Errorf("list latest runs: %w", err)
		}
		if len(runs) == 0 {
			return nil, fmt.Errorf("no runs found")
		}
		return &runs[0], nil
	}
}

// getWorkflowRun fetches a single workflow run by ID.
// GET /repos/{owner}/{repo}/actions/runs/{run_id}
func (c *Client) getWorkflowRun(ctx context.Context, runID int64) (*WorkflowRun, error) {
	u := c.apiURL(fmt.Sprintf("/repos/%s/actions/runs/%d", c.repo, runID))
	var run WorkflowRun
	if err := c.getJSON(ctx, u, &run); err != nil {
		return nil, fmt.Errorf("get run %d: %w", runID, err)
	}
	return &run, nil
}