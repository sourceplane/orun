package nodes

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/objectstore"
)

func testAgentType() AgentTypeSnapshot {
	return AgentTypeSnapshot{
		Kind:       KindAgentTypeSnapshot,
		APIVersion: apiVersionV1,
		Name:       "implementer",
		Harness:    "claude-code",
		Model:      "claude-opus-4-8",
		Runtime:    &AgentRuntime{Effort: "high", MaxTokens: 64000},
		Tools: AgentToolPolicy{
			Allow: []string{"work_get", "spec_get", "catalog_affected"},
			Ask:   []string{"contract_propose"},
			Deny:  []string{"*"},
		},
		MayAffect:       []string{"sourceplane/orun-cloud/billing-*"},
		AutonomyDefault: AutonomyAssist,
		Owner:           "sourceplane/team/payments",
	}
}

const testPersona = "# Implementer\n\nYou take one Ready task to a merged-quality PR.\n"
const testLiteracy = "# orun base literacy v1\n\nLifecycle is derived; you have no status-write tool.\n"

func TestAgentTypeAssembleAndPureID(t *testing.T) {
	ctx := context.Background()
	mem := objectstore.NewMemStore(objectstore.AlgoSHA256)

	id, err := AssembleAgentType(ctx, mem, testAgentType(), []byte(testPersona), "base-orun-literacy", []byte(testLiteracy))
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	pure, err := AgentTypeID(objectstore.AlgoSHA256, testAgentType(), []byte(testPersona), "base-orun-literacy", []byte(testLiteracy))
	if err != nil {
		t.Fatalf("pure id: %v", err)
	}
	if id != pure {
		t.Fatalf("pure id %s != assembled id %s", pure, id)
	}

	// The node is a tree bundling record + persona + literacy.
	entries, err := mem.GetTree(ctx, id)
	if err != nil {
		t.Fatalf("get tree: %v", err)
	}
	names := map[string]objectstore.ObjectID{}
	for _, e := range entries {
		names[e.Name] = e.ID
	}
	for _, want := range []string{"agent-type.json", "body.md", "base-literacy.md"} {
		if _, ok := names[want]; !ok {
			t.Fatalf("tree missing %s (have %v)", want, entries)
		}
	}

	// The record round-trips with BodyRef stamped to the persona blob and
	// Extends pinned to the literacy blob.
	_, recBytes, err := mem.Get(ctx, names["agent-type.json"])
	if err != nil {
		t.Fatalf("get record: %v", err)
	}
	rec, err := Decode[AgentTypeSnapshot](recBytes)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rec.BodyRef != string(names["body.md"]) {
		t.Fatalf("bodyRef %q != body blob %q", rec.BodyRef, names["body.md"])
	}
	if want := "base-orun-literacy@" + string(names["base-literacy.md"]); rec.Extends != want {
		t.Fatalf("extends %q, want %q", rec.Extends, want)
	}
	if err := rec.Validate(); err != nil {
		t.Fatalf("decoded record invalid: %v", err)
	}
	_, body, err := mem.Get(ctx, names["body.md"])
	if err != nil || string(body) != testPersona {
		t.Fatalf("persona not stored verbatim: %q err=%v", body, err)
	}
}

