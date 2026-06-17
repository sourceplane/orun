package objmodel

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/clock"
	"github.com/sourceplane/orun/internal/execseal"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/nodewriter"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/objplan"
	"github.com/sourceplane/orun/internal/objremote"
	"github.com/sourceplane/orun/internal/sourcectx"
)

// graph builds a full object graph (source → catalog → revision → trigger, plus
// two sealed executions) and returns a Reader over it. It uses the same
// high-level entry points the CLI uses (objplan.Plan, execseal.Seal), so the
// fixture is faithful to production writes rather than hand-rolled trees.
type graph struct {
	r       *Reader
	store   *objectstore.LocalStore
	refs    *refstore.LocalRefStore
	root    string
	revID   objectstore.ObjectID
	catID   objectstore.ObjectID
	srcID   objectstore.ObjectID
	execIDs []string
}

func newGraph(t *testing.T) graph {
	t.Helper()
	ctx := context.Background()
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
	memo := objplan.NewResolveMemo(root)

	view := catalogView("api", "web")
	planBytes := []byte(`{"metadata":{"name":"deploy-all"},"jobs":[{"component":"ns/repo/api"},{"component":"ns/repo/web"}]}`)
	res, err := objplan.Plan(ctx, w, store, memo, planInput(view, planBytes), objplan.Options{})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	sealer := execseal.New(w)
	var execIDs []string
	for i, status := range []string{nodes.StatusSucceeded, nodes.StatusFailed} {
		id, err := sealer.Seal(ctx, execseal.SealInput{
			RevisionID: res.RevisionID,
			// trg_00001 is the logical id the IDGen minted for the single plan.
			TriggerID:    "trg_00001",
			ExecutionID:  fmt.Sprintf("exec_%02d", i),
			ExecutionKey: fmt.Sprintf("run-%03d", i),
			Status:       status,
			StartedAt:    time.Date(2026, 6, 2, 10, i, 0, 0, time.UTC),
			Jobs: []nodes.JobInput{{
				Record: nodes.JobRun{JobID: "ns/repo/api@deploy", Folder: "j-1", Status: status},
				Attempts: []nodes.AttemptInput{{
					Record: nodes.JobAttempt{Attempt: 1, Status: status},
					Steps:  []nodes.StepInput{{Record: nodes.StepAttempt{StepID: "build", Status: status}, Log: []byte("log")}},
				}},
			}},
		})
		if err != nil {
			t.Fatalf("seal %d: %v", i, err)
		}
		execIDs = append(execIDs, string(id))
	}

	return graph{
		r:       NewReader(store, refs, root),
		store:   store,
		refs:    refs,
		root:    root,
		revID:   res.RevisionID,
		catID:   res.CatalogID,
		srcID:   res.SourceID,
		execIDs: execIDs,
	}
}

func catalogView(names ...string) *catalogresolve.CatalogView {
	manifests := make([]*catalogmodel.ComponentManifest, 0, len(names))
	graphNodes := make([]catalogmodel.GraphNode, 0, len(names))
	for _, name := range names {
		key := "ns/repo/" + name
		manifests = append(manifests, &catalogmodel.ComponentManifest{
			Identity: catalogmodel.ComponentIdentity{ComponentKey: key, Name: name, Namespace: "ns", Repo: "repo"},
		})
		graphNodes = append(graphNodes, catalogmodel.GraphNode{Key: key, Kind: "Component", Name: name})
	}
	return &catalogresolve.CatalogView{
		ResolvedCatalog: &catalogresolve.ResolvedCatalog{Manifests: manifests},
		Snapshot:        &catalogmodel.CatalogSnapshot{CatalogSnapshotKey: "cat-test"},
		Graphs:          []*catalogmodel.CatalogGraph{{Nodes: graphNodes}},
	}
}

func planInput(view *catalogresolve.CatalogView, planBytes []byte) objplan.Input {
	return objplan.Input{
		Workspace:      sourcectx.WorkspaceState{Repo: "ns/repo", HeadRevision: "abc123def456", TreeHash: "9aa7710", Branch: "main"},
		SourceHumanKey: "src-main-abc",
		Resolve:        func() (*catalogresolve.CatalogView, error) { return view, nil },
		PlanBytes:      planBytes,
		RevisionScope:  nodes.RevisionScope{Mode: "full"},
		JobCount:       2,
		Trigger: nodes.TriggerOccurrence{
			TriggerName: "system.manual",
			Source:      nodes.TriggerSource{Flavor: "system", System: "manual"},
			Scope:       nodes.RevisionScope{Mode: "full"}, Actor: "cli",
		},
	}
}

func TestResolveRef(t *testing.T) {
	ctx := context.Background()
	g := newGraph(t)

	id, err := g.r.ResolveRef(ctx, "catalogs/current")
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}
	if id != g.catID {
		t.Fatalf("catalogs/current = %s, want %s", id, g.catID)
	}
	if _, err := g.r.ResolveRef(ctx, "sources/current"); err != nil {
		t.Fatalf("ResolveRef sources/current: %v", err)
	}
	if _, err := g.r.ResolveRef(ctx, ""); err == nil {
		t.Fatal("empty ref should error")
	}
	if _, err := g.r.ResolveRef(ctx, "no/such/ref"); err == nil {
		t.Fatal("missing ref should error")
	}
}

