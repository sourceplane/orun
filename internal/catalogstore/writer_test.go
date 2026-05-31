package catalogstore_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogstore"
	"github.com/sourceplane/orun/internal/statestore"
)

// spyStore is a minimal in-memory StateStore that records call order and
// supports the operations PR-2 invokes (CreateIfAbsent, CompareAndSwap,
// Write, Read). It exists in this test file rather than depending on a
// Phase 1 helper per the PR's "inline locally" rule. PR-3 may move this
// into a shared internal/catalogstore/storetest package.
type spyStore struct {
	mu      sync.Mutex
	objects map[string][]byte
	// revisions is a per-path content-derived revision counter. We use a
	// monotonic uint64 incremented on every Write/CreateIfAbsent/CAS so
	// tests don't need to compute SHAs.
	revisions  map[string]string
	revCounter uint64
	// trace records every store operation as a typed string in call
	// order. PR-1 ops: "create:<path>", "write:<path>". PR-2 adds
	// "cas:<path>:<oldRev>" and "read:<path>".
	trace []string
	// failCreate, when set, returns the supplied error from the next
	// CreateIfAbsent for path == failCreatePath. Used to inject ErrExists
	// with a body the spy did not actually store, simulating a divergent
	// concurrent writer.
	failCreate     error
	failCreatePath string
	// preExisting maps path → body that should already be present
	// (returns ErrExists with that body on CreateIfAbsent).
	preExisting map[string][]byte
	// casConflicts queues per-path forced ErrConflict responses. Each
	// CompareAndSwap on a path with pending count > 0 returns ErrConflict
	// without modifying state, decrements the counter, and the spy
	// also bumps the on-disk revision so the caller's re-read picks up
	// a fresh oldRev. Used to drive retry-budget tests.
	casConflicts map[string]int
	// readErr / casErr / createErr inject a one-shot non-standard error
	// on the next Read / CompareAndSwap / CreateIfAbsent for the given
	// path. Used by verifier-attached coverage tests for the defensive
	// "non-Exists" / "non-Conflict" / "Read failed mid-CAS" branches.
	readErr     map[string]error
	casErr      map[string]error
	createNStdE map[string]error
	writeErr    map[string]error
}

func newSpyStore() *spyStore {
	return &spyStore{
		objects:      map[string][]byte{},
		revisions:    map[string]string{},
		preExisting:  map[string][]byte{},
		casConflicts: map[string]int{},
		readErr:      map[string]error{},
		casErr:       map[string]error{},
		createNStdE:  map[string]error{},
		writeErr:     map[string]error{},
	}
}

// nextRev returns a fresh per-store revision string. Monotonic, unique
// across paths.
func (s *spyStore) nextRev() string {
	s.revCounter++
	return fmt.Sprintf("rev-%d", s.revCounter)
}

func (s *spyStore) Root() string { return "(spy)" }

func (s *spyStore) Read(ctx context.Context, p string) ([]byte, statestore.ObjectMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trace = append(s.trace, "read:"+p)
	if e, ok := s.readErr[p]; ok && e != nil {
		delete(s.readErr, p)
		return nil, statestore.ObjectMeta{}, e
	}
	if b, ok := s.objects[p]; ok {
		rev := s.revisions[p]
		return append([]byte(nil), b...), statestore.ObjectMeta{Path: p, Size: int64(len(b)), Revision: rev}, nil
	}
	if b, ok := s.preExisting[p]; ok {
		rev := s.revisions[p]
		return append([]byte(nil), b...), statestore.ObjectMeta{Path: p, Size: int64(len(b)), Revision: rev}, nil
	}
	return nil, statestore.ObjectMeta{}, fmt.Errorf("%w: %s", statestore.ErrNotFound, p)
}

func (s *spyStore) Write(ctx context.Context, p string, data []byte, opts statestore.WriteOptions) (statestore.ObjectMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trace = append(s.trace, "write:"+p)
	if e, ok := s.writeErr[p]; ok && e != nil {
		delete(s.writeErr, p)
		return statestore.ObjectMeta{}, e
	}
	s.objects[p] = append([]byte(nil), data...)
	s.revisions[p] = s.nextRev()
	return statestore.ObjectMeta{Path: p, Size: int64(len(data)), Revision: s.revisions[p]}, nil
}

