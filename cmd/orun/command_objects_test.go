package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/clock"
	"github.com/sourceplane/orun/internal/execseal"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/nodewriter"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/objgc"
	"github.com/sourceplane/orun/internal/state"
)

func objectsRig(t *testing.T) (*objectstore.LocalStore, *refstore.LocalRefStore, string, objectstore.ObjectID) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "objectmodel")
	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Clock: clock.Fixed{}})
	if err != nil {
		t.Fatalf("refs: %v", err)
	}
	revID, err := nodes.AssembleRevision(context.Background(), store,
		nodes.PlanRevision{Scope: nodes.RevisionScope{Mode: "full"}, JobCount: 2}, []byte(`{"plan":"A"}`))
	if err != nil {
		t.Fatalf("AssembleRevision: %v", err)
	}
	if err := refs.Update(context.Background(), "revisions/latest", "", string(revID)); err != nil {
		t.Fatalf("ref: %v", err)
	}
	return store, refs, root, revID
}

func TestRunObjectsRevParseCatLsTree(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, refs, _, revID := objectsRig(t)

	var buf bytes.Buffer
	if err := runObjectsRevParse(ctx, refs, "revisions/latest", &buf); err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	if strings.TrimSpace(buf.String()) != string(revID) {
		t.Fatalf("rev-parse = %q, want %s", buf.String(), revID)
	}

	// cat of the tree shows its entries; cat of the revision.json blob shows the
	// pretty-printed record.
	entries, err := store.GetTree(ctx, revID)
	if err != nil {
		t.Fatalf("GetTree: %v", err)
	}
	var revBlob objectstore.ObjectID
	for _, e := range entries {
		if e.Name == "revision.json" {
			revBlob = e.ID
		}
	}
	buf.Reset()
	if err := runObjectsCat(ctx, store, refs, string(revBlob), &buf); err != nil {
		t.Fatalf("cat: %v", err)
	}
	if !strings.Contains(buf.String(), `"kind": "PlanRevision"`) {
		t.Fatalf("cat output = %s", buf.String())
	}

	buf.Reset()
	if err := runObjectsLsTree(ctx, store, refs, "revisions/latest", &buf); err != nil {
		t.Fatalf("ls-tree: %v", err)
	}
	if !strings.Contains(buf.String(), "revision.json") || !strings.Contains(buf.String(), "plan.json") {
		t.Fatalf("ls-tree output = %s", buf.String())
	}
}

func TestRunObjectsFsckHealthyAndCorrupt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, refs, root, _ := objectsRig(t)

	var buf bytes.Buffer
	if err := runObjectsFsck(ctx, store, refs, &buf); err != nil {
		t.Fatalf("fsck healthy: %v", err)
	}
	if !strings.Contains(buf.String(), "healthy") {
		t.Fatalf("fsck output = %s", buf.String())
	}

	// Corrupt an object and expect a non-nil error.
	objFile := ""
	_ = filepath.Walk(filepath.Join(root, "objects"), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && !strings.HasPrefix(filepath.Base(p), "tmp-") && objFile == "" {
			objFile = p
		}
		return nil
	})
	if err := os.WriteFile(objFile, []byte("garbage"), 0o644); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	buf.Reset()
	if err := runObjectsFsck(ctx, store, refs, &buf); err == nil {
		t.Fatalf("fsck should fail on corruption; output=%s", buf.String())
	}
}

func TestRunObjectsCheckoutAndErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, refs, root, _ := objectsRig(t)

	var buf bytes.Buffer
	if err := runObjectsCheckout(ctx, store, refs, root, "revisions/latest", &buf); err != nil {
		t.Fatalf("checkout: %v", err)
	}
	dest := filepath.Join(root, "current", "revisions-latest")
	if _, err := os.Stat(filepath.Join(dest, "revision.json")); err != nil {
		t.Fatalf("checkout did not materialize revision.json: %v", err)
	}

	// Unknown ref surfaces an error on each command.
	if err := runObjectsCat(ctx, store, refs, "nope/x", &buf); err == nil {
		t.Fatalf("cat(unknown) should error")
	}
	if err := runObjectsCheckout(ctx, store, refs, root, "nope/x", &buf); err == nil {
		t.Fatalf("checkout(unknown) should error")
	}
}

