package remotestate

import (
	"context"
	"net/http"
)

// Work-plane client (orun-work v2 WP1) — the CLI's seam onto the cloud work
// API (/v1/organizations/{org}/work). Wire shapes mirror the platform's
// @saas/contracts/work. Deliberately small: import apply + the fold summary;
// lifecycle is derived server-side on every read and there is no
// status-writing call to offer (WP-3).

// WorkImportSpec mirrors the CLI import plan's spec entry.
type WorkImportSpec struct {
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	DocPath   string `json:"docPath"`
	DocSHA256 string `json:"docSha256"`
	PlanPath  string `json:"planPath,omitempty"`
}

// WorkImportTask mirrors the CLI import plan's task entry. No lifecycle
// field exists by design — rungs derive from observations after apply.
type WorkImportTask struct {
	SpecSlug    string        `json:"specSlug"`
	MilestoneID string        `json:"milestoneId"`
	Title       string        `json:"title"`
	Contract    *WorkContract `json:"contract,omitempty"`
}

// WorkContract is the wire form of the task contract.
type WorkContract struct {
	Goal         string   `json:"goal,omitempty"`
	Affects      []string `json:"affects,omitempty"`
	DoneWhen     []string `json:"doneWhen,omitempty"`
	Gates        []string `json:"gates,omitempty"`
	DesignRefs   []string `json:"designRefs,omitempty"`
	Deps         []string `json:"deps,omitempty"`
	GatesDefined bool     `json:"gatesDefined,omitempty"`
}

// WorkImportRequest is the apply body (the dry-run plan, verbatim).
type WorkImportRequest struct {
	Workspace string           `json:"workspace"`
	Root      string           `json:"root"`
	Prefix    string           `json:"prefix,omitempty"`
	Specs     []WorkImportSpec `json:"specs"`
	Tasks     []WorkImportTask `json:"tasks"`
}

// WorkImportResponse reports apply counts; re-imports skip idempotently.
type WorkImportResponse struct {
	SpecsCreated int `json:"specsCreated"`
	SpecsSkipped int `json:"specsSkipped"`
	TasksCreated int `json:"tasksCreated"`
	TasksSkipped int `json:"tasksSkipped"`
}

// WorkActor is a membership subject on the wire.
type WorkActor struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Via  string `json:"via,omitempty"`
}

// WorkLifecycle is the fold's per-task output: a rung WITH its evidence.
type WorkLifecycle struct {
	Rung     string   `json:"rung"`
	Ready    bool     `json:"ready"`
	Blocked  bool     `json:"blocked"`
	Evidence []string `json:"evidence,omitempty"`
}

// WorkTaskView is one task in the summary.
type WorkTaskView struct {
	Key       string            `json:"key"`
	Spec      string            `json:"spec,omitempty"`
	Title     string            `json:"title"`
	Labels    map[string]string `json:"labels,omitempty"`
	Contract  *WorkContract     `json:"contract,omitempty"`
	CreatedBy WorkActor         `json:"createdBy"`
	CreatedAt string            `json:"createdAt,omitempty"`
	Lifecycle WorkLifecycle     `json:"lifecycle"`
}

// WorkSpecView is one spec in the summary with its derived progress.
type WorkSpecView struct {
	Key       string         `json:"key"`
	Title     string         `json:"title"`
	DocRef    string         `json:"docRef,omitempty"`
	CreatedBy WorkActor      `json:"createdBy"`
	CreatedAt string         `json:"createdAt,omitempty"`
	Progress  map[string]int `json:"progress"`
}

// WorkSummary is the workspace lens: everything derives from the two logs.
type WorkSummary struct {
	Specs    []WorkSpecView `json:"specs"`
	Tasks    []WorkTaskView `json:"tasks"`
	CoordSeq int64          `json:"coordSeq"`
	ObsSeq   int64          `json:"obsSeq"`
}

// workPath builds an org-scoped work path (no project segment — the work
// plane is workspace-scoped, WP-7).
func (c *Client) workPath(suffix string) string {
	return "/v1/organizations/" + urlSegment(c.scope.orgSegment()) + "/work" + suffix
}

// ImportWork applies a dry-run import plan through the cloud mutators.
// Every resulting event carries actor via=import; nothing about lifecycle
// crosses this wire.
func (c *Client) ImportWork(ctx context.Context, req WorkImportRequest) (*WorkImportResponse, error) {
	var resp WorkImportResponse
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/import"), req, &resp, false); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetWorkSummary fetches the fold summary (rungs with evidence).
func (c *Client) GetWorkSummary(ctx context.Context) (*WorkSummary, error) {
	var resp WorkSummary
	if err := c.doJSON(ctx, http.MethodGet, c.workPath(""), nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ── Coordination mutators (the WP1 routes; the MCP's write surface) ─────────

// CreateWorkTaskRequest mirrors the platform's CreateWorkTaskRequest.
type CreateWorkTaskRequest struct {
	Prefix   string        `json:"prefix"`
	Title    string        `json:"title"`
	SpecKey  string        `json:"specKey,omitempty"`
	Contract *WorkContract `json:"contract,omitempty"`
}

// WorkMutationResponse reports the appended coordination event's seq.
type WorkMutationResponse struct {
	Key string `json:"key"`
	Seq int64  `json:"seq"`
}

// CreateWorkTask creates a task through the one mutator surface.
func (c *Client) CreateWorkTask(ctx context.Context, req CreateWorkTaskRequest) (*WorkMutationResponse, error) {
	var resp WorkMutationResponse
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/tasks"), req, &resp, false); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CommentWork appends a comment_added event.
func (c *Client) CommentWork(ctx context.Context, key, body string) (*WorkMutationResponse, error) {
	var resp WorkMutationResponse
	req := struct {
		Body string `json:"body"`
	}{Body: body}
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/tasks/"+urlSegment(key)+"/comment"), req, &resp, false); err != nil {
		return nil, err
	}
	return &resp, nil
}

// AssignWork appends an assigned/unassigned event for a membership subject.
func (c *Client) AssignWork(ctx context.Context, key, subject string, unassign bool) (*WorkMutationResponse, error) {
	var resp WorkMutationResponse
	req := struct {
		Subject  string `json:"subject"`
		Unassign bool   `json:"unassign,omitempty"`
	}{Subject: subject, Unassign: unassign}
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/tasks/"+urlSegment(key)+"/assign"), req, &resp, false); err != nil {
		return nil, err
	}
	return &resp, nil
}

// EditWorkContract appends a contract_edited event.
func (c *Client) EditWorkContract(ctx context.Context, key string, contract WorkContract) (*WorkMutationResponse, error) {
	var resp WorkMutationResponse
	req := struct {
		Contract WorkContract `json:"contract"`
	}{Contract: contract}
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/tasks/"+urlSegment(key)+"/contract"), req, &resp, false); err != nil {
		return nil, err
	}
	return &resp, nil
}