func (s *spyStore) CreateIfAbsent(ctx context.Context, p string, data []byte) (statestore.ObjectMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trace = append(s.trace, "create:"+p)
	if e, ok := s.createNStdE[p]; ok && e != nil {
		delete(s.createNStdE, p)
		return statestore.ObjectMeta{}, e
	}
	if s.failCreate != nil && p == s.failCreatePath {
		err := s.failCreate
		s.failCreate = nil
		s.failCreatePath = ""
		return statestore.ObjectMeta{}, err
	}
	if _, ok := s.objects[p]; ok {
		return statestore.ObjectMeta{}, fmt.Errorf("%w: %s", statestore.ErrExists, p)
	}
	if _, ok := s.preExisting[p]; ok {
		// Move it into objects so subsequent Reads see the same body and
		// future CreateIfAbsents continue to ErrExists.
		s.objects[p] = s.preExisting[p]
		if _, ok := s.revisions[p]; !ok {
			s.revisions[p] = s.nextRev()
		}
		delete(s.preExisting, p)
		return statestore.ObjectMeta{}, fmt.Errorf("%w: %s", statestore.ErrExists, p)
	}
	s.objects[p] = append([]byte(nil), data...)
	s.revisions[p] = s.nextRev()
	return statestore.ObjectMeta{Path: p, Size: int64(len(data)), Revision: s.revisions[p]}, nil
}

func (s *spyStore) CompareAndSwap(ctx context.Context, p string, oldRev string, data []byte) (statestore.ObjectMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trace = append(s.trace, "cas:"+p+":"+oldRev)
	if e, ok := s.casErr[p]; ok && e != nil {
		delete(s.casErr, p)
		return statestore.ObjectMeta{}, e
	}
	// Forced-conflict injection: pretend the rev moved.
	if n, ok := s.casConflicts[p]; ok && n > 0 {
		s.casConflicts[p] = n - 1
		// Bump the on-disk revision so the caller's re-read sees a
		// new oldRev, but DON'T mutate the body so an idempotent merge
		// keeps producing the same target body.
		if _, ok := s.objects[p]; ok {
			s.revisions[p] = s.nextRev()
		}
		return statestore.ObjectMeta{}, fmt.Errorf("%w: %s", statestore.ErrConflict, p)
	}
	cur, ok := s.objects[p]
	if !ok {
		return statestore.ObjectMeta{}, fmt.Errorf("%w: %s", statestore.ErrNotFound, p)
	}
	_ = cur
	if s.revisions[p] != oldRev {
		return statestore.ObjectMeta{}, fmt.Errorf("%w: %s", statestore.ErrConflict, p)
	}
	s.objects[p] = append([]byte(nil), data...)
	s.revisions[p] = s.nextRev()
	return statestore.ObjectMeta{Path: p, Size: int64(len(data)), Revision: s.revisions[p]}, nil
}

func (s *spyStore) List(ctx context.Context, prefix string) ([]statestore.ObjectInfo, error) {
	return nil, nil
}

func (s *spyStore) Delete(ctx context.Context, p string) error { return nil }

// ----- fixture builders ----------------------------------------------

const (
	testSrcKey = "src-branch-main-cabcdef-tabcdef0"
	testCatKey = "cat-deadbeef"
)

func makeSource() catalogmodel.SourceSnapshot {
	return catalogmodel.SourceSnapshot{
		APIVersion:        "orun.io/v1alpha1",
		Kind:              "SourceSnapshot",
		SourceSnapshotKey: testSrcKey,
		SourceSnapshotID:  "src_01H000000000000000000000",
		Repo:              "sourceplane/orun",
		Ref:               "refs/heads/main",
		Branch:            "main",
		SourceScope:       catalogmodel.SourceScopeBranchMain,
		HeadRevision:      "abcdef0",
		TreeHash:          "abcdef0",
		WorkingTree:       catalogmodel.WorkingTreeClean,
		CatalogInputHash:  "sha256:deadbeef",
		CreatedAt:         "2026-05-31T00:00:00Z",
	}
}

