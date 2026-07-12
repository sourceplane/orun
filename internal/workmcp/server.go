// Package workmcp is the work-plane tool provider of the orun MCP
// (orun-work v2 WP5, mounted by orun-mcp UM0): the agent surface,
// policy-identical to the console. The stdio JSON-RPC transport lives in
// internal/mcpserve; this package supplies the tools.
//
// The tool surface is the whole point (agents-and-mcp.md): reads return the
// fold's output WITH evidence; the write surface is four tools — task_create,
// task_comment, task_assign, contract_propose — and deliberately nothing
// else. There is NO lifecycle write tool (lifecycle is a derived query,
// WP-3: the category "agent lies about status" is unrepresentable) and NO
// pin tool (pins are human-only, WP-10; the cloud mutator also rejects agent
// pins server-side — defense in depth, not client-side trust).
package workmcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sourceplane/orun/internal/mcpserve"
	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/workbrief"
	"github.com/sourceplane/orun/internal/worklens"
)

// WorkAPI is the seam onto the cloud work plane; *remotestate.Client
// implements it. Every write goes through the same mutators as the console
// keyboard (WD/WP one-write-path heritage).
type WorkAPI interface {
	GetWorkSummary(ctx context.Context) (*remotestate.WorkSummary, error)
	GetWorkTimeline(ctx context.Context, key string) (*remotestate.WorkTimeline, error)
	GetWorkDoc(ctx context.Context, specKey, rev string) (*remotestate.WorkDoc, error)
	CreateWorkTask(ctx context.Context, req remotestate.CreateWorkTaskRequest) (*remotestate.WorkMutationResponse, error)
	CommentWork(ctx context.Context, key, body string) (*remotestate.WorkMutationResponse, error)
	AssignWork(ctx context.Context, key, subject string, unassign bool) (*remotestate.WorkMutationResponse, error)
	EditWorkContract(ctx context.Context, key string, contract remotestate.WorkContract) (*remotestate.WorkMutationResponse, error)
	// v4 (WH5) — the hierarchy legs. Reads only for decisions: there is no
	// approve/adopt call on this seam at all (V4-2).
	GetEpicBrief(ctx context.Context, epicKey, id string) (*remotestate.WorkEpicBrief, error)
	GetEpicMilestones(ctx context.Context, epicKey string) (*remotestate.WorkMilestonesView, error)
	GetWorkDesign(ctx context.Context, key string) (*remotestate.WorkDesignView, error)
	GetWorkRollups(ctx context.Context, initiativeKey string) (*remotestate.WorkRollups, error)
	CreateWorkDesign(ctx context.Context, initiativeKey string, req remotestate.CreateWorkDesignRequest) (*remotestate.WorkMutationResponse, error)
	RegenerateWorkTasks(ctx context.Context, epicKey, milestone string, req remotestate.RegenerateWorkTasksRequest) (*remotestate.RegenerateWorkTasksResponse, error)
}

// Server is the work-plane mcpserve.ToolProvider for one workspace-scoped
// client.
type Server struct {
	API       WorkAPI
	Workspace string
}

