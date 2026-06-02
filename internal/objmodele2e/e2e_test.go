package objmodele2e

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/clock"
	"github.com/sourceplane/orun/internal/execseal"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/nodewriter"
	"github.com/sourceplane/orun/internal/objgc"
	"github.com/sourceplane/orun/internal/objindex"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/objplan"
	"github.com/sourceplane/orun/internal/objremote"
	"github.com/sourceplane/orun/internal/sourcectx"
	"github.com/sourceplane/orun/internal/workingview"
)

type endpoint struct {
	store *objectstore.LocalStore
	refs  *refstore.LocalRefStore
	w     *nodewriter.Writer
	root  string
	memo  *objplan.ResolveMemo
}

func newEndpoint(t *testing.T) endpoint {
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
	n := 0
	w := nodewriter.New(store, refs, nodewriter.WithIDGen(func() string { n++; return fmt.Sprintf("trg_%05d", n) }))
	return endpoint{store: store, refs: refs, w: w, root: root, memo: objplan.NewResolveMemo(root)}
}

func (e endpoint) remote() objremote.Endpoint {
	return objremote.Endpoint{Objects: e.store, Refs: e.refs}
}

func catalogView(nComponents int) *catalogresolve.CatalogView {
	manifests := make([]*catalogmodel.ComponentManifest, 0, nComponents)
	nodesList := make([]catalogmodel.GraphNode, 0, nComponents)
	for i := 0; i < nComponents; i++ {
		name := fmt.Sprintf("svc%d", i)
		key := "ns/repo/" + name
		manifests = append(manifests, &catalogmodel.ComponentManifest{
			Identity: catalogmodel.ComponentIdentity{ComponentKey: key, Name: name, Namespace: "ns", Repo: "repo"},
		})
		nodesList = append(nodesList, catalogmodel.GraphNode{Key: key, Kind: "Component", Name: name})
	}
	return &catalogresolve.CatalogView{
		ResolvedCatalog: &catalogresolve.ResolvedCatalog{Manifests: manifests},
		Snapshot:        &catalogmodel.CatalogSnapshot{CatalogSnapshotKey: "cat-e2e"},
		Graphs:          []*catalogmodel.CatalogGraph{{Nodes: nodesList}},
	}
}

func planInput(view *catalogresolve.CatalogView, planBytes []byte, n int) objplan.Input {
	return objplan.Input{
		Workspace:      sourcectx.WorkspaceState{Repo: "ns/repo", HeadRevision: "abc123def456", TreeHash: "9aa7710", Branch: "main"},
		SourceHumanKey: "src-main-abc",
		Resolve:        func() (*catalogresolve.CatalogView, error) { return view, nil },
		PlanBytes:      planBytes,
		RevisionScope:  nodes.RevisionScope{Mode: "full"},
		JobCount:       1,
		Trigger: nodes.TriggerOccurrence{
			TriggerName: "system.manual",
			Source:      nodes.TriggerSource{Flavor: "system", System: "manual"},
			Scope:       nodes.RevisionScope{Mode: "full"}, Actor: "cli",
		},
	}
}

func closureCount(t *testing.T, s objectstore.ObjectStore, id objectstore.ObjectID) int {
	t.Helper()
	n := 0
	if err := s.Walk(context.Background(), id, func(objectstore.ObjectID, objectstore.Kind) error { n++; return nil }); err != nil {
		t.Fatalf("walk: %v", err)
	}
	return n
}

func objectCount(t *testing.T, root string) (count int, bytes int64) {
	t.Helper()
	_ = filepath.WalkDir(filepath.Join(root, "objects"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			count++
			if info, e := d.Info(); e == nil {
				bytes += info.Size()
			}
		}
		return nil
	})
	return count, bytes
}