func makeCatalog() catalogmodel.CatalogSnapshot {
	return catalogmodel.CatalogSnapshot{
		APIVersion:         "orun.io/v1alpha1",
		Kind:               "CatalogSnapshot",
		CatalogSnapshotKey: testCatKey,
		CatalogSnapshotID:  "cat_01H000000000000000000000",
		SourceSnapshotKey:  testSrcKey,
		Repo:               "sourceplane/orun",
		SourceScope:        catalogmodel.SourceScopeBranchMain,
		HeadRevision:       "abcdef0",
		TreeHash:           "abcdef0",
		WorkingTree:        catalogmodel.WorkingTreeClean,
		Authoritative:      true,
		Resolver: catalogmodel.CatalogResolver{
			OrunVersion:     "0.18.0",
			SchemaVersion:   "orun.io/v1alpha1",
			ResolverVersion: 1,
			StackSources:    []string{},
		},
		CatalogHash: "sha256:deadbeef",
		CreatedAt:   "2026-05-31T00:00:00Z",
	}
}

func makeManifest(name string) catalogmodel.ComponentManifest {
	return catalogmodel.ComponentManifest{
		APIVersion: "orun.io/v1alpha1",
		Kind:       "ComponentManifest",
		Identity: catalogmodel.ComponentIdentity{
			ComponentID:  "cmp_01H000000000000000000000",
			ComponentKey: "sourceplane/orun/" + name,
			Name:         name,
			Namespace:    "sourceplane",
			Repo:         "orun",
			Path:         name,
			SourceFile:   name + "/component.yaml",
		},
		Source: catalogmodel.ComponentSource{
			SourceSnapshotKey:  testSrcKey,
			CatalogSnapshotKey: testCatKey,
			ManifestHash:       "sha256:cafe",
		},
	}
}

func makeGraph(kind string) *catalogmodel.CatalogGraph {
	return &catalogmodel.CatalogGraph{
		APIVersion:         "orun.io/v1alpha1",
		Kind:               "CatalogGraph",
		SourceSnapshotKey:  testSrcKey,
		CatalogSnapshotKey: testCatKey,
		Nodes:              []catalogmodel.GraphNode{{Key: "n", Kind: "Component", Name: kind}},
	}
}

func makeAllGraphs() catalogstore.CatalogGraphs {
	return catalogstore.CatalogGraphs{
		Dependencies: makeGraph("dependencies"),
		Systems:      makeGraph("systems"),
		APIs:         makeGraph("apis"),
		Resources:    makeGraph("resources"),
		Owners:       makeGraph("owners"),
	}
}

// ----- Step A tests ---------------------------------------------------

func TestWriteSourceSnapshot_HappyPath(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	if err := st.WriteSourceSnapshot(context.Background(), makeSource()); err != nil {
		t.Fatalf("WriteSourceSnapshot: %v", err)
	}
	wantPath := "sources/" + testSrcKey + "/source.json"
	if _, ok := spy.objects[wantPath]; !ok {
		t.Errorf("missing object at %s; got: %v", wantPath, keys(spy.objects))
	}
}

func TestWriteSourceSnapshot_IdempotentOnIdenticalRewrite(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	src := makeSource()
	if err := st.WriteSourceSnapshot(context.Background(), src); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := st.WriteSourceSnapshot(context.Background(), src); err != nil {
		t.Errorf("byte-identical re-write should be idempotent, got %v", err)
	}
}

func TestWriteSourceSnapshot_MismatchReturnsErrSourceMismatch(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	src := makeSource()
	if err := st.WriteSourceSnapshot(context.Background(), src); err != nil {
		t.Fatalf("first: %v", err)
	}
	divergent := src
	divergent.HeadRevision = "ffffeee" // different body
	err := st.WriteSourceSnapshot(context.Background(), divergent)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, catalogstore.ErrSourceMismatch) {
		t.Errorf("err not ErrSourceMismatch chain: %v", err)
	}
	if !errors.Is(err, statestore.ErrExists) {
		t.Errorf("err must preserve statestore.ErrExists: %v", err)
	}
}

func TestWriteSourceSnapshot_RejectsInvalidKey(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	src := makeSource()
	src.SourceSnapshotKey = "BAD"
	if err := st.WriteSourceSnapshot(context.Background(), src); err == nil {
		t.Fatalf("expected error for bad key")
	}
	if len(spy.trace) != 0 {
		t.Errorf("no writes should have been issued; trace=%v", spy.trace)
	}
}

// ----- Step B happy path & ordering ----------------------------------