func TestSanitizeCheckoutName(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"revisions/latest": "revisions-latest", "a.b_c-1": "a.b_c-1", "": "object"} {
		if got := sanitizeCheckoutName(in); got != want {
			t.Fatalf("sanitizeCheckoutName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRunObjectsLogAndReindex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, refs, root, revID := objectsRig(t)

	// Seal an execution under the revision.
	sealer := execseal.New(nodewriter.New(store, refs))
	if _, err := sealer.Seal(ctx, execseal.SealInput{
		RevisionID: revID, ExecutionID: "exec_001", ExecutionKey: "run-001",
		Status: nodes.StatusSucceeded, StartedAt: time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC),
		Jobs: []nodes.JobInput{{Record: nodes.JobRun{JobID: "a", Folder: "j-1", Status: nodes.StatusSucceeded},
			Attempts: []nodes.AttemptInput{{Record: nodes.JobAttempt{Attempt: 1, Status: nodes.StatusSucceeded}}}}},
	}); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	var buf bytes.Buffer
	if err := runObjectsLog(ctx, store, refs, root, &buf); err != nil {
		t.Fatalf("log: %v", err)
	}
	if !strings.Contains(buf.String(), "exec_001") || !strings.Contains(buf.String(), "succeeded") {
		t.Fatalf("log output = %s", buf.String())
	}

	buf.Reset()
	if err := runObjectsReindex(ctx, store, refs, root, &buf); err != nil {
		t.Fatalf("reindex: %v", err)
	}
	if !strings.Contains(buf.String(), "reindexed 1 executions") {
		t.Fatalf("reindex output = %s", buf.String())
	}

	// Empty store → "no executions".
	s2, r2, root2, _ := objectsRig(t)
	buf.Reset()
	if err := runObjectsLog(ctx, s2, r2, root2, &buf); err != nil {
		t.Fatalf("log empty: %v", err)
	}
	if !strings.Contains(buf.String(), "no executions") {
		t.Fatalf("empty log output = %s", buf.String())
	}
}

func TestRunObjectsGC(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, refs, root, revID := objectsRig(t)
	// Seal an execution (reachable), then add an unreachable orphan.
	sealer := execseal.New(nodewriter.New(store, refs))
	if _, err := sealer.Seal(ctx, execseal.SealInput{
		RevisionID: revID, ExecutionID: "exec_001", Status: nodes.StatusSucceeded, StartedAt: time.Now(),
		Jobs: []nodes.JobInput{{Record: nodes.JobRun{JobID: "a", Folder: "j-1", Status: nodes.StatusSucceeded},
			Attempts: []nodes.AttemptInput{{Record: nodes.JobAttempt{Attempt: 1, Status: nodes.StatusSucceeded}}}}},
	}); err != nil {
		t.Fatalf("Seal: %v", err)
	}
	orphan, _ := store.PutBlob(ctx, []byte("unreachable orphan"))

	var buf bytes.Buffer
	if err := runObjectsGC(ctx, store, refs, root, objgc.Options{}, &buf); err != nil {
		t.Fatalf("gc: %v", err)
	}
	if !strings.Contains(buf.String(), "swept=") {
		t.Fatalf("gc output = %s", buf.String())
	}
	if has, _ := store.Has(ctx, orphan); has {
		t.Fatalf("gc did not sweep the orphan")
	}
}

func TestRunObjectsMigrate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// Legacy store with one plan + one execution.
	legacy := state.NewStore(t.TempDir())
	if err := legacy.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	plan := &model.Plan{Metadata: model.PlanMetadata{Name: "demo"}, Jobs: []model.PlanJob{{ID: "a"}}}
	if err := legacy.SavePlan(plan, "demo"); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}
	checksum := state.PlanChecksumShort(plan)
	if _, err := legacy.CreateExecution("exec-1", plan); err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	_ = legacy.SaveState("exec-1", &state.ExecState{ExecID: "exec-1", PlanChecksum: checksum,
		Jobs: map[string]*state.JobState{"a": {Status: "success"}}})
	_ = legacy.SaveMetadata("exec-1", &state.ExecMetadata{ExecID: "exec-1", Status: "success"})

	store, refs, _, _ := objectsRig(t)
	var buf bytes.Buffer
	if err := runObjectsMigrate(ctx, legacy, store, refs, false, &buf); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !strings.Contains(buf.String(), "plans=1") || !strings.Contains(buf.String(), "executions=1") {
		t.Fatalf("migrate output = %s", buf.String())
	}
}
