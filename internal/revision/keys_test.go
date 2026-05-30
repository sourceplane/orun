package revision

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/statestore"
	"github.com/sourceplane/orun/internal/testfx/statefs"
	"github.com/sourceplane/orun/internal/triggerctx"
)

// newTestStore returns a LocalStore rooted under a fresh statefs workspace.
// We deliberately route through the statestore driver (not a hand-rolled
// fake) so the test exercises the same CreateIfAbsent/Write/CompareAndSwap
// semantics production code will see.
func newTestStore(t *testing.T) statestore.StateStore {
	t.Helper()
	root := statefs.NewWorkspace(t)
	store, err := statestore.NewLocalStore(statestore.LocalConfig{Root: root + "/.orun"})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	return store
}

// newTestTrigger returns a deterministic system-manual TriggerOccurrence
// suitable for revision-key derivation. The 7-hex headRevision is required
// by the triggerctx key pattern.
func newTestTrigger(t *testing.T) triggerctx.TriggerOccurrence {
	t.Helper()
	occ := triggerctx.NewSystemManual(triggerctx.SystemOptions{
		Source: triggerctx.TriggerSource{
			Repo:         "git@example.com:o/r.git",
			Ref:          "refs/heads/main",
			SourceScope:  "main",
			HeadRevision: "abcdef0",
			WorkingTree:  triggerctx.WorkingTreeClean,
		},
		PlanScope: triggerctx.PlanScope{
			Mode:               triggerctx.PlanScopeFull,
			ActiveEnvironments: []string{"prod"},
		},
		Now: time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC),
	})
	return occ
}

func TestPlanShortHash_Variants(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"deadbeefcafebabe1234", "deadbeef"},
		{"sha256:DEADBEEFCAFEBABE", "deadbeef"},
		{"  AaBbCcDd0123  ", "aabbccdd"},
	}
	for _, c := range cases {
		got, err := PlanShortHash(c.in)
		if err != nil {
			t.Fatalf("PlanShortHash(%q) err: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("PlanShortHash(%q) = %q want %q", c.in, got, c.want)
		}
	}
}

func TestPlanShortHash_Invalid(t *testing.T) {
	for _, in := range []string{"", "abc", "sha256:gggggggg", "zzzzzzzz"} {
		_, err := PlanShortHash(in)
		if !errors.Is(err, statestore.ErrInvalid) {
			t.Errorf("PlanShortHash(%q) err=%v want ErrInvalid", in, err)
		}
	}
}

func TestRevisionKey_DeterministicAndValid(t *testing.T) {
	trig := newTestTrigger(t)
	planHash := "feedface00112233445566778899aabbccddeeff00112233"

	got1, err := RevisionKey(trig, planHash)
	if err != nil {
		t.Fatalf("RevisionKey: %v", err)
	}
	got2, _ := RevisionKey(trig, planHash)
	if got1 != got2 {
		t.Fatalf("RevisionKey not deterministic: %q vs %q", got1, got2)
	}
	if err := ValidateRevisionKey(got1); err != nil {
		t.Fatalf("ValidateRevisionKey(%q): %v", got1, err)
	}
	// Format: rev-<scope>-<sha7>-p<planHash8>
	want := "rev-main-abcdef0-pfeedface"
	if got1 != want {
		t.Errorf("RevisionKey = %q want %q", got1, want)
	}
}

func TestRevisionKey_MissingTriggerKey(t *testing.T) {
	var trig triggerctx.TriggerOccurrence
	_, err := RevisionKey(trig, "deadbeef")
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("err=%v want ErrInvalid", err)
	}
}

func TestRevisionKey_BadTriggerKey(t *testing.T) {
	trig := newTestTrigger(t)
	trig.TriggerKey = "not-a-trigger-key"
	_, err := RevisionKey(trig, "deadbeef")
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("err=%v want ErrInvalid", err)
	}
}

func TestRevisionKey_TriggerKeyMissingPrefix(t *testing.T) {
	trig := newTestTrigger(t)
	// Hand-craft a key that matches the regex shape but not the prefix
	// guard. The regex demands "trg-…" so any non-prefixed key is rejected
	// by the pattern check first.
	trig.TriggerKey = "xrg-main-abcdef0"
	_, err := RevisionKey(trig, "deadbeefcafebabe")
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("err=%v want ErrInvalid", err)
	}
}