func TestWriteCatalogSnapshot_HappyPath_CallOrder(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)

	manifests := []catalogmodel.ComponentManifest{makeManifest("aaa"), makeManifest("bbb")}
	graphs := makeAllGraphs()
	indexes := catalogstore.CatalogLocalIndexes{
		Components: map[string]any{"aaa": map[string]string{"k": "v"}},
		Owners:     map[string]any{"team-x": map[string]string{"k": "v"}},
	}

	if err := st.WriteCatalogSnapshot(context.Background(), makeSource(), makeCatalog(), manifests, graphs, indexes); err != nil {
		t.Fatalf("WriteCatalogSnapshot: %v", err)
	}

	// Assert phase ordering: B.1 manifests → B.2 graphs in fixed order →
	// B.3 catalog doc → B.4 local indexes.
	want := []string{
		"create:sources/" + testSrcKey + "/catalogs/" + testCatKey + "/components/aaa/manifest.json",
		"create:sources/" + testSrcKey + "/catalogs/" + testCatKey + "/components/bbb/manifest.json",
		"create:sources/" + testSrcKey + "/catalogs/" + testCatKey + "/graph/dependencies.json",
		"create:sources/" + testSrcKey + "/catalogs/" + testCatKey + "/graph/systems.json",
		"create:sources/" + testSrcKey + "/catalogs/" + testCatKey + "/graph/apis.json",
		"create:sources/" + testSrcKey + "/catalogs/" + testCatKey + "/graph/resources.json",
		"create:sources/" + testSrcKey + "/catalogs/" + testCatKey + "/graph/owners.json",
		"create:sources/" + testSrcKey + "/catalogs/" + testCatKey + "/catalog.json",
	}
	if len(spy.trace) < len(want) {
		t.Fatalf("trace too short: %v", spy.trace)
	}
	for i, w := range want {
		if spy.trace[i] != w {
			t.Errorf("trace[%d] = %q, want %q", i, spy.trace[i], w)
		}
	}
	// B.4: every remaining entry must be a "write:" op (local indexes).
	for _, op := range spy.trace[len(want):] {
		if op[:6] != "write:" {
			t.Errorf("post-doc trace entry must be a local-index Write, got %q", op)
		}
	}
}

func TestWriteCatalogSnapshot_GraphOrderIsFixed(t *testing.T) {
	// Provide graphs out of the canonical order to confirm the writer's
	// internal kind list — not the input "order" — drives the trace.
	spy := newSpyStore()
	st := catalogstore.New(spy)
	graphs := catalogstore.CatalogGraphs{
		Owners:       makeGraph("owners"),
		Resources:    makeGraph("resources"),
		APIs:         makeGraph("apis"),
		Systems:      makeGraph("systems"),
		Dependencies: makeGraph("dependencies"),
	}
	if err := st.WriteCatalogSnapshot(context.Background(), makeSource(), makeCatalog(), nil, graphs, catalogstore.CatalogLocalIndexes{}); err != nil {
		t.Fatalf("WriteCatalogSnapshot: %v", err)
	}
	// Filter just the graph creates from the trace.
	var graphTrace []string
	for _, e := range spy.trace {
		if len(e) >= 7 && e[:7] == "create:" && contains(e, "/graph/") {
			graphTrace = append(graphTrace, e)
		}
	}
	wantOrder := []string{"dependencies.json", "systems.json", "apis.json", "resources.json", "owners.json"}
	if len(graphTrace) != len(wantOrder) {
		t.Fatalf("graph traces: got %v", graphTrace)
	}
	for i, want := range wantOrder {
		if !endsWith(graphTrace[i], want) {
			t.Errorf("graph[%d]=%q, want suffix %q", i, graphTrace[i], want)
		}
	}
}

// ----- Step B mismatch & inconsistency -------------------------------

func TestWriteCatalogSnapshot_ManifestMismatchAbortsRest(t *testing.T) {
	spy := newSpyStore()
	manifest := makeManifest("aaa")
	manifestPath, err := catalogstore.ComponentManifestPath(testSrcKey, testCatKey, "aaa")
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	// Pre-existing body that differs from what we will encode.
	spy.preExisting[manifestPath] = []byte(`{"different":"body"}`)

	st := catalogstore.New(spy)
	err = st.WriteCatalogSnapshot(context.Background(), makeSource(), makeCatalog(),
		[]catalogmodel.ComponentManifest{manifest, makeManifest("bbb")},
		makeAllGraphs(), catalogstore.CatalogLocalIndexes{})
	if err == nil {
		t.Fatalf("expected ErrManifestMismatch")
	}
	if !errors.Is(err, catalogstore.ErrManifestMismatch) {
		t.Errorf("err not ErrManifestMismatch chain: %v", err)
	}
	if !errors.Is(err, statestore.ErrExists) {
		t.Errorf("err must preserve statestore.ErrExists: %v", err)
	}
	// No further writes should have been issued after the failing
	// manifest. The trace records the failing CreateIfAbsent + the
	// follow-up Read, but no additional creates/writes after that.
	for _, e := range spy.trace[1:] {
		if e[:6] == "write:" || (len(e) >= 7 && e[:7] == "create:") {
			t.Errorf("post-failure write observed: %q", e)
		}
	}
}