func TestCatalog(t *testing.T) {
	ctx := context.Background()
	g := newGraph(t)

	// Default ref resolves to catalogs/current.
	v, err := g.r.Catalog(ctx, "")
	if err != nil {
		t.Fatalf("Catalog(default): %v", err)
	}
	if v.ObjectID != g.catID {
		t.Fatalf("catalog id = %s, want %s", v.ObjectID, g.catID)
	}
	if len(v.Components) != 2 {
		t.Fatalf("components = %d, want 2", len(v.Components))
	}
	// Bare-id read returns the same view.
	byID, err := g.r.Catalog(ctx, string(g.catID))
	if err != nil {
		t.Fatalf("Catalog(id): %v", err)
	}
	if byID.ObjectID != g.catID {
		t.Fatalf("catalog-by-id id = %s, want %s", byID.ObjectID, g.catID)
	}
}

func TestRevision(t *testing.T) {
	ctx := context.Background()
	g := newGraph(t)

	v, err := g.r.Revision(ctx, "")
	if err != nil {
		t.Fatalf("Revision(default): %v", err)
	}
	if v.ObjectID != g.revID {
		t.Fatalf("revision id = %s, want %s", v.ObjectID, g.revID)
	}
	if v.CatalogID != string(g.catID) {
		t.Fatalf("revision.catalogId = %s, want %s", v.CatalogID, g.catID)
	}
	if v.SourceID != string(g.srcID) {
		t.Fatalf("revision.sourceId = %s, want %s", v.SourceID, g.srcID)
	}
	if v.PlanName != "deploy-all" {
		t.Fatalf("plan name = %q, want deploy-all", v.PlanName)
	}
	if len(v.Components) != 2 || v.Components[0] != "ns/repo/api" {
		t.Fatalf("revision components = %v", v.Components)
	}
	if v.ScopeMode != "full" {
		t.Fatalf("scope mode = %q, want full", v.ScopeMode)
	}
}

func TestExecution(t *testing.T) {
	ctx := context.Background()
	g := newGraph(t)

	v, err := g.r.Execution(ctx, "executions/by-id/exec_00")
	if err != nil {
		t.Fatalf("Execution: %v", err)
	}
	if v.ExecutionID != "exec_00" {
		t.Fatalf("execution id = %q, want exec_00", v.ExecutionID)
	}
	if v.Status != nodes.StatusSucceeded {
		t.Fatalf("status = %q, want succeeded", v.Status)
	}
	if len(v.Jobs) != 1 {
		t.Fatalf("jobs = %d, want 1 (full detail)", len(v.Jobs))
	}
	// Bare id resolves via executions/by-id.
	if _, err := g.r.Execution(ctx, "exec_01"); err != nil {
		t.Fatalf("Execution(bare id): %v", err)
	}
}

func TestListExecutions(t *testing.T) {
	ctx := context.Background()
	g := newGraph(t)

	all, err := g.r.ListExecutions(ctx, Filter{})
	if err != nil {
		t.Fatalf("ListExecutions: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("list = %d, want 2", len(all))
	}
	// Newest-first: exec_01 started a minute after exec_00.
	if all[0].ExecutionID != "exec_01" {
		t.Fatalf("newest-first head = %q, want exec_01", all[0].ExecutionID)
	}
	if all[0].Summary.JobsTotal != 1 {
		t.Fatalf("summary jobsTotal = %d, want 1", all[0].Summary.JobsTotal)
	}

	// Status filter.
	failed, err := g.r.ListExecutions(ctx, Filter{Status: nodes.StatusFailed})
	if err != nil {
		t.Fatalf("ListExecutions(status): %v", err)
	}
	if len(failed) != 1 || failed[0].Status != nodes.StatusFailed {
		t.Fatalf("failed filter = %+v", failed)
	}

	// Limit.
	one, err := g.r.ListExecutions(ctx, Filter{Limit: 1})
	if err != nil {
		t.Fatalf("ListExecutions(limit): %v", err)
	}
	if len(one) != 1 {
		t.Fatalf("limit=1 returned %d", len(one))
	}

	// Component filter: both executions ran ns/repo/api.
	comp, err := g.r.ListExecutions(ctx, Filter{Component: "ns/repo/api"})
	if err != nil {
		t.Fatalf("ListExecutions(component): %v", err)
	}
	if len(comp) != 2 {
		t.Fatalf("component filter = %d, want 2", len(comp))
	}
	// A component nothing ran filters everything out.
	none, err := g.r.ListExecutions(ctx, Filter{Component: "ns/repo/ghost"})
	if err != nil {
		t.Fatalf("ListExecutions(ghost): %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("ghost component = %d, want 0", len(none))
	}
}

func TestComponentHistory(t *testing.T) {
	ctx := context.Background()
	g := newGraph(t)

	h, err := g.r.ComponentHistory(ctx, "ns/repo/api")
	if err != nil {
		t.Fatalf("ComponentHistory: %v", err)
	}
	if h.ComponentKey != "ns/repo/api" {
		t.Fatalf("component key = %q", h.ComponentKey)
	}
	if len(h.Executions) != 2 {
		t.Fatalf("history executions = %d, want 2", len(h.Executions))
	}
	if _, err := g.r.ComponentHistory(ctx, ""); err == nil {
		t.Fatal("empty component key should error")
	}
}

func TestReaderImplementsModelReader(t *testing.T) {
	var _ ModelReader = NewReader(nil, nil, "")
}

// --- substitutability: the SAME ModelReader over a remote-backed store ---

// memObjectTransport / memRefTransport are in-process fakes for the exported
// remote transports, backed by maps. They stand in for the hosted object bucket
// and ref KV so the integration test can prove the read seam is identical local
// vs remote without any HTTP.
type memObjectTransport struct{ objs map[string][]byte }

func (f *memObjectTransport) HasObject(_ context.Context, d string) (bool, error) {
	_, ok := f.objs[d]
	return ok, nil
}
func (f *memObjectTransport) GetObject(_ context.Context, d string) ([]byte, bool, error) {
	b, ok := f.objs[d]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), b...), true, nil
}
func (f *memObjectTransport) PutObject(_ context.Context, d, _ string, framed []byte) error {
	if _, ok := f.objs[d]; !ok {
		f.objs[d] = append([]byte(nil), framed...)
	}
	return nil
}

