package nodes

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func goodID(c string) string { return "sha256:" + strings.Repeat(c, 64) }

func TestValidateAcceptsGoodRecords(t *testing.T) {
	t.Parallel()
	good := []interface{ Validate() error }{
		SourceSnapshot{Kind: KindSourceSnapshot, Scope: ScopeMain},
		SourceSnapshot{Kind: KindSourceSnapshot, Scope: ScopeLocalNoGit},
		CatalogSnapshot{Kind: KindCatalogSnapshot, SourceID: goodID("a")},
		ComponentManifest{Kind: KindComponentManifest, Identity: ComponentIdentity{ComponentKey: "ns/repo/api-edge", Name: "api-edge"}},
		CatalogGraph{Kind: KindCatalogGraph, EdgeKind: "dependencies"},
		PlanRevision{Kind: KindPlanRevision, PlanHash: goodID("b"), Scope: RevisionScope{Mode: "full"}},
		TriggerOccurrence{Kind: KindTriggerOccurrence, TriggerID: "trg_1", TriggerName: "system.manual", RevisionID: goodID("c"), CreatedAt: time.Now()},
		ExecutionRun{Kind: KindExecutionRun, ExecutionID: "exec_1", RevisionID: goodID("d"), Status: StatusRunning},
		JobRun{Kind: KindJobRun, JobID: "a@b", Folder: "j-1", Status: StatusSucceeded},
		JobAttempt{Kind: KindJobAttempt, Attempt: 1, Status: StatusSucceeded},
		StepAttempt{Kind: KindStepAttempt, StepID: "build", Status: StatusSucceeded},
		ImpactOwnership{Kind: KindImpactOwnership, SchemaVersion: 1},
		ImpactOwnership{Kind: KindImpactOwnership, SchemaVersion: 1,
			Components: map[string]string{"apps/api": "ns/repo/api", ".": "ns/repo/root"}},
	}
	for i, g := range good {
		if err := g.Validate(); err != nil {
			t.Fatalf("good[%d] (%T) rejected: %v", i, g, err)
		}
	}
}

func TestValidateRejectsBadRecords(t *testing.T) {
	t.Parallel()
	bad := []interface{ Validate() error }{
		SourceSnapshot{Kind: "X", Scope: ScopeMain},
		SourceSnapshot{Kind: KindSourceSnapshot, Scope: "weird"},
		CatalogSnapshot{Kind: KindCatalogSnapshot, SourceID: "bad"},
		CatalogSnapshot{Kind: KindCatalogSnapshot, SourceID: goodID("a"), ComponentCount: 5},
		CatalogSnapshot{Kind: KindCatalogSnapshot, SourceID: goodID("a"), ComponentCount: 1,
			Components: []CatalogComponentRef{{ComponentKey: "bad-key", ManifestID: goodID("a")}}},
		ComponentManifest{Kind: KindComponentManifest, Identity: ComponentIdentity{ComponentKey: "only/two", Name: "two"}},
		ComponentManifest{Kind: KindComponentManifest, Identity: ComponentIdentity{ComponentKey: "ns/repo/api", Name: "wrong"}},
		CatalogGraph{Kind: KindCatalogGraph, EdgeKind: ""},
		PlanRevision{Kind: KindPlanRevision, PlanHash: "bad", Scope: RevisionScope{Mode: "full"}},
		PlanRevision{Kind: KindPlanRevision, PlanHash: goodID("b"), Scope: RevisionScope{Mode: "weird"}},
		PlanRevision{Kind: KindPlanRevision, PlanHash: goodID("b"), Scope: RevisionScope{Mode: "full"}, CatalogID: "bad"},
		TriggerOccurrence{Kind: KindTriggerOccurrence, TriggerID: "no-prefix", RevisionID: goodID("c"), TriggerName: "x", CreatedAt: time.Now()},
		TriggerOccurrence{Kind: KindTriggerOccurrence, TriggerID: "trg_1", RevisionID: "bad", TriggerName: "x", CreatedAt: time.Now()},
		TriggerOccurrence{Kind: KindTriggerOccurrence, TriggerID: "trg_1", RevisionID: goodID("c"), TriggerName: "", CreatedAt: time.Now()},
		TriggerOccurrence{Kind: KindTriggerOccurrence, TriggerID: "trg_1", RevisionID: goodID("c"), TriggerName: "x"}, // zero time
		ExecutionRun{Kind: KindExecutionRun, ExecutionID: "", RevisionID: goodID("d"), Status: StatusRunning},
		ExecutionRun{Kind: KindExecutionRun, ExecutionID: "e", RevisionID: "bad", Status: StatusRunning},
		ExecutionRun{Kind: KindExecutionRun, ExecutionID: "e", RevisionID: goodID("d"), Status: "weird"},
		ExecutionRun{Kind: KindExecutionRun, ExecutionID: "e", RevisionID: goodID("d"), Status: StatusRunning, TriggerID: "nope"},
		JobRun{Kind: KindJobRun, JobID: "", Folder: "j", Status: StatusSucceeded},
		JobRun{Kind: KindJobRun, JobID: "x", Folder: "j", Status: "weird"},
		JobAttempt{Kind: KindJobAttempt, Attempt: 0, Status: StatusSucceeded},
		JobAttempt{Kind: KindJobAttempt, Attempt: 1, Status: "weird"},
		StepAttempt{Kind: KindStepAttempt, StepID: "", Status: StatusSucceeded},
		StepAttempt{Kind: KindStepAttempt, StepID: "s", Status: "weird"},
		StepAttempt{Kind: KindStepAttempt, StepID: "s", Status: StatusSucceeded, LogID: "bad"},
		ImpactOwnership{Kind: "X", SchemaVersion: 1},
		ImpactOwnership{Kind: KindImpactOwnership, SchemaVersion: 0},
		ImpactOwnership{Kind: KindImpactOwnership, SchemaVersion: 1,
			Components: map[string]string{"apps/api/": "ns/repo/api"}}, // trailing slash
		ImpactOwnership{Kind: KindImpactOwnership, SchemaVersion: 1,
			Components: map[string]string{"apps/api": "bad-key"}}, // bad component key
	}
	for i, b := range bad {
		if err := b.Validate(); !errors.Is(err, ErrInvalid) {
			t.Fatalf("bad[%d] (%T) accepted or wrong error: %v", i, b, err)
		}
	}
}