func TestWriteCatalogSnapshot_PreflightSourceKeyMismatch(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	src := makeSource()
	cat := makeCatalog()
	cat.SourceSnapshotKey = "src-branch-main-cffffff-tffffff0" // valid shape, wrong linkage
	err := st.WriteCatalogSnapshot(context.Background(), src, cat, nil, makeAllGraphs(), catalogstore.CatalogLocalIndexes{})
	if !errors.Is(err, catalogstore.ErrInputsInconsistent) {
		t.Fatalf("expected ErrInputsInconsistent, got %v", err)
	}
	if len(spy.trace) != 0 {
		t.Errorf("no writes should have been issued, got: %v", spy.trace)
	}
}

func TestWriteCatalogSnapshot_PreflightManifestSourceKeyMismatch(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	m := makeManifest("aaa")
	m.Source.SourceSnapshotKey = "src-branch-main-cffffff-tffffff0"
	err := st.WriteCatalogSnapshot(context.Background(), makeSource(), makeCatalog(),
		[]catalogmodel.ComponentManifest{m}, makeAllGraphs(), catalogstore.CatalogLocalIndexes{})
	if !errors.Is(err, catalogstore.ErrInputsInconsistent) {
		t.Fatalf("expected ErrInputsInconsistent, got %v", err)
	}
	if len(spy.trace) != 0 {
		t.Errorf("no writes; got %v", spy.trace)
	}
}

func TestWriteCatalogSnapshot_PreflightManifestCatalogKeyMismatch(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	m := makeManifest("aaa")
	m.Source.CatalogSnapshotKey = "cat-feedface"
	err := st.WriteCatalogSnapshot(context.Background(), makeSource(), makeCatalog(),
		[]catalogmodel.ComponentManifest{m}, makeAllGraphs(), catalogstore.CatalogLocalIndexes{})
	if !errors.Is(err, catalogstore.ErrInputsInconsistent) {
		t.Fatalf("expected ErrInputsInconsistent, got %v", err)
	}
	if len(spy.trace) != 0 {
		t.Errorf("no writes; got %v", spy.trace)
	}
}

func TestWriteCatalogSnapshot_IdempotentOnIdenticalRewrite(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	manifests := []catalogmodel.ComponentManifest{makeManifest("aaa")}
	if err := st.WriteCatalogSnapshot(context.Background(), makeSource(), makeCatalog(), manifests, makeAllGraphs(), catalogstore.CatalogLocalIndexes{}); err != nil {
		t.Fatalf("first: %v", err)
	}
	// Second call with identical inputs MUST succeed (every CreateIfAbsent
	// hits ErrExists with byte-identical body and is treated as success).
	if err := st.WriteCatalogSnapshot(context.Background(), makeSource(), makeCatalog(), manifests, makeAllGraphs(), catalogstore.CatalogLocalIndexes{}); err != nil {
		t.Errorf("idempotent re-write should succeed, got %v", err)
	}
}

func TestWriteCatalogSnapshot_CatalogDocMismatch(t *testing.T) {
	spy := newSpyStore()
	catalogPath, err := catalogstore.CatalogDocPath(testSrcKey, testCatKey)
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	spy.preExisting[catalogPath] = []byte(`{"different":"catalog"}`)
	st := catalogstore.New(spy)
	err = st.WriteCatalogSnapshot(context.Background(), makeSource(), makeCatalog(), nil, makeAllGraphs(), catalogstore.CatalogLocalIndexes{})
	if !errors.Is(err, catalogstore.ErrCatalogMismatch) {
		t.Fatalf("expected ErrCatalogMismatch, got %v", err)
	}
	if !errors.Is(err, statestore.ErrExists) {
		t.Errorf("must preserve statestore.ErrExists chain: %v", err)
	}
}