func obj(props map[string]interface{}, required ...string) map[string]interface{} {
	s := map[string]interface{}{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func str(desc string) map[string]interface{} {
	return map[string]interface{}{"type": "string", "description": desc}
}

// ToolNames returns the closed tool surface's names, in definition order —
// the list the agent runtime's MCP config writer filters through tool policy
// (internal/agent/mcp.go).
func ToolNames() []string {
	defs := Tools()
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Name
	}
	return names
}

// ReadOnly reports whether name is one of the surface's read tools —
// display metadata for `orun mcp tools`, derived from the wire annotations
// (UM4) so the display can never disagree with what a client sees.
func ReadOnly(name string) bool {
	for _, t := range Tools() {
		if t.Name == name {
			ro, _ := t.Annotations["readOnlyHint"].(bool)
			return ro
		}
	}
	return false
}

// Tools returns the closed tool surface. Note what is absent: no
// task_update_status (no lifecycle write exists anywhere), no pin.
//
// Wire annotations (orun-mcp UM4). The reads are the plain truth:
// readOnly/non-destructive/idempotent. The write tools are readOnly:false,
// destructive:false (mutator-shaped per WP-6 — every write appends or
// applies through the one mutator surface; nothing on this plane deletes
// or irreversibly overwrites), and idempotent:FALSE: unlike the platform
// writes (per-attempt Idempotency-Key, UM2), the work mutators carry no
// idempotency key and every call appends a new event to the coordination
// log — a blind retry of task_create/task_comment/design_propose
// duplicates the artifact, task_assign/contract_propose append duplicate
// events, and task_regenerate mints fresh task keys per run. Truthful
// hints over optimistic ones: a strict client should confirm before
// replaying a work write.
func Tools() []mcpserve.ToolDef {
	readAnn := mcpserve.Annotations(true, false, true)
	writeAnn := mcpserve.Annotations(false, false, false)
	contractSchema := obj(map[string]interface{}{
		"goal":     str("one or two sentences; the brief's first line"),
		"affects":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "catalog component keys"},
		"doneWhen": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		"gates":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "checks verified from orun execution truth"},
		"deps":     map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
	})
	return []mcpserve.ToolDef{
		{Name: "work_query", Description: "The workspace lens: specs with progress, tasks with DERIVED lifecycle and its evidence, the drift inbox, claim suggestions. Nothing returned is a stored status.", InputSchema: obj(map[string]interface{}{}), Annotations: readAnn},
		{Name: "work_get", Description: "One task: envelope, contract, and the fold's lifecycle with evidence.", InputSchema: obj(map[string]interface{}{"key": str("task key, e.g. ORN-142")}, "key"), Annotations: readAnn},
		{Name: "spec_get", Description: "The frozen brief: a content-addressed SpecSnapshot (intent only — contracts and docs, never a rung or assignee). Implement against exactly this.", InputSchema: obj(map[string]interface{}{"spec": str("spec slug")}, "spec"), Annotations: readAnn},
		{Name: "work_timeline", Description: "The unified timeline for one item: both logs (what people said, what the world did) interleaved by time — evidence attached, read-only.", InputSchema: obj(map[string]interface{}{"key": str("task or spec key")}, "key"), Annotations: readAnn},
		{Name: "spec_doc", Description: "A spec's cloud document revision (content-addressed, V3-2; latest when rev is omitted) — read-only.", InputSchema: obj(map[string]interface{}{"spec": str("spec slug"), "rev": str("revision digest sha256:<hex> (optional)")}, "spec"), Annotations: readAnn},
		{Name: "task_create", Description: "Create a task (e.g. discovered follow-up work) through the one mutator surface.", InputSchema: obj(map[string]interface{}{"prefix": str("task-key prefix, 2–5 uppercase"), "title": str("task title"), "spec": str("parent spec slug (optional)"), "milestone": str("milestone key within spec (optional, v4)"), "contract": contractSchema}, "prefix", "title"), Annotations: writeAnn},
		{Name: "task_comment", Description: "Append a comment to a task's coordination log.", InputSchema: obj(map[string]interface{}{"key": str("task key"), "body": str("comment body")}, "key", "body"), Annotations: writeAnn},
		{Name: "task_assign", Description: "Assign a membership subject (self-assignment claims work).", InputSchema: obj(map[string]interface{}{"key": str("task key"), "subject": str("membership subject id (usr_/sp_/team_)")}, "key", "subject"), Annotations: writeAnn},
		{Name: "contract_propose", Description: "Propose a contract change: applied through the mutators AND flagged with a review comment — an agent cannot quietly redefine its own definition of done.", InputSchema: obj(map[string]interface{}{"key": str("task key"), "contract": contractSchema}, "key", "contract"), Annotations: writeAnn},
		// v4 (WH5) — the hierarchy surface. Note what is STILL absent: no
		// approve tool, no adopt tool (human-only decisions, V4-2), and
		// still no status or pin.
		{Name: "epic_brief", Description: "The frozen brief an APPROVAL sealed: EpicSnapshot canonical bytes + content id (doc ref, milestone ladder + hash, task contracts, approval record). Implement against exactly this; verify sha256(bytes) == id. An unapproved epic has no brief.", InputSchema: obj(map[string]interface{}{"epic": str("epic slug"), "id": str("pinned snapshot id sha256:<hex> (optional; latest otherwise)")}, "epic"), Annotations: readAnn},
		{Name: "milestone_get", Description: "One epic's milestone ladder: authored goals/done-when plus DERIVED progress per milestone — read-only.", InputSchema: obj(map[string]interface{}{"epic": str("epic slug")}, "epic"), Annotations: readAnn},
		{Name: "design_get", Description: "One design: doc pointer, sealed context (what it assumed), structured proposal, and folded intent state — read-only.", InputSchema: obj(map[string]interface{}{"key": str("design key, e.g. DSG-1")}, "key"), Annotations: readAnn},
		{Name: "initiative_get", Description: "One initiative's DERIVED rollup: health with named evidence, progress, per-epic intent + execution. Nothing returned is enterable.", InputSchema: obj(map[string]interface{}{"initiative": str("initiative key")}, "initiative"), Annotations: readAnn},
		{Name: "design_propose", Description: "Create a Draft design under an initiative: a document reference plus a structured proposal (epics → milestones → task skeletons). A design is a PROPOSAL — humans review, compare, and adopt; adoption mints epics and is not available here.", InputSchema: obj(map[string]interface{}{"initiative": str("initiative key"), "title": str("design title"), "docRef": str("design doc revision sha256:<hex> (optional)"), "proposal": map[string]interface{}{"type": "object", "description": "{epics: [{slug, title, docSeed?, milestones[], taskSkeletons[]}]}"}}, "initiative", "title"), Annotations: writeAnn},
		{Name: "task_regenerate", Description: "Re-plan one milestone in one verdict batch: PLANNED (draft/ready) tasks cancel, in-flight tasks survive, and every proposed contract is applied AND flagged for human review. Tasks are implementation detail (V4-5) — this never touches the epic's approval.", InputSchema: obj(map[string]interface{}{"epic": str("epic slug"), "milestone": str("milestone key, e.g. M1"), "prefix": str("task-key prefix (default WK)"), "tasks": map[string]interface{}{"type": "array", "items": obj(map[string]interface{}{"title": str("task title"), "contract": contractSchema}, "title"), "description": "the replacement plan"}}, "epic", "milestone", "tasks"), Annotations: writeAnn},
	}
}

