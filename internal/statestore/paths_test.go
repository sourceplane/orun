package statestore

import (
	"strings"
	"testing"
)

func TestRevisionDir(t *testing.T) {
	got := RevisionDir("rev-pr139-def456a-p8f31c09")
	want := "revisions/rev-pr139-def456a-p8f31c09"
	if got != want {
		t.Fatalf("RevisionDir = %q, want %q", got, want)
	}
}

func TestPlanPath(t *testing.T) {
	got := PlanPath("rev-x")
	want := "revisions/rev-x/plan.json"
	if got != want {
		t.Fatalf("PlanPath = %q, want %q", got, want)
	}
}

func TestTriggerPath(t *testing.T) {
	got := TriggerPath("rev-x")
	want := "revisions/rev-x/trigger.json"
	if got != want {
		t.Fatalf("TriggerPath = %q, want %q", got, want)
	}
}

func TestRevisionDocPath(t *testing.T) {
	got := RevisionDocPath("rev-x")
	want := "revisions/rev-x/revision.json"
	if got != want {
		t.Fatalf("RevisionDocPath = %q, want %q", got, want)
	}
}

func TestManifestPath(t *testing.T) {
	got := ManifestPath("rev-x")
	want := "revisions/rev-x/manifest.json"
	if got != want {
		t.Fatalf("ManifestPath = %q, want %q", got, want)
	}
}

func TestExecutionDir(t *testing.T) {
	got := ExecutionDir("rev-x", "exec-y")
	want := "revisions/rev-x/executions/exec-y"
	if got != want {
		t.Fatalf("ExecutionDir = %q, want %q", got, want)
	}
}

func TestExecutionDocPath(t *testing.T) {
	got := ExecutionDocPath("rev-x", "exec-y")
	want := "revisions/rev-x/executions/exec-y/execution.json"
	if got != want {
		t.Fatalf("ExecutionDocPath = %q, want %q", got, want)
	}
}

func TestSnapshotPath(t *testing.T) {
	got := SnapshotPath("rev-x", "exec-y")
	want := "revisions/rev-x/executions/exec-y/snapshot.latest.json"
	if got != want {
		t.Fatalf("SnapshotPath = %q, want %q", got, want)
	}
}

func TestEventPath(t *testing.T) {
	got := EventPath("rev-x", "exec-y", 7, "execution-created")
	want := "revisions/rev-x/executions/exec-y/events/00000000000000000007-execution-created.json"
	if got != want {
		t.Fatalf("EventPath = %q, want %q", got, want)
	}
}

func TestEventPathOrderable(t *testing.T) {
	a := EventPath("r", "e", 2, "k")
	b := EventPath("r", "e", 10, "k")
	if !(a < b) {
		t.Fatalf("expected lexicographic order: %q < %q", a, b)
	}
}

func TestLatestRevisionRefPath(t *testing.T) {
	if got := LatestRevisionRefPath(); got != "refs/latest-revision.json" {
		t.Fatalf("got %q", got)
	}
}

func TestLatestExecutionRefPath(t *testing.T) {
	if got := LatestExecutionRefPath(); got != "refs/latest-execution.json" {
		t.Fatalf("got %q", got)
	}
}

func TestTriggerLatestRefPath(t *testing.T) {
	got := TriggerLatestRefPath("nightly")
	want := "refs/triggers/nightly/latest.json"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestTriggerScopeRefPath(t *testing.T) {
	got := TriggerScopeRefPath("on-pr", "main")
	want := "refs/triggers/on-pr/main.json"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestNamedRefPath(t *testing.T) {
	got := NamedRefPath("staging")
	want := "refs/named/staging.json"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRevisionIndexPath(t *testing.T) {
	got := RevisionIndexPath("rev-x")
	want := "indexes/revisions/rev-x.json"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestExecutionIndexPath(t *testing.T) {
	got := ExecutionIndexPath("exec-y")
	want := "indexes/executions/exec-y.json"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestValidateComponent(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"abc", false},
		{"abcXYZ", false},
		{"abc.def", false},
		{"abc_def", false},
		{"abc-def", false},
		{"a1b2c3", false},
		{"rev-pr139-def456a-p8f31c09", false},
		{"00000000000000000007-execution-created", false},

		{"", true},
		{".", true},
		{"..", true},
		{"a/b", true},
		{"a\\b", true},
		{"a b", true},
		{"a$b", true},
		{"é", true},
	}
	for _, tc := range cases {
		err := ValidateComponent(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("ValidateComponent(%q) err=%v wantErr=%v", tc.in, err, tc.wantErr)
		}
	}
}

func TestValidatePath(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"a/b/c", false},
		{"revisions/rev-x/plan.json", false},
		{"refs/triggers/n/latest.json", false},

		{"", true},
		{"/abs", true},
		{"a/", true},
		{"a//b", true},
		{"a/../b", true},
		{"a/./b", true},
		{"a/b\\c", true},
		{"a/b c", true},
		{"a/b$c", true},
	}
	for _, tc := range cases {
		err := ValidatePath(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("ValidatePath(%q) err=%v wantErr=%v", tc.in, err, tc.wantErr)
		}
	}
}

func TestPathHelpersValidatedComponents(t *testing.T) {
	// All valid helper outputs must round-trip through ValidatePath.
	paths := []string{
		RevisionDir("rev-x"),
		PlanPath("rev-x"),
		TriggerPath("rev-x"),
		RevisionDocPath("rev-x"),
		ManifestPath("rev-x"),
		ExecutionDir("rev-x", "exec-y"),
		ExecutionDocPath("rev-x", "exec-y"),
		SnapshotPath("rev-x", "exec-y"),
		EventPath("rev-x", "exec-y", 1, "execution-created"),
		LatestRevisionRefPath(),
		LatestExecutionRefPath(),
		TriggerLatestRefPath("nightly"),
		TriggerScopeRefPath("on-pr", "main"),
		NamedRefPath("staging"),
		RevisionIndexPath("rev-x"),
		ExecutionIndexPath("exec-y"),
	}
	for _, p := range paths {
		if err := ValidatePath(p); err != nil {
			t.Errorf("helper produced invalid path %q: %v", p, err)
		}
	}
}

func TestPathHelpersPanicOnInvalidComponent(t *testing.T) {
	cases := []struct {
		name string
		fn   func()
	}{
		{"RevisionDir", func() { RevisionDir("a/b") }},
		{"ExecutionDir-exec", func() { ExecutionDir("rev", "..") }},
		{"EventPath-kind", func() { EventPath("rev", "exec", 1, "bad kind") }},
		{"TriggerLatestRefPath", func() { TriggerLatestRefPath("") }},
		{"TriggerScopeRefPath-name", func() { TriggerScopeRefPath("a/b", "main") }},
		{"NamedRefPath", func() { NamedRefPath("..") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("expected panic")
				} else if !strings.Contains(stringify(r), "invalid component") {
					t.Fatalf("unexpected panic: %v", r)
				}
			}()
			tc.fn()
		})
	}
}

func stringify(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if e, ok := v.(error); ok {
		return e.Error()
	}
	return ""
}
