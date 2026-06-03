package execmodel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func TestGenerateExecID(t *testing.T) {
	id := GenerateExecID("My Plan")
	if !strings.HasPrefix(id, "my-plan-") {
		t.Fatalf("exec id = %q", id)
	}
	if GenerateExecID("") == GenerateExecID("") {
		t.Fatalf("ids should be random")
	}
	long := GenerateExecID(strings.Repeat("x", 50))
	// name is capped at 30 chars before the date/suffix.
	if len(strings.Split(long, "-")[0]) > 30 {
		t.Fatalf("name not capped: %q", long)
	}
}

func TestSummarizeExecutionState(t *testing.T) {
	if got := SummarizeExecutionState(nil); got != (ExecutionCounts{}) {
		t.Fatalf("nil = %+v", got)
	}
	st := &ExecState{Jobs: map[string]*JobState{
		"a": {Status: "completed"}, "b": {Status: "failed"}, "c": {Status: "running"},
		"d": {Status: "pending"}, "e": nil,
	}}
	got := SummarizeExecutionState(st)
	want := ExecutionCounts{Total: 4, Completed: 1, Failed: 1, Running: 1, Pending: 1}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestPlanChecksumShort(t *testing.T) {
	if PlanChecksumShort(nil) != "" {
		t.Fatalf("nil plan")
	}
	p := &model.Plan{}
	p.Metadata.Checksum = "sha256-0123456789abcdef"
	if got := PlanChecksumShort(p); got != "0123456789ab" {
		t.Fatalf("short = %q", got)
	}
}

func TestLoadPlanFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(path, []byte(`{"metadata":{"name":"demo"}}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	p, err := LoadPlanFile(path)
	if err != nil || p.Metadata.Name != "demo" {
		t.Fatalf("load = %+v, %v", p, err)
	}
	if _, err := LoadPlanFile(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatalf("expected error for missing file")
	}
}