// Tools implements mcpserve.ToolProvider.
func (s *Server) Tools() []mcpserve.ToolDef { return Tools() }

// Call implements mcpserve.ToolProvider: owned=false for names outside the
// work roster (another provider's business), and every owned failure maps
// to an isError result — the mutator's verdict is something the agent
// should reason about, not a protocol fault.
func (s *Server) Call(ctx context.Context, name string, args json.RawMessage) (mcpserve.Result, bool) {
	if !toolNames[name] {
		return nil, false
	}
	result, err := s.call(ctx, name, args)
	if err != nil {
		return toolText(fmt.Sprintf("error: %v", err), true), true
	}
	return result, true
}

// toolNames is the owned roster, derived from Tools() so ownership can
// never drift from the advertised surface.
var toolNames = func() map[string]bool {
	m := map[string]bool{}
	for _, t := range Tools() {
		m[t.Name] = true
	}
	return m
}()

func toolText(text string, isErr bool) mcpserve.Result {
	return mcpserve.TextResult(text, isErr)
}

func toolJSON(v interface{}) (mcpserve.Result, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return toolText(string(b), false), nil
}

func (s *Server) call(ctx context.Context, name string, args json.RawMessage) (mcpserve.Result, error) {
	switch name {
	case "work_query":
		summary, err := s.API.GetWorkSummary(ctx)
		if err != nil {
			return nil, err
		}
		return toolJSON(summary)

	case "work_get":
		var a struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Key == "" {
			return nil, fmt.Errorf("work_get: key is required")
		}
		summary, err := s.API.GetWorkSummary(ctx)
		if err != nil {
			return nil, err
		}
		for _, t := range summary.Tasks {
			if t.Key == a.Key {
				return toolJSON(t)
			}
		}
		return nil, fmt.Errorf("work_get: unknown task %s", a.Key)

	case "spec_get":
		var a struct {
			Spec string `json:"spec"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Spec == "" {
			return nil, fmt.Errorf("spec_get: spec is required")
		}
		summary, err := s.API.GetWorkSummary(ctx)
		if err != nil {
			return nil, err
		}
		snap, err := workbrief.SnapshotFromSummary(s.Workspace, a.Spec, summary)
		if err != nil {
			return nil, err
		}
		id, canonical, err := worklens.SealSpecSnapshot(*snap)
		if err != nil {
			return nil, err
		}
		return toolText(fmt.Sprintf("%s\n%s", id, canonical), false), nil

	case "work_timeline":
		var a struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Key == "" {
			return nil, fmt.Errorf("work_timeline: key is required")
		}
		timeline, err := s.API.GetWorkTimeline(ctx, a.Key)
		if err != nil {
			return nil, err
		}
		return toolJSON(timeline)

	case "spec_doc":
		var a struct {
			Spec string `json:"spec"`
			Rev  string `json:"rev"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Spec == "" {
			return nil, fmt.Errorf("spec_doc: spec is required")
		}
		doc, err := s.API.GetWorkDoc(ctx, a.Spec, a.Rev)
		if err != nil {
			return nil, err
		}
		return toolText(fmt.Sprintf("%s (parent %s)\n\n%s", doc.Revision, doc.Parent, doc.Body), false), nil

	case "task_create":
		var a struct {
			Prefix    string                    `json:"prefix"`
			Title     string                    `json:"title"`
			Spec      string                    `json:"spec"`
			Milestone string                    `json:"milestone"`
			Contract  *remotestate.WorkContract `json:"contract"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Prefix == "" || a.Title == "" {
			return nil, fmt.Errorf("task_create: prefix and title are required")
		}
		out, err := s.API.CreateWorkTask(ctx, remotestate.CreateWorkTaskRequest{
			Prefix: a.Prefix, Title: a.Title, SpecKey: a.Spec, Milestone: a.Milestone, Contract: a.Contract,
		})
		if err != nil {
			return nil, err
		}
		return toolText(fmt.Sprintf("created %s (event seq %d)", out.Key, out.Seq), false), nil

	case "task_comment":
		var a struct {
			Key  string `json:"key"`
			Body string `json:"body"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Key == "" || a.Body == "" {
			return nil, fmt.Errorf("task_comment: key and body are required")
		}
		out, err := s.API.CommentWork(ctx, a.Key, a.Body)
		if err != nil {
			return nil, err
		}
		return toolText(fmt.Sprintf("commented on %s (event seq %d)", out.Key, out.Seq), false), nil

	case "task_assign":
		var a struct {
			Key     string `json:"key"`
			Subject string `json:"subject"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Key == "" || a.Subject == "" {
			return nil, fmt.Errorf("task_assign: key and subject are required")
		}
		out, err := s.API.AssignWork(ctx, a.Key, a.Subject, false)
		if err != nil {
			return nil, err
		}
		return toolText(fmt.Sprintf("assigned %s to %s (event seq %d)", out.Key, a.Subject, out.Seq), false), nil

	case "contract_propose":
		var a struct {
			Key      string                   `json:"key"`
			Contract remotestate.WorkContract `json:"contract"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Key == "" {
			return nil, fmt.Errorf("contract_propose: key and contract are required")
		}
		out, err := s.API.EditWorkContract(ctx, a.Key, a.Contract)
		if err != nil {
			return nil, err
		}
		// The flag: a proposal is applied AND surfaced for human review —
		// an agent cannot quietly redefine its own definition of done.
		if _, err := s.API.CommentWork(ctx, a.Key, "contract proposed via MCP — human review requested"); err != nil {
			return nil, fmt.Errorf("contract applied (seq %d) but review flag failed: %w", out.Seq, err)
		}
		return toolText(fmt.Sprintf("contract proposed on %s (event seq %d); flagged for human review", out.Key, out.Seq), false), nil

	case "epic_brief":
		var a struct {
			Epic string `json:"epic"`
			ID   string `json:"id"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Epic == "" {
			return nil, fmt.Errorf("epic_brief: epic is required")
		}
		brief, err := s.API.GetEpicBrief(ctx, a.Epic, a.ID)
		if err != nil {
			return nil, err
		}
		if err := worklens.VerifySealedBytes(brief.ID, []byte(brief.Canonical)); err != nil {
			return nil, fmt.Errorf("epic_brief: %w", err)
		}
		return toolText(fmt.Sprintf("%s\n%s", brief.ID, brief.Canonical), false), nil

	case "milestone_get":
		var a struct {
			Epic string `json:"epic"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Epic == "" {
			return nil, fmt.Errorf("milestone_get: epic is required")
		}
		view, err := s.API.GetEpicMilestones(ctx, a.Epic)
		if err != nil {
			return nil, err
		}
		return toolJSON(view)

	case "design_get":
		var a struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Key == "" {
			return nil, fmt.Errorf("design_get: key is required")
		}
		design, err := s.API.GetWorkDesign(ctx, a.Key)
		if err != nil {
			return nil, err
		}
		return toolJSON(design)

	case "initiative_get":
		var a struct {
			Initiative string `json:"initiative"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Initiative == "" {
			return nil, fmt.Errorf("initiative_get: initiative is required")
		}
		rollups, err := s.API.GetWorkRollups(ctx, a.Initiative)
		if err != nil {
			return nil, err
		}
		return toolJSON(rollups)

	case "design_propose":
		var a struct {
			Initiative string          `json:"initiative"`
			Title      string          `json:"title"`
			DocRef     string          `json:"docRef"`
			Proposal   json.RawMessage `json:"proposal"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Initiative == "" || a.Title == "" {
			return nil, fmt.Errorf("design_propose: initiative and title are required")
		}
		out, err := s.API.CreateWorkDesign(ctx, a.Initiative, remotestate.CreateWorkDesignRequest{
			Title: a.Title, DocRef: a.DocRef, Proposal: a.Proposal,
		})
		if err != nil {
			return nil, err
		}
		return toolText(fmt.Sprintf("proposed design %s (event seq %d) — a human reviews, compares, and adopts; adoption mints the epics", out.Key, out.Seq), false), nil

	case "task_regenerate":
		var a struct {
			Epic      string `json:"epic"`
			Milestone string `json:"milestone"`
			Prefix    string `json:"prefix"`
			Tasks     []struct {
				Title    string                    `json:"title"`
				Contract *remotestate.WorkContract `json:"contract"`
			} `json:"tasks"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Epic == "" || a.Milestone == "" || len(a.Tasks) == 0 {
			return nil, fmt.Errorf("task_regenerate: epic, milestone, and tasks are required")
		}
		req := remotestate.RegenerateWorkTasksRequest{Prefix: a.Prefix}
		for _, t := range a.Tasks {
			req.Tasks = append(req.Tasks, struct {
				Title    string                    `json:"title"`
				Contract *remotestate.WorkContract `json:"contract,omitempty"`
			}{Title: t.Title, Contract: t.Contract})
		}
		out, err := s.API.RegenerateWorkTasks(ctx, a.Epic, a.Milestone, req)
		if err != nil {
			return nil, err
		}
		return toolText(fmt.Sprintf("regenerated %s/%s: created %v, canceled %v, kept in-flight %v — proposed contracts are flagged for human review", a.Epic, a.Milestone, out.Created, out.Canceled, out.Kept), false), nil

	default:
		return nil, fmt.Errorf("unknown tool %s", name)
	}
}
