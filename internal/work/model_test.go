package work

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestNewStateValidation(t *testing.T) {
	if _, err := NewState("", "ORN"); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("empty project: err = %v", err)
	}
	if _, err := NewState("acme/platform", "x"); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("bad prefix: err = %v", err)
	}
	if _, err := NewState("acme/platform", "ORN"); err != nil {
		t.Errorf("valid state: err = %v", err)
	}
}

func TestKeyValidators(t *testing.T) {
	good := func(err error, ctx string) {
		t.Helper()
		if err != nil {
			t.Errorf("%s: unexpected err %v", ctx, err)
		}
	}
	bad := func(err error, ctx string) {
		t.Helper()
		if !errors.Is(err, ErrInvalidKey) {
			t.Errorf("%s: err = %v, want ErrInvalidKey", ctx, err)
		}
	}
	good(ValidatePrefix("ORN"), "ORN")
	bad(ValidatePrefix("o"), "lowercase")
	bad(ValidatePrefix("TOOLONG"), "too long")
	good(ValidateTaskKey("ORN-142"), "ORN-142")
	bad(ValidateTaskKey("ORN-0"), "zero seq")
	bad(ValidateTaskKey("orn-1"), "lowercase")
	good(ValidateSlug("orun-work"), "slug")
	bad(ValidateSlug("Orun_Work"), "bad slug")
	good(ValidateComponentKey("sourceplane/orun/api-edge"), "component")
	bad(ValidateComponentKey("two/segments"), "short component")
}

func TestFormatAndParseWorkKey(t *testing.T) {
	if got := FormatTaskKey("ORN", 142); got != "ORN-142" {
		t.Errorf("FormatTaskKey = %q", got)
	}
	if got := FormatWorkKey("acme/platform", "ORN-142"); got != "acme/platform/ORN-142" {
		t.Errorf("FormatWorkKey = %q", got)
	}
	proj, key, err := ParseWorkKey("acme/platform/epics/orun-work")
	if err != nil {
		t.Fatalf("ParseWorkKey: %v", err)
	}
	if proj != "acme/platform" || key != "epics/orun-work" {
		t.Errorf("ParseWorkKey = (%q,%q)", proj, key)
	}
	if _, _, err := ParseWorkKey("too/short"); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("short work key: err = %v", err)
	}
}

func TestNewItemIDPrefixes(t *testing.T) {
	cases := map[Kind]string{
		KindInitiative: IDPrefixInitiative,
		KindEpic:       IDPrefixEpic,
		KindTask:       IDPrefixTask,
	}
	for k, prefix := range cases {
		if id := NewItemID(k); !strings.HasPrefix(id, prefix) {
			t.Errorf("NewItemID(%s) = %q, want prefix %q", k, id, prefix)
		}
	}
	if !strings.HasPrefix(NewEventID(), IDPrefixEvent) {
		t.Error("NewEventID prefix")
	}
	if !strings.HasPrefix(NewPrincipalID(), IDPrefixPrincipal) {
		t.Error("NewPrincipalID prefix")
	}
}

func TestActorValidate(t *testing.T) {
	if err := (Actor{Type: ActorUser, ID: "prn_x"}).Validate(); err != nil {
		t.Errorf("valid actor: %v", err)
	}
	if err := (Actor{Type: ActorUser}).Validate(); !errors.Is(err, ErrMissingActor) {
		t.Errorf("empty id: err = %v", err)
	}
	if err := (Actor{Type: "ghost", ID: "x"}).Validate(); !errors.Is(err, ErrMissingActor) {
		t.Errorf("bad type: err = %v", err)
	}
	if !(Actor{Type: ActorUser, ID: "x"}).IsHuman() {
		t.Error("user should be human")
	}
	if (Actor{Type: ActorAutomation, ID: "x"}).IsHuman() {
		t.Error("automation is not human")
	}
}