// TestObjectModelE2E walks the full pipeline end-to-end across every layer.
func TestObjectModelE2E(t *testing.T) {
	ctx := context.Background()
	local := newEndpoint(t)
	view := catalogView(3)

	// 1. Plan: source → catalog → revision → trigger.
	res, err := objplan.Plan(ctx, local.w, local.store, local.memo, planInput(view, []byte(`{"plan":"A"}`), 1), objplan.Options{})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if res.SourceID == "" || res.CatalogID == "" || res.RevisionID == "" || res.TriggerID == "" {
		t.Fatalf("incomplete plan result: %+v", res)
	}

	// 2. Re-plan the SAME plan (different trigger): revision deduped, catalog memoized.
	res2, err := objplan.Plan(ctx, local.w, local.store, local.memo, planInput(view, []byte(`{"plan":"A"}`), 2), objplan.Options{})
	if err != nil {
		t.Fatalf("re-plan: %v", err)
	}
	if res2.RevisionID != res.RevisionID || res2.CatalogID != res.CatalogID {
		t.Fatalf("identical plan should reuse revision+catalog: %+v vs %+v", res, res2)
	}
	if !res2.RevisionReused {
		t.Fatalf("second plan should report revision reused")
	}
	if res2.TriggerID == res.TriggerID {
		t.Fatalf("triggers must be distinct events")
	}

	// 3. Seal an execution under the revision.
	sealer := execseal.New(local.w)
	execID, err := sealer.Seal(ctx, execseal.SealInput{
		RevisionID: res.RevisionID, TriggerID: "trg_1", ExecutionID: "exec_e2e", ExecutionKey: "run-001",
		Status: nodes.StatusSucceeded, StartedAt: time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC),
		Jobs: []nodes.JobInput{{Record: nodes.JobRun{JobID: "svc0@deploy", Folder: "j-1", Status: nodes.StatusSucceeded},
			Attempts: []nodes.AttemptInput{{Record: nodes.JobAttempt{Attempt: 1, Status: nodes.StatusSucceeded},
				Steps: []nodes.StepInput{{Record: nodes.StepAttempt{StepID: "build", Status: nodes.StatusSucceeded}, Log: []byte("ok")}}}}}},
	})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	// 4. Index lists the execution.
	ix := objindex.New(local.store, local.refs, local.root)
	entries, err := ix.ListExecutions(ctx)
	if err != nil || len(entries) != 1 || entries[0].ExecutionID != "exec_e2e" {
		t.Fatalf("index list = %+v, %v", entries, err)
	}

	// 5. fsck is clean.
	if problems, err := workingview.Fsck(ctx, local.store, local.refs); err != nil || len(problems) != 0 {
		t.Fatalf("fsck local = %v, %v", problems, err)
	}

	// 6. Push executions/latest to a remote.
	remoteEP := newEndpoint(t)
	push, err := objremote.Push(ctx, local.remote(), remoteEP.remote(), "executions/latest")
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if push.Copied != push.Closure || push.Copied == 0 || !push.RefMoved {
		t.Fatalf("push = %+v", push)
	}

	// 7. Pull into a fresh endpoint; it can read the sealed execution.
	fresh := newEndpoint(t)
	pull, err := objremote.Pull(ctx, fresh.remote(), remoteEP.remote(), "executions/latest")
	if err != nil || pull.Copied == 0 {
		t.Fatalf("pull = %+v, %v", pull, err)
	}
	if r, err := fresh.refs.Read(ctx, "executions/latest"); err != nil || r.Target != string(execID) {
		t.Fatalf("fresh executions/latest = %v, %v", r, err)
	}
	if problems, err := workingview.Fsck(ctx, fresh.store, fresh.refs); err != nil || len(problems) != 0 {
		t.Fatalf("fsck fresh = %v, %v", problems, err)
	}

	// 8. GC keeps the reachable graph intact (an unreachable orphan is swept).
	orphan, _ := local.store.PutBlob(ctx, []byte("orphan"))
	gc, err := objgc.Collect(ctx, local.store, local.refs, ix, objgc.Options{})
	if err != nil {
		t.Fatalf("gc: %v", err)
	}
	if gc.Swept < 1 {
		t.Fatalf("gc swept nothing: %+v", gc)
	}
	if has, _ := local.store.Has(ctx, orphan); has {
		t.Fatalf("orphan survived gc")
	}
	if has, _ := local.store.Has(ctx, execID); !has {
		t.Fatalf("reachable execution swept by gc")
	}
	if has, _ := local.store.Has(ctx, res.RevisionID); !has {
		t.Fatalf("reachable revision swept by gc")
	}
}

// TestObjectModelDedupDiskWin proves the efficiency claim: planning many times
// against the SAME catalog stores the catalog once and adds only a few small
// objects per plan — far fewer than copying the catalog each time.
func TestObjectModelDedupDiskWin(t *testing.T) {
	ctx := context.Background()
	local := newEndpoint(t)
	view := catalogView(8) // a non-trivial catalog

	// First plan writes the catalog.
	res, err := objplan.Plan(ctx, local.w, local.store, local.memo, planInput(view, []byte(`{"plan":0}`), 0), objplan.Options{})
	if err != nil {
		t.Fatalf("plan 0: %v", err)
	}
	catObjects := closureCount(t, local.store, res.CatalogID)
	count1, _ := objectCount(t, local.root)

	// 49 more plans against the SAME catalog (memoized) with DIFFERENT plans.
	const more = 49
	for i := 1; i <= more; i++ {
		if _, err := objplan.Plan(ctx, local.w, local.store, local.memo,
			planInput(view, []byte(fmt.Sprintf(`{"plan":%d}`, i)), i), objplan.Options{}); err != nil {
			t.Fatalf("plan %d: %v", i, err)
		}
	}
	countN, _ := objectCount(t, local.root)

	perPlan := float64(countN-count1) / float64(more)
	t.Logf("catalog objects=%d, per-extra-plan objects=%.2f, total objects after %d plans=%d",
		catObjects, perPlan, more+1, countN)

	// The catalog (stored once) is far larger than the per-plan delta — i.e. the
	// catalog is shared, not copied. A naive copy-per-plan layout would add
	// ~catObjects per plan.
	if perPlan >= float64(catObjects) {
		t.Fatalf("per-plan delta %.2f not below catalog size %d — dedup not happening", perPlan, catObjects)
	}
	// And the total stays well under the naive copy-everything bound.
	naive := (more + 1) * catObjects
	if countN >= naive {
		t.Fatalf("total objects %d not below naive copy bound %d", countN, naive)
	}
}
