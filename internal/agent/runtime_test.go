package agent

import (
	"context"
	"testing"

	"github.com/sourceplane/orun/internal/agent/driver"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/worklens"
)

func TestBriefDeterministicAndSealed(t *testing.T) {
	ctx := context.Background()
	in := BriefInput{
		RunKind:  nodes.RunKindImplementation,
		Task:     "ORN-142",
		Persona:  []byte("# Implementer\n\npersona\n"),
		Contract: &worklens.Contract{Goal: "sweep leases", Affects: []string{"a/b/c"}, DoneWhen: []string{"green"}, Gates: []string{"parity"}},
		Affected: []string{"a/b/c", "a/b/d"},
	}
	a1, err := AssembleBrief(ctx, objectstore.NewMemStore(objectstore.AlgoSHA256), in)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := AssembleBrief(ctx, objectstore.NewMemStore(objectstore.AlgoSHA256), in)
	if err != nil {
		t.Fatal(err)
	}
	if a1.ID != a2.ID {
		t.Fatalf("brief not deterministic: %s vs %s", a1.ID, a2.ID)
	}
	// Instructions layer literacy + persona + contract.
	for _, want := range []string{"orun base literacy", "# Implementer", "sweep leases", "ORN-142", "parity"} {
		if !contains(a1.Instructions, want) {
			t.Fatalf("instructions missing %q", want)
		}
	}
	if a1.Node.Affected == "" || a1.Node.Literacy == "" || a1.Node.Instructions == "" {
		t.Fatalf("brief node refs not sealed: %+v", a1.Node)
	}
}

func TestRunLoopStubProducesSealedSession(t *testing.T) {
	ctx := context.Background()
	store := objectstore.NewMemStore(objectstore.AlgoSHA256)
	brief, err := AssembleBrief(ctx, store, BriefInput{RunKind: nodes.RunKindImplementation, Task: "ORN-9"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(ctx, store, RunOptions{
		SessionID: "as_test1",
		Driver:    &driver.Stub{},
		Brief:     brief,
		Branch:    "agent/ORN-9-x",
		Policy:    NewToolPolicy(nodes.AgentToolPolicy{Allow: []string{"catalog_affected"}, Deny: []string{"*"}}),
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Outcome.Status != "completed" || res.Outcome.PR == "" {
		t.Fatalf("outcome = %+v", res.Outcome)
	}
	if len(res.Segments) == 0 {
		t.Fatal("no sealed segments")
	}
	// The sealed segment replays the transcript: an artifact event carries the PR.
	_, body, err := store.Get(ctx, objectstore.ObjectID(res.Segments[0]))
	if err != nil {
		t.Fatal(err)
	}
	seg, err := nodes.Decode[nodes.AgentSessionSegment](body)
	if err != nil {
		t.Fatal(err)
	}
	var sawArtifact, sawToolCall bool
	for _, e := range seg.Entries {
		if e.Kind == nodes.SessionEventArtifactProduced {
			sawArtifact = true
		}
		if e.Kind == nodes.SessionEventToolCall {
			sawToolCall = true
		}
		if e.Kind == "status_asserted" || e.Kind == "lifecycle_set" {
			t.Fatalf("forbidden event kind in log: %s", e.Kind)
		}
	}
	if !sawArtifact || !sawToolCall {
		t.Fatalf("transcript incomplete: artifact=%v toolcall=%v", sawArtifact, sawToolCall)
	}
	if err := seg.Validate(); err != nil {
		t.Fatalf("sealed segment invalid: %v", err)
	}
}

func TestRunLoopDeniedAndAskTools(t *testing.T) {
	ctx := context.Background()
	store := objectstore.NewMemStore(objectstore.AlgoSHA256)
	brief, _ := AssembleBrief(ctx, store, BriefInput{RunKind: nodes.RunKindImplementation, Task: "ORN-1"})

	script := []driver.Event{
		{Kind: driver.EventToolCall, Text: "x", Fields: map[string]any{"tool": "danger_delete"}},
		{Kind: driver.EventApproval, Text: "may I propose a contract?", RequestID: "r1"},
		{Kind: driver.EventDone, Fields: map[string]any{"status": "completed"}},
	}
	approvals := 0
	res, err := Run(ctx, store, RunOptions{
		SessionID: "as_test2",
		Driver:    &driver.Stub{Script: script},
		Brief:     brief,
		Policy:    NewToolPolicy(nodes.AgentToolPolicy{Allow: []string{"work_get"}, Ask: []string{"contract_propose"}, Deny: []string{"*"}}),
		Approve: func(driver.Event) driver.Verdict {
			approvals++
			return driver.Verdict{Approved: true, Reason: "ok"}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if approvals != 1 {
		t.Fatalf("approvals=%d, want 1", approvals)
	}
	_, body, _ := store.Get(ctx, objectstore.ObjectID(res.Segments[0]))
	seg, _ := nodes.Decode[nodes.AgentSessionSegment](body)
	var deniedLogged, approvalResolved bool
	for _, e := range seg.Entries {
		if e.Kind == nodes.SessionEventToolCall {
			if d, _ := e.Payload["decision"].(string); d == "deny" {
				deniedLogged = true
			}
		}
		if e.Kind == nodes.SessionEventApprovalResolved {
			if ok, _ := e.Payload["approved"].(bool); ok {
				approvalResolved = true
			}
		}
	}
	if !deniedLogged {
		t.Error("denied tool call not logged as deny")
	}
	if !approvalResolved {
		t.Error("approval verdict not logged")
	}
}

func TestToolPolicyPrecedence(t *testing.T) {
	p := NewToolPolicy(nodes.AgentToolPolicy{Allow: []string{"work_get", "catalog_*"}, Ask: []string{"contract_propose"}, Deny: []string{"*"}})
	cases := map[string]Decision{
		"work_get":         DecisionAllow, // exact allow beats wildcard deny
		"catalog_affected": DecisionAllow, // wildcard allow beats wildcard deny
		"contract_propose": DecisionAsk,   // exact ask
		"rm_rf":            DecisionDeny,  // only matches deny *
	}
	for tool, want := range cases {
		if got := p.Decide(tool); got != want {
			t.Errorf("Decide(%q)=%v want %v", tool, got, want)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
