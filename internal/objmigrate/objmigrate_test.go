package objmigrate

import (
	"context"
	"testing"

	"github.com/sourceplane/orun/internal/clock"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/state"
)

func legacyWith(t *testing.T, withExec bool) (*state.Store, string, string) {
	t.Helper()
	ls := state.NewStore(t.TempDir())
	if err := ls.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	plan := &model.Plan{Metadata: model.PlanMetadata{Name: "demo"}, Jobs: []model.PlanJob{{ID: "a@deploy"}}}
	if err := ls.SavePlan(plan, "demo"); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}
	checksum := state.PlanChecksumShort(plan)
	execID := "exec-1"
	if withExec {
		if _, err := ls.CreateExecution(execID, plan); err != nil {
			t.Fatalf("CreateExecution: %v", err)
		}
		if err := ls.SaveState(execID, &state.ExecState{
			ExecID: execID, PlanChecksum: checksum,
			Jobs: map[string]*state.JobState{"a@deploy": {Status: "success", Steps: map[string]string{"build": "success"}}},
		}); err != nil {
			t.Fatalf("SaveState: %v", err)
		}
		if err := ls.SaveMetadata(execID, &state.ExecMetadata{ExecID: execID, Status: "success"}); err != nil {
			t.Fatalf("SaveMetadata: %v", err)
		}
	}
	return ls, checksum, execID
}

func objstores(t *testing.T) (*objectstore.LocalStore, *refstore.LocalRefStore) {
	t.Helper()
	root := t.TempDir()
	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Clock: clock.Fixed{}})
	if err != nil {
		t.Fatalf("refs: %v", err)
	}
	return store, refs
}

func TestMigrateHappyPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	legacy, checksum, execID := legacyWith(t, true)
	store, refs := objstores(t)

	res, err := Migrate(ctx, legacy, store, refs, false)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if res.Plans != 1 || res.Executions != 1 || res.OrphanExecutions != 0 {
		t.Fatalf("result = %+v", res)
	}
	// Revision is reachable via a by-hash ref and present in the store.
	revRef, err := refs.Read(ctx, "revisions/by-hash/"+sanitizeSeg(checksum))
	if err != nil {
		t.Fatalf("revision ref: %v", err)
	}
	if has, _ := store.Has(ctx, objectstore.ObjectID(revRef.Target)); !has {
		t.Fatalf("revision object missing")
	}
	// Execution sealed with a by-id ref.
	execRef, err := refs.Read(ctx, "executions/by-id/"+execID)
	if err != nil {
		t.Fatalf("execution ref: %v", err)
	}
	if has, _ := store.Has(ctx, objectstore.ObjectID(execRef.Target)); !has {
		t.Fatalf("execution object missing")
	}
}

func TestMigrateIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	legacy, checksum, execID := legacyWith(t, true)
	store, refs := objstores(t)

	if _, err := Migrate(ctx, legacy, store, refs, false); err != nil {
		t.Fatalf("Migrate 1: %v", err)
	}
	rev1, _ := refs.Read(ctx, "revisions/by-hash/"+sanitizeSeg(checksum))
	exec1, _ := refs.Read(ctx, "executions/by-id/"+execID)

	if _, err := Migrate(ctx, legacy, store, refs, false); err != nil {
		t.Fatalf("Migrate 2: %v", err)
	}
	rev2, _ := refs.Read(ctx, "revisions/by-hash/"+sanitizeSeg(checksum))
	exec2, _ := refs.Read(ctx, "executions/by-id/"+execID)

	if rev1.Target != rev2.Target || exec1.Target != exec2.Target {
		t.Fatalf("migration not idempotent: rev %s/%s exec %s/%s", rev1.Target, rev2.Target, exec1.Target, exec2.Target)
	}
}

func TestMigrateDryRun(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	legacy, _, _ := legacyWith(t, true)
	store, refs := objstores(t)

	res, err := Migrate(ctx, legacy, store, refs, true)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if !res.DryRun || res.Plans != 1 || res.Executions != 1 {
		t.Fatalf("dry-run result = %+v", res)
	}
	// Nothing written.
	names, _ := refs.List(ctx, "")
	if len(names) != 0 {
		t.Fatalf("dry-run wrote refs: %v", names)
	}
	empty := true
	_ = store.Iterate(ctx, func(objectstore.ObjectID) error { empty = false; return nil })
	if !empty {
		t.Fatalf("dry-run wrote objects")
	}
}

func TestMigrateOrphanExecution(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ls := state.NewStore(t.TempDir())
	if err := ls.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	// An execution whose plan checksum has no matching plan.
	execID := "exec-orphan"
	plan := &model.Plan{Metadata: model.PlanMetadata{Name: "gone"}, Jobs: []model.PlanJob{{ID: "a"}}}
	if _, err := ls.CreateExecution(execID, plan); err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	if err := ls.SaveState(execID, &state.ExecState{ExecID: execID, PlanChecksum: "sha256-orphanchecksum",
		Jobs: map[string]*state.JobState{"a": {Status: "success"}}}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	store, refs := objstores(t)

	res, err := Migrate(ctx, ls, store, refs, false)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if res.OrphanExecutions != 1 || res.Executions != 1 {
		t.Fatalf("orphan result = %+v", res)
	}
	// The orphan execution was still sealed.
	if _, err := refs.Read(ctx, "executions/by-id/"+execID); err != nil {
		t.Fatalf("orphan execution not sealed: %v", err)
	}
}

func TestSanitizeSeg(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"sha256-abc": "sha256-abc", "a/b@c": "a-b-c", "": "x", "***": "x"} {
		if got := sanitizeSeg(in); got != want {
			t.Fatalf("sanitizeSeg(%q) = %q, want %q", in, got, want)
		}
	}
}