// ----- helpers --------------------------------------------------------

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestWriteCatalogSnapshot_AllLocalIndexAxesWritten(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	idx := catalogstore.CatalogLocalIndexes{
		Components: map[string]any{"a": map[string]string{"k": "v"}},
		Owners:     map[string]any{"o": map[string]string{"k": "v"}},
		Systems:    map[string]any{"s": map[string]string{"k": "v"}},
		Domains:    map[string]any{"d": map[string]string{"k": "v"}},
		Types:      map[string]any{"t": map[string]string{"k": "v"}},
	}
	if err := st.WriteCatalogSnapshot(context.Background(), makeSource(), makeCatalog(), nil, makeAllGraphs(), idx); err != nil {
		t.Fatalf("WriteCatalogSnapshot: %v", err)
	}
	wantSuffixes := []string{
		"/indexes/components/a.json",
		"/indexes/owners/o.json",
		"/indexes/systems/s.json",
		"/indexes/domains/d.json",
		"/indexes/types/t.json",
	}
	for _, suf := range wantSuffixes {
		found := false
		for k := range spy.objects {
			if endsWith(k, suf) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing local index with suffix %q; got %v", suf, keys(spy.objects))
		}
	}
}

func TestWriteSourceSnapshot_NonExistsCreateError(t *testing.T) {
	spy := newSpyStore()
	docPath, _ := catalogstore.SourceDocPath(testSrcKey)
	spy.failCreate = errors.New("disk full")
	spy.failCreatePath = docPath
	st := catalogstore.New(spy)
	err := st.WriteSourceSnapshot(context.Background(), makeSource())
	if err == nil {
		t.Fatalf("expected error")
	}
	if errors.Is(err, catalogstore.ErrSourceMismatch) {
		t.Errorf("non-ErrExists path must NOT classify as ErrSourceMismatch: %v", err)
	}
}

func TestWriteCatalogSnapshot_InvalidLocalIndexKeyReturnsError(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	idx := catalogstore.CatalogLocalIndexes{
		Components: map[string]any{"BAD": map[string]string{"k": "v"}},
	}
	err := st.WriteCatalogSnapshot(context.Background(), makeSource(), makeCatalog(), nil, makeAllGraphs(), idx)
	if err == nil {
		t.Fatalf("expected error from invalid local index key")
	}
	if !errors.Is(err, catalogstore.ErrInvalidPathInput) {
		t.Errorf("err not ErrInvalidPathInput chain: %v", err)
	}
}

func TestWriteCatalogSnapshot_GraphBodyMismatch(t *testing.T) {
	spy := newSpyStore()
	depPath, _ := catalogstore.CatalogGraphPath(testSrcKey, testCatKey, "dependencies")
	spy.preExisting[depPath] = []byte(`{"different":"graph"}`)
	st := catalogstore.New(spy)
	err := st.WriteCatalogSnapshot(context.Background(), makeSource(), makeCatalog(), nil, makeAllGraphs(), catalogstore.CatalogLocalIndexes{})
	if !errors.Is(err, catalogstore.ErrCatalogMismatch) {
		t.Fatalf("expected ErrCatalogMismatch (graph divergence), got %v", err)
	}
}

func TestWriteCatalogSnapshot_CatalogKeyValidationOnPreflight(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	cat := makeCatalog()
	cat.CatalogSnapshotKey = "BAD"
	err := st.WriteCatalogSnapshot(context.Background(), makeSource(), cat, nil, makeAllGraphs(), catalogstore.CatalogLocalIndexes{})
	if err == nil {
		t.Fatalf("expected error")
	}
	if len(spy.trace) != 0 {
		t.Errorf("no writes; got %v", spy.trace)
	}
}

func TestWriteCatalogSnapshot_SourceKeyValidationOnPreflight(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	src := makeSource()
	src.SourceSnapshotKey = "BAD"
	err := st.WriteCatalogSnapshot(context.Background(), src, makeCatalog(), nil, makeAllGraphs(), catalogstore.CatalogLocalIndexes{})
	if err == nil {
		t.Fatalf("expected error")
	}
	if len(spy.trace) != 0 {
		t.Errorf("no writes; got %v", spy.trace)
	}
}

func endsWith(s, suffix string) bool {
	if len(s) < len(suffix) {
		return false
	}
	return s[len(s)-len(suffix):] == suffix
}
