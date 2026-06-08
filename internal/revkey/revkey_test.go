package revkey

import (
	"errors"
	"testing"

	"github.com/sourceplane/orun/internal/triggerctx"
)

// newTestTrigger returns a deterministic system-manual TriggerOccurrence
// suitable for revision-key derivation (its 7-hex headRevision yields a
// trg-main-abcdef0 trigger key).
func newTestTrigger(t *testing.T) triggerctx.TriggerOccurrence {
	t.Helper()
	return triggerctx.NewSystemManual(triggerctx.SystemOptions{
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
	})
}

func TestPlanShortHash_Variants(t *testing.T) {
	cases := []struct{ in, want string }{
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
		if _, err := PlanShortHash(in); !errors.Is(err, ErrInvalid) {
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
	if want := "rev-main-abcdef0-pfeedface"; got1 != want {
		t.Errorf("RevisionKey = %q want %q", got1, want)
	}
}

func TestRevisionKey_MissingTriggerKey(t *testing.T) {
	if _, err := RevisionKey(triggerctx.TriggerOccurrence{}, "deadbeef00"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err=%v want ErrInvalid", err)
	}
}

func TestRevisionKey_BadTriggerKey(t *testing.T) {
	if _, err := RevisionKey(triggerctx.TriggerOccurrence{TriggerKey: "not-a-trigger-key"}, "deadbeef00"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err=%v want ErrInvalid", err)
	}
}

func TestValidateRevisionKey(t *testing.T) {
	if err := ValidateRevisionKey("rev-main-abcdef0-pfeedface"); err != nil {
		t.Errorf("valid key rejected: %v", err)
	}
	for _, bad := range []string{"", "rev-MAIN-p12345678", "revision-x", "rev-main-pXYZ"} {
		if err := ValidateRevisionKey(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("ValidateRevisionKey(%q) err=%v want ErrInvalid", bad, err)
		}
	}
}
