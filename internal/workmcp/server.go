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

// Tools returns the closed tool surface. Note what is absent: no
// task_update_status (no lifecycle write exists anywhere), no pin.
func Tools() []mcpserve.ToolDef {
	contractSchema := obj(map[string]interface{}{
		"goal":     str("one or two sentences; the brief's first line"),
		"affects":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "catalog component keys"},
		"doneWhen": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		"gates":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "checks verified from orun execution truth"},
		"deps":     map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
	})
	return []mcpserve.ToolDef{
		{Name: "work_query", Description: "The workspace lens: specs with progress, tasks with DERIVED lifecycle and its evidence, the drift inbox, claim suggestions. Nothing returned is a stored status.", InputSchema: obj(map[string]interface{}{})},
		{Name: "work_get", Description: "One task: envelope, contract, and the fold's lifecycle with evidence.", InputSchema: obj(map[string]interface{}{"key": str("task key, e.g. ORN-142")}, "key")},
		{Name: "spec_get", Description: "The frozen brief: a content-addressed SpecSnapshot (intent only — contracts and docs, never a rung or assignee). Implement against exactly this.", InputSchema: obj(map[string]interface{}{"spec": str("spec slug")}, "spec")},
		{Name: "work_timeline", Description: "The unified timeline for one item: both logs (what people said, what the world did) interleaved by time — evidence attached, read-only.", InputSchema: obj(map[string]interface{}{"key": str("task or spec key")}, "key")},
		{Name: "spec_doc", Description: "A spec's cloud document revision (content-addressed, V3-2; latest when rev is omitted) — read-only.", InputSchema: obj(map[string]interface{}{"spec": str("spec slug"), "rev": str("revision digest sha256:<hex> (optional)")}, "spec")},
		{Name: "task_create", Description: "Create a task (e.g. discovered follow-up work) through the one mutator surface.", InputSchema: obj(map[string]interface{}{"prefix": str("task-key prefix, 2–5 uppercase"), "title": str("task title"), "spec": str("parent spec slug (optional)"), "contract": contractSchema}, "prefix", "title")},
		{Name: "task_comment", Description: "Append a comment to a task's coordination log.", InputSchema: obj(map[string]interface{}{"key": str("task key"), "body": str("comment body")}, "key", "body")},
		{Name: "task_assign", Description: "Assign a membership subject (self-assignment claims work).", InputSchema: obj(map[string]interface{}{"key": str("task key"), "subject": str("membership subject id (usr_/sp_/team_)")}, "key", "subject")},
		{Name: "contract_propose", Description: "Propose a contract change: applied through the mutators AND flagged with a review comment — an agent cannot quietly redefine its own definition of done.", InputSchema: obj(map[string]interface{}{"key": str("task key"), "contract": contractSchema}, "key", "contract")},
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
			Prefix   string                    `json:"prefix"`
			Title    string                    `json:"title"`
			Spec     string                    `json:"spec"`
			Contract *remotestate.WorkContract `json:"contract"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Prefix == "" || a.Title == "" {
			return nil, fmt.Errorf("task_create: prefix and title are required")
		}
		out, err := s.API.CreateWorkTask(ctx, remotestate.CreateWorkTaskRequest{
			Prefix: a.Prefix, Title: a.Title, SpecKey: a.Spec, Contract: a.Contract,
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

	default:
		return nil, fmt.Errorf("unknown tool %s", name)
	}
}