type memRefTransport struct{ refs map[string]string }

func (f *memRefTransport) ReadRef(_ context.Context, name string) (refstore.Ref, bool, error) {
	t, ok := f.refs[name]
	if !ok {
		return refstore.Ref{}, false, nil
	}
	return refstore.Ref{Kind: "Ref", Target: t, Writer: "saas"}, true, nil
}
func (f *memRefTransport) UpdateRef(_ context.Context, name, old, nw string) error {
	if f.refs[name] != old {
		return refstore.ErrConflict
	}
	if nw == "" {
		delete(f.refs, name)
		return nil
	}
	f.refs[name] = nw
	return nil
}
func (f *memRefTransport) ListRefs(_ context.Context, prefix string) ([]string, error) {
	var out []string
	for name := range f.refs {
		if len(name) >= len(prefix) && name[:len(prefix)] == prefix {
			out = append(out, name)
		}
	}
	return out, nil
}
func (f *memRefTransport) DeleteRef(_ context.Context, name string) error {
	delete(f.refs, name)
	return nil
}

func TestModelReaderOverRemoteStore(t *testing.T) {
	ctx := context.Background()
	g := newGraph(t)

	// A remote endpoint backed by in-memory transports.
	remoteStore := objectstore.NewRemoteStore(&memObjectTransport{objs: map[string][]byte{}}, "", "")
	remoteRefs := refstore.NewRemoteRefStore(&memRefTransport{refs: map[string]string{}})
	local := objremote.Endpoint{Objects: g.store, Refs: g.refs}
	remote := objremote.Endpoint{Objects: remoteStore, Refs: remoteRefs}

	// Push every published ref's closure into the remote — set-difference sync
	// over content addressing, exactly as `orun push` does.
	names, err := g.refs.List(ctx, "")
	if err != nil {
		t.Fatalf("list local refs: %v", err)
	}
	for _, name := range names {
		if _, err := objremote.Push(ctx, local, remote, name); err != nil {
			t.Fatalf("push %s: %v", name, err)
		}
	}

	// The SAME objmodel.Reader type, now over the remote pair with no working
	// tree (root ""), serves the identical views.
	rr := NewReader(remoteStore, remoteRefs, "")

	cat, err := rr.Catalog(ctx, "")
	if err != nil {
		t.Fatalf("remote Catalog: %v", err)
	}
	if cat.ObjectID != g.catID || len(cat.Components) != 2 {
		t.Fatalf("remote catalog = %s / %d components", cat.ObjectID, len(cat.Components))
	}

	rev, err := rr.Revision(ctx, "")
	if err != nil {
		t.Fatalf("remote Revision: %v", err)
	}
	if rev.ObjectID != g.revID || rev.PlanName != "deploy-all" {
		t.Fatalf("remote revision = %s / %q", rev.ObjectID, rev.PlanName)
	}

	execs, err := rr.ListExecutions(ctx, Filter{})
	if err != nil {
		t.Fatalf("remote ListExecutions: %v", err)
	}
	if len(execs) != 2 || execs[0].ExecutionID != "exec_01" {
		t.Fatalf("remote executions = %+v", execs)
	}

	one, err := rr.Execution(ctx, "executions/by-id/exec_00")
	if err != nil {
		t.Fatalf("remote Execution: %v", err)
	}
	if one.Status != nodes.StatusSucceeded || len(one.Jobs) != 1 {
		t.Fatalf("remote execution detail = %+v", one)
	}

	hist, err := rr.ComponentHistory(ctx, "ns/repo/api")
	if err != nil {
		t.Fatalf("remote ComponentHistory: %v", err)
	}
	if len(hist.Executions) != 2 {
		t.Fatalf("remote history = %d execs", len(hist.Executions))
	}
}