func TestAgentTypeDeterminism(t *testing.T) {
	// Same logical content assembled twice — and with differently-ordered
	// slices untouched — must yield one id; a persona edit must change it.
	a, err := AgentTypeID(objectstore.AlgoSHA256, testAgentType(), []byte(testPersona), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := AgentTypeID(objectstore.AlgoSHA256, testAgentType(), []byte(testPersona), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("identical inputs → %s vs %s", a, b)
	}
	c, err := AgentTypeID(objectstore.AlgoSHA256, testAgentType(), []byte(testPersona+"\nedited"), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if c == a {
		t.Fatal("persona edit did not change identity")
	}
	tuned := testAgentType()
	tuned.Model = "claude-sonnet-5"
	d, err := AgentTypeID(objectstore.AlgoSHA256, tuned, []byte(testPersona), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if d == a {
		t.Fatal("model re-tune did not change identity")
	}
}

func TestAgentTypeValidate(t *testing.T) {
	ctx := context.Background()
	mem := objectstore.NewMemStore(objectstore.AlgoSHA256)

	noOwner := testAgentType()
	noOwner.Owner = ""
	if _, err := AssembleAgentType(ctx, mem, noOwner, []byte(testPersona), "", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("ownerless type sealed: %v", err)
	}
	if _, err := AssembleAgentType(ctx, mem, testAgentType(), nil, "", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty persona sealed: %v", err)
	}
	badAutonomy := testAgentType()
	badAutonomy.AutonomyDefault = "yolo"
	if _, err := AssembleAgentType(ctx, mem, badAutonomy, []byte(testPersona), "", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad autonomy sealed: %v", err)
	}
	badName := testAgentType()
	badName.Name = "im/pl"
	if _, err := AssembleAgentType(ctx, mem, badName, []byte(testPersona), "", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad name sealed: %v", err)
	}
}

func testBlobID(t *testing.T, s *objectstore.MemStore, data string) string {
	t.Helper()
	id, err := s.PutBlob(context.Background(), []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	return string(id)
}

func TestAgentBrief(t *testing.T) {
	ctx := context.Background()
	mem := objectstore.NewMemStore(objectstore.AlgoSHA256)
	instr := testBlobID(t, mem, "instructions")

	b := AgentBrief{RunKind: RunKindImplementation, Task: "ORN-142", Instructions: instr}
	id, err := AssembleAgentBrief(ctx, mem, b)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	pure, err := AgentBriefID(objectstore.AlgoSHA256, b)
	if err != nil {
		t.Fatal(err)
	}
	if id != pure {
		t.Fatalf("pure %s != assembled %s", pure, id)
	}
	bad := b
	bad.RunKind = "vibe"
	if _, err := AssembleAgentBrief(ctx, mem, bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad runKind sealed: %v", err)
	}
	noInstr := b
	noInstr.Instructions = ""
	if _, err := AssembleAgentBrief(ctx, mem, noInstr); !errors.Is(err, ErrInvalid) {
		t.Fatalf("brief without instructions sealed: %v", err)
	}
}

func TestAgentSessionSegmentChain(t *testing.T) {
	ctx := context.Background()
	mem := objectstore.NewMemStore(objectstore.AlgoSHA256)

	g0 := AgentSessionSegment{
		SessionID: "as_01hz",
		FromSeq:   0, ToSeq: 1,
		Entries: []AgentSessionEvent{
			{Seq: 0, Kind: SessionEventStateChanged, Payload: map[string]any{"state": "running"}},
			{Seq: 1, Kind: SessionEventMessageAgent},
		},
	}
	id0, err := AssembleAgentSessionSegment(ctx, mem, g0)
	if err != nil {
		t.Fatalf("segment 0: %v", err)
	}
	g1 := AgentSessionSegment{
		SessionID: "as_01hz",
		FromSeq:   2, ToSeq: 2,
		Entries: []AgentSessionEvent{{Seq: 2, Kind: SessionEventArtifactProduced}},
		Prev:    string(id0),
	}
	if _, err := AssembleAgentSessionSegment(ctx, mem, g1); err != nil {
		t.Fatalf("segment 1: %v", err)
	}

	// Closed vocabulary: an unknown kind — including any status-assertion
	// shape — is unrepresentable.
	bad := g0
	bad.Entries = []AgentSessionEvent{{Seq: 0, Kind: "status_asserted"}}
	if _, err := AssembleAgentSessionSegment(ctx, mem, bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown event kind sealed: %v", err)
	}
	nonMono := g0
	nonMono.Entries = []AgentSessionEvent{
		{Seq: 1, Kind: SessionEventMessageUser},
		{Seq: 0, Kind: SessionEventMessageAgent},
	}
	if _, err := AssembleAgentSessionSegment(ctx, mem, nonMono); !errors.Is(err, ErrInvalid) {
		t.Fatalf("non-monotonic seq sealed: %v", err)
	}
}

func TestAgentSessionSnapshot(t *testing.T) {
	ctx := context.Background()
	mem := objectstore.NewMemStore(objectstore.AlgoSHA256)
	typeID := testBlobID(t, mem, "type")
	briefID := testBlobID(t, mem, "brief")
	segID := testBlobID(t, mem, "seg")

	sn := AgentSessionSnapshot{
		SessionID: "as_01hz",
		RunKind:   RunKindDesign,
		AgentType: typeID,
		Brief:     briefID,
		Segments:  []string{segID},
		Outcome:   &AgentOutcome{Status: "completed", PR: "https://github.com/x/y/pull/1"},
	}
	id, err := AssembleAgentSession(ctx, mem, sn)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	pure, err := AgentSessionID(objectstore.AlgoSHA256, sn)
	if err != nil {
		t.Fatal(err)
	}
	if id != pure {
		t.Fatalf("pure %s != assembled %s", pure, id)
	}

	badStatus := sn
	badStatus.Outcome = &AgentOutcome{Status: "victorious"}
	if _, err := AssembleAgentSession(ctx, mem, badStatus); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad outcome sealed: %v", err)
	}
	badID := sn
	badID.SessionID = "sess-1"
	if _, err := AssembleAgentSession(ctx, mem, badID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad session id sealed: %v", err)
	}
}

func TestSessionEventVocabularyClosed(t *testing.T) {
	for _, k := range []string{
		SessionEventStateChanged, SessionEventHarness, SessionEventMessageUser,
		SessionEventMessageAgent, SessionEventToolCall, SessionEventToolResult,
		SessionEventApprovalRequested, SessionEventApprovalResolved,
		SessionEventArtifactProduced, SessionEventCostSample, SessionEventError,
	} {
		if !ValidSessionEventKind(k) {
			t.Fatalf("known kind %q rejected", k)
		}
	}
	for _, k := range []string{"", "status", "lifecycle_set", "pinned"} {
		if ValidSessionEventKind(k) {
			t.Fatalf("unknown kind %q accepted", k)
		}
	}
}

func TestAgentRecordEncodingIsCanonical(t *testing.T) {
	// Payload map key order must not affect identity (CanonicalEncode sorts).
	mk := func(p map[string]any) AgentSessionSegment {
		return AgentSessionSegment{
			SessionID: "as_1", FromSeq: 0, ToSeq: 0,
			Entries: []AgentSessionEvent{{Seq: 0, Kind: SessionEventCostSample, Payload: p}},
		}
	}
	a, err := AgentSessionSegmentID(objectstore.AlgoSHA256, mk(map[string]any{"tokens": 5, "minutes": 1}))
	if err != nil {
		t.Fatal(err)
	}
	b, err := AgentSessionSegmentID(objectstore.AlgoSHA256, mk(map[string]any{"minutes": 1, "tokens": 5}))
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("map order changed identity: %s vs %s", a, b)
	}
	// And the encoded record carries no insignificant whitespace.
	enc, err := Encode(mk(map[string]any{"x": 1}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(string(enc), "\n\t") || strings.Contains(string(enc), ": ") {
		t.Fatalf("non-canonical encoding: %s", enc)
	}
}