func TestPrincipalValidate(t *testing.T) {
	human := Principal{ID: "prn_1", Type: PrincipalHuman, Handle: "rahul", GitHub: &GitHubIdentity{UserID: 42}}
	if err := human.Validate(); err != nil {
		t.Errorf("valid human: %v", err)
	}
	if human.ActorType() != ActorUser {
		t.Error("human actor type")
	}
	agent := Principal{ID: "prn_2", Type: PrincipalAgent, Handle: "claude", Owner: "prn_1"}
	if err := agent.Validate(); err != nil {
		t.Errorf("valid agent: %v", err)
	}
	if agent.ActorType() != ActorAgent {
		t.Error("agent actor type")
	}
	if err := (Principal{Type: PrincipalHuman, Handle: "x"}).Validate(); !errors.Is(err, ErrInvalidPrincipal) {
		t.Errorf("missing id: err = %v", err)
	}
	if err := (Principal{ID: "p", Handle: "x", Type: "robot"}).Validate(); !errors.Is(err, ErrInvalidPrincipal) {
		t.Errorf("bad type: err = %v", err)
	}
	if err := (Principal{ID: "p", Handle: "x", Type: PrincipalAgent}).Validate(); !errors.Is(err, ErrInvalidPrincipal) {
		t.Errorf("agent without owner should fail: err = %v", err)
	}
}

func TestContractCompletenessAndAgentReady(t *testing.T) {
	var nilC *Contract
	if nilC.Complete() {
		t.Error("nil contract is not complete")
	}
	full := &Contract{Goal: "g", Affects: []string{"a/b/c"}, DoneWhen: []string{"d"}, Gates: []string{"tests"}}
	if !full.Complete() {
		t.Error("full contract should be complete")
	}
	if (&Contract{Goal: "g"}).Complete() {
		t.Error("goal-only contract is not complete")
	}
	if !full.AgentReady(nil) {
		t.Error("complete contract with nil resolver is agent-ready")
	}
	if full.AgentReady(func(string) bool { return false }) {
		t.Error("unresolved affects must not be agent-ready")
	}
	if !full.AgentReady(func(string) bool { return true }) {
		t.Error("resolved affects should be agent-ready")
	}
	if (&Contract{Goal: "g"}).AgentReady(nil) {
		t.Error("incomplete contract is never agent-ready")
	}
}

func TestLinkValidate(t *testing.T) {
	if err := (Link{Project: "p", From: "a", Type: LinkBlocks, To: "b"}).Validate(); err != nil {
		t.Errorf("valid link: %v", err)
	}
	if err := (Link{Project: "p", From: "a", Type: "ghost", To: "b"}).Validate(); !errors.Is(err, ErrInvalidLink) {
		t.Errorf("bad type: err = %v", err)
	}
	if err := (Link{Project: "p", Type: LinkBlocks, To: "b"}).Validate(); !errors.Is(err, ErrInvalidLink) {
		t.Errorf("missing from: err = %v", err)
	}
}

func TestDecodePayloadErrors(t *testing.T) {
	if err := decodePayload(nil, &statusChangedPayload{}); !errors.Is(err, ErrInvalidEvent) {
		t.Errorf("nil payload: err = %v", err)
	}
	if err := decodePayload([]byte("{not json"), &statusChangedPayload{}); !errors.Is(err, ErrInvalidEvent) {
		t.Errorf("bad json: err = %v", err)
	}
}

func TestCanonicalEncoding(t *testing.T) {
	a := map[string]any{"b": 2, "a": []any{1, "x", true}}
	b := map[string]any{"a": []any{1, "x", true}, "b": 2}
	eq, err := CanonicalEqual(a, b)
	if err != nil {
		t.Fatalf("CanonicalEqual: %v", err)
	}
	if !eq {
		t.Fatal("reordered maps should canonical-equal")
	}
	got, err := Canonical(a)
	if err != nil {
		t.Fatalf("Canonical: %v", err)
	}
	if string(got) != `{"a":[1,"x",true],"b":2}` {
		t.Fatalf("Canonical = %s", got)
	}
	if _, err := Canonical(math.NaN()); err == nil {
		t.Error("Canonical(NaN) should error")
	}
	if _, err := CanonicalEqual(math.NaN(), 1); err == nil {
		t.Error("CanonicalEqual(NaN,1) should error")
	}
	if _, err := CanonicalEqual(1, math.NaN()); err == nil {
		t.Error("CanonicalEqual(1,NaN) should error")
	}
}

func TestSchemaEmbedIsValid(t *testing.T) {
	if len(Schema) == 0 {
		t.Fatal("embedded schema is empty")
	}
	if !strings.Contains(string(Schema), "\"WorkEvent\"") || !strings.Contains(string(Schema), "\"$defs\"") {
		t.Fatal("embedded schema missing expected $defs/WorkEvent")
	}
}
