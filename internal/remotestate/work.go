package remotestate

import (
	"context"
	"encoding/json"
	"net/http"
)

// Work-plane client (orun-work v2 WP1) — the CLI's seam onto the cloud work
// API (/v1/organizations/{org}/work). Wire shapes mirror the platform's
// @saas/contracts/work. Deliberately small: import apply + the fold summary;
// lifecycle is derived server-side on every read and there is no
// status-writing call to offer (WP-3).

// WorkImportSpec mirrors the CLI import plan's spec entry.
type WorkImportSpec struct {
	Slug       string `json:"slug"`
	Title      string `json:"title"`
	DocPath    string `json:"docPath"`
	DocSHA256  string `json:"docSha256"`
	PlanPath   string `json:"planPath,omitempty"`
	Initiative string `json:"initiative,omitempty"`
}

// WorkImportTask mirrors the CLI import plan's task entry. No lifecycle
// field exists by design — rungs derive from observations after apply.
type WorkImportTask struct {
	SpecSlug    string        `json:"specSlug"`
	MilestoneID string        `json:"milestoneId"`
	Milestone   string        `json:"milestone,omitempty"`
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

// WorkImportInitiative mirrors the plan's initiative entry (v4 WH6).
type WorkImportInitiative struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

// WorkImportMilestone mirrors the plan's milestone entry (v4 WH6).
type WorkImportMilestone struct {
	SpecSlug string   `json:"specSlug"`
	Key      string   `json:"key"`
	Title    string   `json:"title"`
	Goal     string   `json:"goal,omitempty"`
	DoneWhen []string `json:"doneWhen,omitempty"`
	Ordinal  int      `json:"ordinal"`
}

// WorkImportRequest is the apply body (the dry-run plan, verbatim).
type WorkImportRequest struct {
	Workspace   string                 `json:"workspace"`
	Root        string                 `json:"root"`
	Prefix      string                 `json:"prefix,omitempty"`
	Initiatives []WorkImportInitiative `json:"initiatives,omitempty"`
	Specs       []WorkImportSpec       `json:"specs"`
	Milestones  []WorkImportMilestone  `json:"milestones,omitempty"`
	Tasks       []WorkImportTask       `json:"tasks"`
}

// WorkImportResponse reports apply counts; re-imports skip idempotently.
type WorkImportResponse struct {
	SpecsCreated  int `json:"specsCreated"`
	SpecsSkipped  int `json:"specsSkipped"`
	TasksCreated  int `json:"tasksCreated"`
	TasksSkipped  int `json:"tasksSkipped"`
	// v4 (WH6) — additive; zero on pre-v4 servers.
	InitiativesCreated int `json:"initiativesCreated,omitempty"`
	InitiativesSkipped int `json:"initiativesSkipped,omitempty"`
	MilestonesCreated  int `json:"milestonesCreated,omitempty"`
	MilestonesSkipped  int `json:"milestonesSkipped,omitempty"`
	TasksMigrated      int `json:"tasksMigrated,omitempty"`
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
	Prefix    string        `json:"prefix"`
	Title     string        `json:"title"`
	SpecKey   string        `json:"specKey,omitempty"`
	Milestone string        `json:"milestone,omitempty"` // v4: lands inside SpecKey's ladder
	Contract  *WorkContract `json:"contract,omitempty"`
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

// ── Read-only v3 surfaces the MCP exposes (PM5) ──────────────────────────────

// WorkTimelineEntry is one interleaved entry of the two logs — a
// coordination event or an observation, by time. Payloads stay raw: the MCP
// hands them to the agent verbatim; nothing here is interpreted client-side.
type WorkTimelineEntry struct {
	At          string          `json:"at"`
	Type        string          `json:"type"` // "event" | "observation"
	Event       json.RawMessage `json:"event,omitempty"`
	Observation json.RawMessage `json:"observation,omitempty"`
}

// WorkTimeline is the unified timeline for one item (PM1 route).
type WorkTimeline struct {
	Key     string              `json:"key"`
	Entries []WorkTimelineEntry `json:"entries"`
}

// GetWorkTimeline fetches both logs interleaved for one task/spec key.
func (c *Client) GetWorkTimeline(ctx context.Context, key string) (*WorkTimeline, error) {
	var resp WorkTimeline
	if err := c.doJSON(ctx, http.MethodGet, c.workPath("/timeline/"+urlSegment(key)), nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// WorkDoc is one content-addressed cloud document revision (V3-2: the
// digest form equals the imported doc_ref).
type WorkDoc struct {
	Revision  string `json:"revision"`
	Parent    string `json:"parent,omitempty"`
	SpecKey   string `json:"specKey"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
}

// WorkMilestoneView mirrors the platform's milestone view: authored fields
// plus DERIVED progress (never entered — V4-4).
type WorkMilestoneView struct {
	Key        string         `json:"key"`
	Title      string         `json:"title"`
	Goal       string         `json:"goal,omitempty"`
	DoneWhen   []string       `json:"doneWhen,omitempty"`
	TargetDate string         `json:"targetDate,omitempty"`
	Ordinal    int            `json:"ordinal"`
	Progress   map[string]int `json:"progress,omitempty"`
	Total      int            `json:"total,omitempty"`
	Complete   int            `json:"complete,omitempty"`
}

// WorkMilestonesView is one epic's ladder with derived progress.
type WorkMilestonesView struct {
	Epic        string              `json:"epic"`
	Milestones  []WorkMilestoneView `json:"milestones"`
	Unscheduled *struct {
		Total    int `json:"total"`
		Complete int `json:"complete"`
	} `json:"unscheduled,omitempty"`
}

// GetEpicMilestones fetches an epic's milestone ladder with derived
// per-milestone progress.
func (c *Client) GetEpicMilestones(ctx context.Context, epicKey string) (*WorkMilestonesView, error) {
	var resp WorkMilestonesView
	if err := c.doJSON(ctx, http.MethodGet, c.workPath("/epics/"+urlSegment(epicKey)+"/milestones"), nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// WorkDesignView mirrors the platform's design view: the doc chain pointer,
// the sealed context, the structured proposal, and the FOLDED intent state.
type WorkDesignView struct {
	Key        string          `json:"key"`
	Initiative string          `json:"initiative"`
	Title      string          `json:"title"`
	DocRef     string          `json:"docRef,omitempty"`
	Context    json.RawMessage `json:"context,omitempty"`
	Proposal   json.RawMessage `json:"proposal,omitempty"`
	CreatedBy  WorkActor       `json:"createdBy"`
	CreatedAt  string          `json:"createdAt,omitempty"`
	Intent     json.RawMessage `json:"intent,omitempty"`
}

// GetWorkDesign fetches one design.
func (c *Client) GetWorkDesign(ctx context.Context, key string) (*WorkDesignView, error) {
	var resp WorkDesignView
	if err := c.doJSON(ctx, http.MethodGet, c.workPath("/designs/"+urlSegment(key)), nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CreateWorkDesignRequest mirrors the platform's CreateWorkDesignRequest —
// a design is a PROPOSAL (agents may author one); adoption stays human-only.
type CreateWorkDesignRequest struct {
	Title    string          `json:"title"`
	DocRef   string          `json:"docRef,omitempty"`
	Proposal json.RawMessage `json:"proposal,omitempty"`
	Catalog  string          `json:"catalog,omitempty"`
}

// CreateWorkDesign creates a Draft design under an initiative; the cloud
// seals the context (catalog digest + log cursors) server-side.
func (c *Client) CreateWorkDesign(ctx context.Context, initiativeKey string, req CreateWorkDesignRequest) (*WorkMutationResponse, error) {
	var resp WorkMutationResponse
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/initiatives/"+urlSegment(initiativeKey)+"/designs"), req, &resp, false); err != nil {
		return nil, err
	}
	return &resp, nil
}

// WorkRollups mirrors the platform's initiative rollup: derived health with
// named evidence plus per-epic intent + execution.
type WorkRollups struct {
	Initiative string          `json:"initiative"`
	Health     string          `json:"health"`
	Evidence   []string        `json:"evidence,omitempty"`
	Progress   map[string]int  `json:"progress"`
	Total      int             `json:"total"`
	Complete   int             `json:"complete"`
	Epics      json.RawMessage `json:"epics"`
}

// GetWorkRollups fetches one initiative's derived rollup.
func (c *Client) GetWorkRollups(ctx context.Context, initiativeKey string) (*WorkRollups, error) {
	var resp WorkRollups
	if err := c.doJSON(ctx, http.MethodGet, c.workPath("/rollups?initiative="+urlSegment(initiativeKey)), nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// RegenerateWorkTasksRequest mirrors the platform's request: the replacement
// plan for one milestone. Planned tasks cancel; in-flight tasks survive.
type RegenerateWorkTasksRequest struct {
	Tasks []struct {
		Title    string        `json:"title"`
		Contract *WorkContract `json:"contract,omitempty"`
	} `json:"tasks"`
	Prefix string `json:"prefix,omitempty"`
}

// RegenerateWorkTasksResponse reports the batch's one verdict.
type RegenerateWorkTasksResponse struct {
	Canceled []string `json:"canceled"`
	Kept     []string `json:"kept"`
	Created  []string `json:"created"`
}

// RegenerateWorkTasks re-plans one milestone in one verdict batch.
func (c *Client) RegenerateWorkTasks(ctx context.Context, epicKey, milestone string, req RegenerateWorkTasksRequest) (*RegenerateWorkTasksResponse, error) {
	var resp RegenerateWorkTasksResponse
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/epics/"+urlSegment(epicKey)+"/milestones/"+urlSegment(milestone)+"/regenerate"), req, &resp, false); err != nil {
		return nil, err
	}
	return &resp, nil
}

// WorkEpicBrief is the sealed brief approval minted (orun-work-v4 WH4):
// canonical bytes plus their content id. Verification is content addressing
// itself — sha256(Canonical) MUST equal ID; there is no second canonicalizer.
type WorkEpicBrief struct {
	ID        string `json:"id"`
	Subject   string `json:"subject"`
	Canonical string `json:"canonical"`
	CreatedAt string `json:"createdAt,omitempty"`
}

// GetEpicBrief fetches the sealed EpicSnapshot for an epic — latest when id
// is empty, or the exact pinned snapshot.
func (c *Client) GetEpicBrief(ctx context.Context, epicKey, id string) (*WorkEpicBrief, error) {
	path := c.workPath("/epics/" + urlSegment(epicKey) + "/brief")
	if id != "" {
		path += "?id=" + urlSegment(id)
	}
	var resp WorkEpicBrief
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetWorkDoc fetches a spec's cloud document (latest when rev is empty).
func (c *Client) GetWorkDoc(ctx context.Context, specKey, rev string) (*WorkDoc, error) {
	path := c.workPath("/specs/" + urlSegment(specKey) + "/doc")
	if rev != "" {
		path += "?rev=" + urlSegment(rev)
	}
	var resp WorkDoc
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}