func TestValidateRevisionKey_Cases(t *testing.T) {
	good := []string{
		"rev-main-abcdef0-pfeedface",
		"rev-pr-42-abcdef0-pdeadbeef-x1",
		"rev-local-dirty-pdeadbeef",
	}
	for _, k := range good {
		if err := ValidateRevisionKey(k); err != nil {
			t.Errorf("ValidateRevisionKey(%q): unexpected err %v", k, err)
		}
	}
	bad := []string{
		"",
		"REV-MAIN-pfeedface",       // uppercase scope
		"rev-main-pXEEDFACE",       // non-hex planHash byte
		"rev-main-pfeedface-y2",    // wrong suffix prefix
		"rev-main-abcdef0",         // missing -p prefix on planHash
		"rev-main-abcdef0-pfeedfac", // 7-char planHash
	}
	for _, k := range bad {
		if err := ValidateRevisionKey(k); !errors.Is(err, statestore.ErrInvalid) {
			t.Errorf("ValidateRevisionKey(%q): err=%v want ErrInvalid", k, err)
		}
	}
}

func TestResolveCollision_FreshSlot(t *testing.T) {
	store := newTestStore(t)
	candidate := "rev-main-abcdef0-pfeedface"
	got, err := ResolveCollision(context.Background(), store, candidate)
	if err != nil {
		t.Fatalf("ResolveCollision: %v", err)
	}
	if got != candidate {
		t.Errorf("got %q want %q (no -xN expected on fresh slot)", got, candidate)
	}
}

func TestResolveCollision_AppendsSuffix(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	candidate := "rev-main-abcdef0-pfeedface"

	first, err := ResolveCollision(ctx, store, candidate)
	if err != nil || first != candidate {
		t.Fatalf("first claim got=%q err=%v", first, err)
	}
	second, err := ResolveCollision(ctx, store, candidate)
	if err != nil {
		t.Fatalf("second claim err: %v", err)
	}
	if second != candidate+"-x1" {
		t.Errorf("second claim got %q want %q", second, candidate+"-x1")
	}
	third, err := ResolveCollision(ctx, store, candidate)
	if err != nil {
		t.Fatalf("third claim err: %v", err)
	}
	if third != candidate+"-x2" {
		t.Errorf("third claim got %q want %q", third, candidate+"-x2")
	}
}

func TestResolveCollision_InvalidCandidate(t *testing.T) {
	_, err := ResolveCollision(context.Background(), newTestStore(t), "not-a-key")
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("err=%v want ErrInvalid", err)
	}
}

// fillingStore wraps a real statestore.StateStore but always returns
// ErrExists from CreateIfAbsent, simulating an exhausted suffix space. We
// implement the full interface by embedding the inner store and overriding
// only CreateIfAbsent.
type fillingStore struct{ statestore.StateStore }

func (f fillingStore) CreateIfAbsent(_ context.Context, _ string, _ []byte) (statestore.ObjectMeta, error) {
	return statestore.ObjectMeta{}, statestore.ErrExists
}

func TestResolveCollision_CapExhausted(t *testing.T) {
	inner := newTestStore(t)
	store := fillingStore{StateStore: inner}
	_, err := ResolveCollision(context.Background(), store, "rev-main-abcdef0-pfeedface")
	if !errors.Is(err, statestore.ErrConflict) {
		t.Fatalf("err=%v want ErrConflict", err)
	}
}

// erroringStore returns an injectable error from CreateIfAbsent so we can
// exercise the non-ErrExists / non-nil branch of ResolveCollision.
type erroringStore struct {
	statestore.StateStore
	err error
}

func (e erroringStore) CreateIfAbsent(_ context.Context, _ string, _ []byte) (statestore.ObjectMeta, error) {
	return statestore.ObjectMeta{}, e.err
}

func TestResolveCollision_PassesThroughDriverError(t *testing.T) {
	sentinel := errors.New("disk on fire")
	store := erroringStore{StateStore: newTestStore(t), err: sentinel}
	_, err := ResolveCollision(context.Background(), store, "rev-main-abcdef0-pfeedface")
	if !errors.Is(err, sentinel) {
		t.Fatalf("err=%v want %v", err, sentinel)
	}
}