func TestValidOwnershipDir(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"apps/api-edge": true,
		".":             true, // workspace root
		"libs/shared":   true,
		"":              false,
		"/abs":          false,
		"trailing/":     false,
		"./leading":     false,
		"a//b":          false, // empty segment
		"a/./b":         false,
		"a/../b":        false,
	}
	for in, want := range cases {
		if got := validOwnershipDir(in); got != want {
			t.Errorf("validOwnershipDir(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestValidateRejectsWrongKind(t *testing.T) {
	t.Parallel()
	wrong := []interface{ Validate() error }{
		CatalogSnapshot{Kind: "X"},
		ComponentManifest{Kind: "X"},
		CatalogGraph{Kind: "X"},
		PlanRevision{Kind: "X"},
		TriggerOccurrence{Kind: "X"},
		ExecutionRun{Kind: "X"},
		JobRun{Kind: "X"},
		JobAttempt{Kind: "X"},
		StepAttempt{Kind: "X"},
	}
	for i, w := range wrong {
		if err := w.Validate(); !errors.Is(err, ErrInvalid) {
			t.Fatalf("wrong-kind[%d] (%T) = %v, want ErrInvalid", i, w, err)
		}
	}
}

func TestValidateCatalogBadManifestID(t *testing.T) {
	t.Parallel()
	// Good componentKey but bad manifestId reaches the manifestId check.
	c := CatalogSnapshot{Kind: KindCatalogSnapshot, SourceID: goodID("a"), ComponentCount: 1,
		Components: []CatalogComponentRef{{ComponentKey: "ns/repo/x", Name: "x", ManifestID: "bad"}}}
	if err := c.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad manifestId = %v, want ErrInvalid", err)
	}
}

func TestIsTerminalStatus(t *testing.T) {
	t.Parallel()
	for _, s := range []string{StatusSucceeded, StatusFailed, StatusCancelled} {
		if !IsTerminalStatus(s) {
			t.Fatalf("IsTerminalStatus(%q) = false", s)
		}
	}
	for _, s := range []string{StatusPending, StatusRunning, "weird"} {
		if IsTerminalStatus(s) {
			t.Fatalf("IsTerminalStatus(%q) = true", s)
		}
	}
}
