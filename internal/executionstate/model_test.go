package executionstate

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/statestore"
	"github.com/sourceplane/orun/internal/testfx/statefs"
)

func TestKindConstants(t *testing.T) {
	if KindName != "ExecutionRun" {
		t.Fatalf("KindName=%q want %q", KindName, "ExecutionRun")
	}
	if APIVersion != "orun.io/v1alpha1" {
		t.Fatalf("APIVersion=%q", APIVersion)
	}
}

func TestIsTerminal(t *testing.T) {
	cases := map[string]bool{
		StatusPending:   false,
		StatusRunning:   false,
		StatusCompleted: true,
		StatusFailed:    true,
		StatusCancelled: true,
		"weird":         false,
	}
	for s, want := range cases {
		if got := IsTerminal(s); got != want {
			t.Errorf("IsTerminal(%q)=%v want %v", s, got, want)
		}
	}
}

func TestExecutionRun_RoundTripBytes(t *testing.T) {
	now := time.Date(2026, 5, 30, 18, 0, 0, 0, time.UTC)
	rec := ExecutionRun{
		APIVersion:   APIVersion,
		Kind:         KindName,
		ExecutionID:  "exec_01J",
		ExecutionKey: "run-001",
		RevisionID:   "rev_01J",
		RevisionKey:  "rev-test",
		TriggerID:    "trg_01J",
		TriggerKey:   "trg-key",
		Reason:       ReasonDirectRun,
		Status:       StatusPending,
		Attempt:      1,
		Runner:       RunnerProfile{Mode: "local", Backend: "local", Platform: "darwin"},
		Summary:      ExecSummary{Total: 3, Pending: 3},
		CreatedAt:    now,
	}
	a := marshalCanonicalJSON(rec)
	b := marshalCanonicalJSON(rec)
	if string(a) != string(b) {
		t.Fatalf("non-deterministic encode")
	}
	if !strings.HasSuffix(string(a), "\n") {
		t.Fatalf("encoded bytes missing trailing newline: %q", a)
	}
	if strings.Contains(string(a), "\\u0026") {
		t.Fatalf("HTML escaping detected in canonical encoding")
	}
	// stable key order — apiVersion appears before kind appears before executionId
	idxAPI := strings.Index(string(a), `"apiVersion"`)
	idxKind := strings.Index(string(a), `"kind"`)
	idxExec := strings.Index(string(a), `"executionId"`)
	if !(idxAPI < idxKind && idxKind < idxExec) {
		t.Fatalf("unexpected key order: %s", a)
	}

	var rt ExecutionRun
	if err := json.Unmarshal(a, &rt); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rt.ExecutionKey != rec.ExecutionKey || rt.Status != rec.Status {
		t.Fatalf("round-trip mismatch: %+v", rt)
	}
}

func TestExecutionRun_JSONFile_AssertJSONFile(t *testing.T) {
	root := statefs.NewWorkspace(t)
	store, err := statestore.NewLocalStore(statestore.LocalConfig{Root: filepath.Join(root, ".orun")})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	now := time.Date(2026, 5, 30, 18, 0, 0, 0, time.UTC)
	rec := ExecutionRun{
		APIVersion:   APIVersion,
		Kind:         KindName,
		ExecutionID:  "exec_01J",
		ExecutionKey: "run-001",
		RevisionID:   "rev_01J",
		RevisionKey:  "rev-test",
		TriggerID:    "trg_01J",
		TriggerKey:   "trg-key",
		Reason:       ReasonDirectRun,
		Status:       StatusPending,
		Attempt:      1,
		Runner:       RunnerProfile{Mode: "local", Backend: "local", Platform: "darwin"},
		Summary:      ExecSummary{Total: 1, Pending: 1},
		CreatedAt:    now,
	}
	p := statestore.ExecutionDocPath("rev-test", "run-001")
	if _, err := store.Write(context.Background(), p, marshalCanonicalJSON(rec), statestore.WriteOptions{}); err != nil {
		t.Fatalf("write: %v", err)
	}
	abs := filepath.Join(root, ".orun", filepath.FromSlash(p))
	statefs.AssertJSONFile(t, abs, map[string]any{
		"apiVersion":   APIVersion,
		"kind":         KindName,
		"executionId":  "exec_01J",
		"executionKey": "run-001",
		"revisionId":   "rev_01J",
		"revisionKey":  "rev-test",
		"triggerId":    "trg_01J",
		"triggerKey":   "trg-key",
		"reason":       ReasonDirectRun,
		"status":       StatusPending,
		"attempt":      1,
		"runner":       map[string]any{"mode": "local", "backend": "local", "platform": "darwin"},
		"summary":      map[string]any{"total": 1, "completed": 0, "failed": 0, "running": 0, "pending": 1},
		"createdAt":    now,
	})
}

func TestExecutionRun_OmitEmptyOriginalKey(t *testing.T) {
	rec := ExecutionRun{APIVersion: APIVersion, Kind: KindName}
	b := marshalCanonicalJSON(rec)
	if strings.Contains(string(b), "originalKey") {
		t.Fatalf("originalKey should be omitempty when blank: %s", b)
	}
	if strings.Contains(string(b), "startedAt") {
		t.Fatalf("startedAt should be omitempty when nil: %s", b)
	}
}

func TestStrictAndLooseUnmarshal(t *testing.T) {
	good := []byte(`{"a":1}`)
	type S struct {
		A int `json:"a"`
	}
	var s1, s2 S
	if err := strictJSON(good, &s1); err != nil {
		t.Fatalf("strictJSON good: %v", err)
	}
	if err := looseUnmarshal(good, &s2); err != nil {
		t.Fatalf("looseUnmarshal good: %v", err)
	}
	if err := strictJSON([]byte(`{"a":1,"b":2}`), &s1); err == nil {
		t.Fatalf("strictJSON should reject unknown fields")
	}
	if err := strictJSON([]byte(`not json`), &s1); err == nil {
		t.Fatalf("strictJSON should fail bad json")
	}
	if err := looseUnmarshal([]byte(`not json`), &s2); err == nil {
		t.Fatalf("looseUnmarshal should fail bad json")
	}
	if !equalBytes([]byte("a"), []byte("a")) || equalBytes([]byte("a"), []byte("b")) {
		t.Fatal("equalBytes broken")
	}
}
