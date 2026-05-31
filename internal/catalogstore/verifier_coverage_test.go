// Verifier-attached coverage fixes for Task 0033 (PR #174).
//
// Task 0032 acceptance required ≥ 91 % coverage on internal/catalogstore
// with a hard floor of 90 %. The implementer landed at 85.3 % — below
// the PR-1 floor of 90.7 %. Per Task 0033 Outcome 11 (path "a"
// verifier-attached fix), this file adds focused tests on the
// previously-untested branches:
//
//   * WriteGlobalIndexes: ValidateSourceKey / ValidateCatalogKey reject
//     paths (caller supplies a malformed snapshot key on the
//     source/catalog global index update).
//   * WriteGlobalIndexes: byte-identical no-op merge short-circuit
//     (mergeComponentGlobalIndex produces the existing body, so the
//     bytes.Equal branch returns success without issuing a CAS).
//   * AppendComponentEvent: malformed source/catalog snapshot key
//     rejection paths (non-empty but failing ValidateSourceKey /
//     ValidateCatalogKey).
//   * AppendComponentEvent: seq.lock allocator surfacing on a corrupt
//     existing lock (next=0 envelope and unparseable JSON envelope).
//   * AppendComponentEvent: pre-existing body at the event path causes
//     the immutable-body CreateIfAbsent to surface as an error.
//
// All tests use the existing spyStore — no extensions to the spy are
// required. The package boundary (`package catalogstore_test`) is
// preserved.
package catalogstore_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogstore"
	"github.com/sourceplane/orun/internal/statestore"
)

// TestWriteGlobalIndexes_InvalidSourceKey — Source side has a
// non-empty but malformed SourceSnapshotKey; ValidateSourceKey rejects
// before any state mutation happens.
func TestWriteGlobalIndexes_InvalidSourceKey(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	src := makeSource()
	src.SourceSnapshotKey = "BAD KEY" // wrong shape, non-empty
	err := st.WriteGlobalIndexes(context.Background(), catalogstore.GlobalIndexUpdate{
		Source: &src,
	})
	if err == nil {
		t.Fatalf("expected error for malformed source key")
	}
	if len(spy.trace) != 0 {
		t.Errorf("no writes should have been issued; trace=%v", spy.trace)
	}
}

// TestWriteGlobalIndexes_InvalidCatalogKey — Catalog side malformed.
func TestWriteGlobalIndexes_InvalidCatalogKey(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	cat := makeCatalog()
	cat.CatalogSnapshotKey = "BAD KEY" // wrong shape, non-empty
	err := st.WriteGlobalIndexes(context.Background(), catalogstore.GlobalIndexUpdate{
		Catalog: &cat,
	})
	if err == nil {
		t.Fatalf("expected error for malformed catalog key")
	}
	if len(spy.trace) != 0 {
		t.Errorf("no writes should have been issued; trace=%v", spy.trace)
	}
}

// TestWriteGlobalIndexes_PreExistingIdenticalMergesAndConverges — when
// a pre-existing component-global-index body matches the caller's
// payload (same identity + Latest + Main), the merge path runs through
// the read-merge-CAS loop and converges on the first CAS attempt
// without exhausting the budget. This covers the "Object exists →
// merge → CAS succeeds" happy path of writeComponentGlobalIndex.
func TestWriteGlobalIndexes_PreExistingIdenticalMergesAndConverges(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)

	want := makeComponentGlobalIndex("aaa")
	p, _ := catalogstore.ComponentGlobalIndexPath(want.ComponentKey)

	// Pre-seed with the same encoded body. The CreateIfAbsent will hit
	// ErrExists; Read returns the body; merge produces an equivalent
	// object; CAS on the first attempt succeeds.
	body, err := catalogmodel.PrettyEncode(*want)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	spy.preExisting[p] = body

	if err := st.WriteGlobalIndexes(context.Background(), catalogstore.GlobalIndexUpdate{
		Components: []*catalogmodel.ComponentGlobalIndex{want},
	}); err != nil {
		t.Fatalf("merge with identical pre-existing should succeed, got %v", err)
	}
	// Verify at most ONE CAS attempt fired (no retry-loop exhaustion).
	casCount := 0
	for _, e := range spy.trace {
		if strings.HasPrefix(e, "cas:"+p+":") {
			casCount++
		}
	}
	if casCount > 1 {
		t.Errorf("expected ≤ 1 CAS attempt on identical-body path, got %d (trace=%v)", casCount, spy.trace)
	}
}

// TestAppendComponentEvent_InvalidSourceKey — non-empty but malformed
// SourceSnapshotKey rejected by ValidateSourceKey.
func TestAppendComponentEvent_InvalidSourceKey(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ev := makeEvent("aaa", catalogmodel.EventCatalogResolved)
	ev.SourceSnapshotKey = "BAD KEY"
	err := st.AppendComponentEvent(context.Background(), ev)
	if err == nil {
		t.Fatalf("expected error for malformed source key")
	}
	if len(spy.trace) != 0 {
		t.Errorf("no writes should have been issued; trace=%v", spy.trace)
	}
}

// TestAppendComponentEvent_InvalidCatalogKey — non-empty but malformed
// CatalogSnapshotKey rejected by ValidateCatalogKey.
func TestAppendComponentEvent_InvalidCatalogKey(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ev := makeEvent("aaa", catalogmodel.EventCatalogResolved)
	ev.CatalogSnapshotKey = "BAD KEY"
	err := st.AppendComponentEvent(context.Background(), ev)
	if err == nil {
		t.Fatalf("expected error for malformed catalog key")
	}
	if len(spy.trace) != 0 {
		t.Errorf("no writes should have been issued; trace=%v", spy.trace)
	}
}

// TestAppendComponentEvent_SeqLockZero — pre-existing seq.lock body has
// next=0, which the allocator must reject as corrupt rather than
// allocate seq=0.
func TestAppendComponentEvent_SeqLockZero(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	dir, _ := catalogstore.CatalogDir(testSrcKey, testCatKey)
	lockPath := dir + "/history/components/aaa/events/seq.lock"
	bad, _ := json.Marshal(map[string]uint64{"next": 0})
	spy.preExisting[lockPath] = bad

	err := st.AppendComponentEvent(context.Background(), makeEvent("aaa", catalogmodel.EventCatalogResolved))
	if err == nil {
		t.Fatalf("expected error for next=0 seq.lock")
	}
	if !strings.Contains(err.Error(), "next=0") && !strings.Contains(err.Error(), "invalid seq.lock") {
		t.Errorf("expected 'invalid seq.lock' / 'next=0' error, got %v", err)
	}
}

// TestAppendComponentEvent_SeqLockMalformedJSON — pre-existing seq.lock
// is unparseable; allocator surfaces the decode error rather than
// silently re-initialising.
func TestAppendComponentEvent_SeqLockMalformedJSON(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	dir, _ := catalogstore.CatalogDir(testSrcKey, testCatKey)
	lockPath := dir + "/history/components/aaa/events/seq.lock"
	spy.preExisting[lockPath] = []byte("{not valid json")

	err := st.AppendComponentEvent(context.Background(), makeEvent("aaa", catalogmodel.EventCatalogResolved))
	if err == nil {
		t.Fatalf("expected decode error")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("expected decode error, got %v", err)
	}
}

// TestAppendComponentEvent_BodyAlreadyExists — pre-populating the event
// body path with a divergent body causes the immutable CreateIfAbsent
// at the body path to surface ErrExists wrapped in
// AppendComponentEvent's error envelope. This covers the
// "concurrent writer beat us to <seq>+kind" branch.
func TestAppendComponentEvent_BodyAlreadyExists(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)

	// Pre-populate the body path that AppendComponentEvent will derive
	// for seq=1 + EventCatalogResolved.
	bodyPath, err := catalogstore.ComponentHistoryEventPath(
		testSrcKey, testCatKey, "aaa", 1, catalogmodel.EventCatalogResolved,
	)
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	spy.preExisting[bodyPath] = []byte(`{"already":"there"}`)

	err = st.AppendComponentEvent(context.Background(), makeEvent("aaa", catalogmodel.EventCatalogResolved))
	if err == nil {
		t.Fatalf("expected error from body-path collision")
	}
	if !errors.Is(err, statestore.ErrExists) {
		t.Errorf("expected statestore.ErrExists chain, got %v", err)
	}
}

// TestAppendComponentEvent_RejectsKeyWithoutSlashesBeforeAllocator
// (closes the file).
func TestAppendComponentEvent_RejectsKeyWithoutSlashesBeforeAllocator(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ev := makeEvent("aaa", catalogmodel.EventCatalogResolved)
	ev.ComponentKey = "noseparator" // 1-segment key, ValidateComponentKey rejects.
	err := st.AppendComponentEvent(context.Background(), ev)
	if err == nil {
		t.Fatalf("expected ErrInvalidPathInput")
	}
	if !errors.Is(err, catalogstore.ErrInvalidPathInput) {
		t.Errorf("expected ErrInvalidPathInput chain, got %v", err)
	}
	if len(spy.trace) != 0 {
		t.Errorf("no writes; got %v", spy.trace)
	}
}

// ----- error-injection paths (defensive non-Exists / non-Conflict) ---

// errCustom is a non-statestore error used to drive the defensive
// "neither ErrExists nor ErrConflict" branches.
var errCustom = errors.New("verifier-injected custom error")

// TestWriteRefs_CreateIfAbsentNonExistsErrorSurfaces — writeRefCAS's
// `if !errors.Is(err, statestore.ErrExists)` branch returns a wrapped
// error directly when CreateIfAbsent fails with anything other than
// ErrExists.
func TestWriteRefs_CreateIfAbsentNonExistsErrorSurfaces(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	src := makeSourceRef(catalogmodel.RefNameCurrent)
	currentPath := "refs/sources/current.json"
	spy.createNStdE[currentPath] = errCustom

	err := st.WriteRefs(context.Background(), catalogstore.RefUpdate{Source: &src})
	if err == nil {
		t.Fatalf("expected wrapped non-Exists error")
	}
	if !errors.Is(err, errCustom) {
		t.Errorf("expected errCustom chain, got %v", err)
	}
}

// TestWriteRefs_PostExistsReadErrorSurfaces — when CreateIfAbsent
// returns ErrExists but the follow-up Read fails, writeRefCAS wraps
// the read error.
func TestWriteRefs_PostExistsReadErrorSurfaces(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	src := makeSourceRef(catalogmodel.RefNameCurrent)
	currentPath := "refs/sources/current.json"
	spy.preExisting[currentPath] = []byte(`{"x":1}`)
	spy.readErr[currentPath] = errCustom

	err := st.WriteRefs(context.Background(), catalogstore.RefUpdate{Source: &src})
	if err == nil {
		t.Fatalf("expected post-Exists Read error")
	}
	if !errors.Is(err, errCustom) {
		t.Errorf("expected errCustom chain, got %v", err)
	}
}

// TestWriteRefs_CASNonConflictErrorSurfaces — once we're inside the CAS
// retry loop, a non-Conflict error from CompareAndSwap surfaces
// immediately (does NOT exhaust the budget).
func TestWriteRefs_CASNonConflictErrorSurfaces(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	src := makeSourceRef(catalogmodel.RefNameCurrent)
	currentPath := "refs/sources/current.json"
	spy.preExisting[currentPath] = []byte(`{"different":"body"}`)
	// First CAS attempt returns a non-Conflict error.
	spy.casErr[currentPath] = errCustom

	err := st.WriteRefs(context.Background(), catalogstore.RefUpdate{Source: &src})
	if err == nil {
		t.Fatalf("expected CAS non-Conflict error")
	}
	if !errors.Is(err, errCustom) {
		t.Errorf("expected errCustom chain, got %v", err)
	}
	if errors.Is(err, catalogstore.ErrRefStale) {
		t.Errorf("non-Conflict error must NOT be wrapped as ErrRefStale: %v", err)
	}
}

// (TestWriteRefs_ReReadAfterConflictErrorSurfaces — not added: the
// re-Read-after-conflict branch in writeRefCAS is symmetric to the
// post-Exists Read branch covered by
// TestWriteRefs_PostExistsReadErrorSurfaces. Exercising it from a
// black-box test would require timing hooks on the spy. Branch left
// unexercised; risk noted in the verifier report.)

// TestWriteGlobalIndexes_ComponentCreateIfAbsentNonExistsErrorSurfaces.
func TestWriteGlobalIndexes_ComponentCreateIfAbsentNonExistsErrorSurfaces(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	want := makeComponentGlobalIndex("aaa")
	p, _ := catalogstore.ComponentGlobalIndexPath(want.ComponentKey)
	spy.createNStdE[p] = errCustom

	err := st.WriteGlobalIndexes(context.Background(), catalogstore.GlobalIndexUpdate{
		Components: []*catalogmodel.ComponentGlobalIndex{want},
	})
	if err == nil {
		t.Fatalf("expected wrapped non-Exists error")
	}
	if !errors.Is(err, errCustom) {
		t.Errorf("expected errCustom chain, got %v", err)
	}
}

// TestWriteGlobalIndexes_ComponentReadErrorSurfaces — pre-existing body,
// but the Read inside the CAS loop fails immediately.
func TestWriteGlobalIndexes_ComponentReadErrorSurfaces(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	want := makeComponentGlobalIndex("aaa")
	p, _ := catalogstore.ComponentGlobalIndexPath(want.ComponentKey)
	spy.preExisting[p] = []byte(`{"divergent":"body"}`)
	spy.readErr[p] = errCustom

	err := st.WriteGlobalIndexes(context.Background(), catalogstore.GlobalIndexUpdate{
		Components: []*catalogmodel.ComponentGlobalIndex{want},
	})
	if err == nil {
		t.Fatalf("expected Read error")
	}
	if !errors.Is(err, errCustom) {
		t.Errorf("expected errCustom chain, got %v", err)
	}
}

// TestWriteGlobalIndexes_ComponentDecodeErrorSurfaces — pre-existing
// body is unparseable JSON.
func TestWriteGlobalIndexes_ComponentDecodeErrorSurfaces(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	want := makeComponentGlobalIndex("aaa")
	p, _ := catalogstore.ComponentGlobalIndexPath(want.ComponentKey)
	spy.preExisting[p] = []byte("{not valid json")

	err := st.WriteGlobalIndexes(context.Background(), catalogstore.GlobalIndexUpdate{
		Components: []*catalogmodel.ComponentGlobalIndex{want},
	})
	if err == nil {
		t.Fatalf("expected decode error")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("expected decode error, got %v", err)
	}
}

// TestWriteGlobalIndexes_ComponentCASNonConflictSurfaces — CAS returns a
// non-Conflict error (e.g. transport failure mid-loop).
func TestWriteGlobalIndexes_ComponentCASNonConflictSurfaces(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	want := makeComponentGlobalIndex("aaa")
	want.Latest = catalogmodel.ComponentIndexLocation{
		SourceSnapshotKey:  "src-pr-99-coldxx-toldxx0",
		CatalogSnapshotKey: "cat-oldxx",
	}
	p, _ := catalogstore.ComponentGlobalIndexPath(want.ComponentKey)
	pre := *makeComponentGlobalIndex("aaa")
	body, _ := catalogmodel.PrettyEncode(pre)
	spy.preExisting[p] = body
	spy.casErr[p] = errCustom

	err := st.WriteGlobalIndexes(context.Background(), catalogstore.GlobalIndexUpdate{
		Components: []*catalogmodel.ComponentGlobalIndex{want},
	})
	if err == nil {
		t.Fatalf("expected CAS non-Conflict error")
	}
	if !errors.Is(err, errCustom) {
		t.Errorf("expected errCustom chain, got %v", err)
	}
	if errors.Is(err, catalogstore.ErrRefStale) {
		t.Errorf("non-Conflict error must NOT be ErrRefStale: %v", err)
	}
}

// TestAppendComponentEvent_AllocatorCreateNonExistsErrorSurfaces —
// initial CreateIfAbsent on seq.lock fails with a non-Exists error.
func TestAppendComponentEvent_AllocatorCreateNonExistsErrorSurfaces(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	dir, _ := catalogstore.CatalogDir(testSrcKey, testCatKey)
	lockPath := dir + "/history/components/aaa/events/seq.lock"
	spy.createNStdE[lockPath] = errCustom

	err := st.AppendComponentEvent(context.Background(), makeEvent("aaa", catalogmodel.EventCatalogResolved))
	if err == nil {
		t.Fatalf("expected wrapped error")
	}
	if !errors.Is(err, errCustom) {
		t.Errorf("expected errCustom chain, got %v", err)
	}
}

// TestAppendComponentEvent_AllocatorCASNonConflictSurfaces — seq.lock
// pre-exists, first CAS returns a non-Conflict error.
func TestAppendComponentEvent_AllocatorCASNonConflictSurfaces(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	dir, _ := catalogstore.CatalogDir(testSrcKey, testCatKey)
	lockPath := dir + "/history/components/aaa/events/seq.lock"
	preBody, _ := json.Marshal(map[string]uint64{"next": 5})
	spy.preExisting[lockPath] = preBody
	spy.casErr[lockPath] = errCustom

	err := st.AppendComponentEvent(context.Background(), makeEvent("aaa", catalogmodel.EventCatalogResolved))
	if err == nil {
		t.Fatalf("expected CAS non-Conflict error")
	}
	if !errors.Is(err, errCustom) {
		t.Errorf("expected errCustom chain, got %v", err)
	}
}

// TestAppendComponentEvent_AllocatorReadErrorSurfaces — seq.lock
// pre-exists but Read fails immediately inside the CAS loop.
func TestAppendComponentEvent_AllocatorReadErrorSurfaces(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	dir, _ := catalogstore.CatalogDir(testSrcKey, testCatKey)
	lockPath := dir + "/history/components/aaa/events/seq.lock"
	preBody, _ := json.Marshal(map[string]uint64{"next": 5})
	spy.preExisting[lockPath] = preBody
	spy.readErr[lockPath] = errCustom

	err := st.AppendComponentEvent(context.Background(), makeEvent("aaa", catalogmodel.EventCatalogResolved))
	if err == nil {
		t.Fatalf("expected Read error")
	}
	if !errors.Is(err, errCustom) {
		t.Errorf("expected errCustom chain, got %v", err)
	}
}

// ----- WriteRefs scope-arm coverage ------------------------------------

// makeCatalogRefAuthoritative — small helper that builds a catalog ref
// with Authoritative=true for the D.3 main arm.
func makeCatalogRefAuthoritative() catalogmodel.CatalogRef {
	r := makeCatalogRef(catalogmodel.RefNameCurrent)
	r.Authoritative = true
	return r
}

func makeSourceRefAuthoritative() catalogmodel.SourceRef {
	r := makeSourceRef(catalogmodel.RefNameCurrent)
	r.Authoritative = true
	return r
}

// TestWriteRefs_CatalogOnly_NilSourceShortCircuits — addSource closure
// with refs.Source==nil short-circuits at every D.* arm. This covers
// the "if refs.Source == nil { return nil }" early-return inside the
// closure, which was previously unreached because every existing test
// passes refs.Source != nil.
func TestWriteRefs_CatalogOnly_NilSourceShortCircuits(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	cat := makeCatalogRef(catalogmodel.RefNameCurrent)
	cat.Authoritative = false
	if err := st.WriteRefs(context.Background(), catalogstore.RefUpdate{Catalog: &cat}); err != nil {
		t.Fatalf("catalog-only WriteRefs: %v", err)
	}
	// Two writes: catalogs/current.json + catalogs/latest.json.
	creates := 0
	for _, t := range spy.trace {
		if strings.HasPrefix(t, "create:") {
			creates++
		}
	}
	if creates != 2 {
		t.Errorf("expected 2 catalog ref creates, got %d (trace=%v)", creates, spy.trace)
	}
	// And NO source-side writes.
	for _, tr := range spy.trace {
		if strings.Contains(tr, "refs/sources/") {
			t.Errorf("unexpected source-side write %q", tr)
		}
	}
}

// TestWriteRefs_AuthoritativeWritesMain — D.3 arm: when the source ref
// (or catalog ref) is Authoritative, an extra write to refs/.../main.json
// is appended.
func TestWriteRefs_AuthoritativeWritesMain(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	src := makeSourceRefAuthoritative()
	cat := makeCatalogRefAuthoritative()

	if err := st.WriteRefs(context.Background(), catalogstore.RefUpdate{Source: &src, Catalog: &cat}); err != nil {
		t.Fatalf("WriteRefs: %v", err)
	}
	// Should include both …/sources/main.json and …/catalogs/main.json.
	wantPaths := []string{"refs/sources/main.json", "refs/catalogs/main.json"}
	for _, p := range wantPaths {
		found := false
		for _, tr := range spy.trace {
			if strings.HasSuffix(tr, p) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected write to %s, trace=%v", p, spy.trace)
		}
	}
}

// TestWriteRefs_BranchScopeArm — D.4 arm: refs.Branch sanitizes and
// emits …/sources/branches/<seg>.json + …/catalogs/branches/<seg>.json.
func TestWriteRefs_BranchScopeArm(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	src := makeSourceRef(catalogmodel.RefNameCurrent)
	cat := makeCatalogRef(catalogmodel.RefNameCurrent)

	if err := st.WriteRefs(context.Background(), catalogstore.RefUpdate{
		Source: &src, Catalog: &cat, Branch: "feature/foo",
	}); err != nil {
		t.Fatalf("WriteRefs: %v", err)
	}
	// One write to each branch ref.
	for _, want := range []string{"refs/sources/branches/", "refs/catalogs/branches/"} {
		found := false
		for _, tr := range spy.trace {
			if strings.Contains(tr, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected branch write under %s, trace=%v", want, spy.trace)
		}
	}
}

// TestWriteRefs_BranchSanitizedToEmptyRejected — D.4 sanitization guard.
func TestWriteRefs_BranchSanitizedToEmptyRejected(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	src := makeSourceRef(catalogmodel.RefNameCurrent)
	err := st.WriteRefs(context.Background(), catalogstore.RefUpdate{
		Source: &src, Branch: "///",
	})
	if err == nil {
		t.Fatalf("expected ErrInvalidPathInput for branch-sanitized-to-empty")
	}
	if !errors.Is(err, catalogstore.ErrInvalidPathInput) {
		t.Errorf("expected ErrInvalidPathInput chain, got %v", err)
	}
}

// TestWriteRefs_PRScopeArm — D.5 arm: refs.PR emits PR ref writes.
func TestWriteRefs_PRScopeArm(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	src := makeSourceRef(catalogmodel.RefNameCurrent)
	cat := makeCatalogRef(catalogmodel.RefNameCurrent)

	if err := st.WriteRefs(context.Background(), catalogstore.RefUpdate{
		Source: &src, Catalog: &cat, PR: "42",
	}); err != nil {
		t.Fatalf("WriteRefs: %v", err)
	}
	for _, want := range []string{"refs/sources/prs/", "refs/catalogs/prs/"} {
		found := false
		for _, tr := range spy.trace {
			if strings.Contains(tr, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected PR write under %s, trace=%v", want, spy.trace)
		}
	}
}

// ----- writeLocalIndexes Write-error path ----------------------------

// TestWriteCatalogSnapshot_WriteLocalIndexErrorSurfaces — exercise the
// `s.state.Write(...)` error branch in writeLocalIndexes. Inject
// writeErr on one local-index path.
func TestWriteCatalogSnapshot_WriteLocalIndexErrorSurfaces(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	src := makeSource()
	cat := makeCatalog()

	idx := catalogstore.CatalogLocalIndexes{
		Components: map[string]any{
			"aaa": map[string]any{"k": "v"},
		},
	}
	// Compute the path so we can target writeErr at it.
	p, _ := catalogstore.ComponentLocalIndexPath(testSrcKey, testCatKey, "aaa")
	spy.writeErr[p] = errCustom

	err := st.WriteCatalogSnapshot(context.Background(), src, cat, nil, catalogstore.CatalogGraphs{}, idx)
	if err == nil {
		t.Fatalf("expected Write error")
	}
	if !errors.Is(err, errCustom) {
		t.Errorf("expected errCustom chain, got %v", err)
	}
}

// ----- WriteGlobalIndexes Source/Catalog Write-error paths -------------

// TestWriteGlobalIndexes_SourceWriteErrorSurfaces — C.1 Write fails.
func TestWriteGlobalIndexes_SourceWriteErrorSurfaces(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	src := makeSource()
	p, _ := catalogstore.SourceGlobalIndexPath(testSrcKey)
	spy.writeErr[p] = errCustom

	err := st.WriteGlobalIndexes(context.Background(), catalogstore.GlobalIndexUpdate{Source: &src})
	if err == nil {
		t.Fatalf("expected Write error")
	}
	if !errors.Is(err, errCustom) {
		t.Errorf("expected errCustom chain, got %v", err)
	}
}

// TestWriteGlobalIndexes_CatalogWriteErrorSurfaces — C.2 Write fails.
func TestWriteGlobalIndexes_CatalogWriteErrorSurfaces(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	cat := makeCatalog()
	p, _ := catalogstore.CatalogGlobalIndexPath(testCatKey)
	spy.writeErr[p] = errCustom

	err := st.WriteGlobalIndexes(context.Background(), catalogstore.GlobalIndexUpdate{Catalog: &cat})
	if err == nil {
		t.Fatalf("expected Write error")
	}
	if !errors.Is(err, errCustom) {
		t.Errorf("expected errCustom chain, got %v", err)
	}
}

// ----- AppendComponentEvent body CreateIfAbsent error ----------------

// TestAppendComponentEvent_BodyCreateIfAbsentErrorSurfaces — after the
// allocator returns a seq, the per-event CreateIfAbsent at the body
// path fails. The wrapper preserves the underlying error.
func TestAppendComponentEvent_BodyCreateIfAbsentErrorSurfaces(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ev := makeEvent("aaa", catalogmodel.EventCatalogResolved)
	bodyPath, _ := catalogstore.ComponentHistoryEventPath(ev.SourceSnapshotKey, ev.CatalogSnapshotKey, "aaa", 1, ev.EventType)
	spy.createNStdE[bodyPath] = errCustom

	err := st.AppendComponentEvent(context.Background(), ev)
	if err == nil {
		t.Fatalf("expected body CreateIfAbsent error")
	}
	if !errors.Is(err, errCustom) {
		t.Errorf("expected errCustom chain, got %v", err)
	}
}

// ----- allocateEventSeq: corrupt seq.lock branches -------------------

// TestAppendComponentEvent_SeqLockUnparseableJSON — existing seq.lock
// body cannot be JSON-decoded.
func TestAppendComponentEvent_SeqLockUnparseableJSON(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	dir, _ := catalogstore.CatalogDir(testSrcKey, testCatKey)
	lockPath := dir + "/history/components/aaa/events/seq.lock"
	spy.preExisting[lockPath] = []byte("{not valid json")

	err := st.AppendComponentEvent(context.Background(), makeEvent("aaa", catalogmodel.EventCatalogResolved))
	if err == nil {
		t.Fatalf("expected JSON decode error")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("expected decode error, got %v", err)
	}
}

// TestWriteCatalogSnapshot_InvalidManifestNameSurfaces — a manifest
// whose Identity.Name fails ValidateComponentName triggers the
// ComponentManifestPath error branch (writer.go:84-86).
func TestWriteCatalogSnapshot_InvalidManifestNameSurfaces(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	src := makeSource()
	cat := makeCatalog()
	bad := makeManifest("aaa")
	bad.Identity.Name = "BAD!!!" // invalid component name segment.
	mfs := []catalogmodel.ComponentManifest{bad}

	err := st.WriteCatalogSnapshot(context.Background(), src, cat, mfs, catalogstore.CatalogGraphs{}, catalogstore.CatalogLocalIndexes{})
	if err == nil {
		t.Fatalf("expected manifest path validation error")
	}
	if !errors.Is(err, catalogstore.ErrInvalidPathInput) {
		t.Errorf("expected ErrInvalidPathInput chain, got %v", err)
	}
}
// TestWriteCatalogSnapshot_ManifestCreateNonExistsErrorSurfaces — the
// manifest CreateIfAbsent at step B.1 fails with a non-Exists error,
// surfacing through createOrReconcile (writer.go:208).
func TestWriteCatalogSnapshot_ManifestCreateNonExistsErrorSurfaces(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	src := makeSource()
	cat := makeCatalog()
	mfs := []catalogmodel.ComponentManifest{makeManifest("aaa")}
	mp, _ := catalogstore.ComponentManifestPath(testSrcKey, testCatKey, "aaa")
	spy.createNStdE[mp] = errCustom

	err := st.WriteCatalogSnapshot(context.Background(), src, cat, mfs, catalogstore.CatalogGraphs{}, catalogstore.CatalogLocalIndexes{})
	if err == nil {
		t.Fatalf("expected manifest CreateIfAbsent error")
	}
	if !errors.Is(err, errCustom) {
		t.Errorf("expected errCustom chain, got %v", err)
	}
}

// TestWriteCatalogSnapshot_ManifestPostExistsReadErrorSurfaces — manifest
// pre-exists, ErrExists fires, follow-up Read fails (writer.go:198-200).
func TestWriteCatalogSnapshot_ManifestPostExistsReadErrorSurfaces(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	src := makeSource()
	cat := makeCatalog()
	mfs := []catalogmodel.ComponentManifest{makeManifest("aaa")}
	mp, _ := catalogstore.ComponentManifestPath(testSrcKey, testCatKey, "aaa")
	spy.preExisting[mp] = []byte(`{"divergent":"body"}`)
	spy.readErr[mp] = errCustom

	err := st.WriteCatalogSnapshot(context.Background(), src, cat, mfs, catalogstore.CatalogGraphs{}, catalogstore.CatalogLocalIndexes{})
	if err == nil {
		t.Fatalf("expected post-Exists Read error")
	}
	// readErr is included via %v (not %w) but the underlying ErrExists
	// is wrapped as %w; verify both surface in the message and chain.
	if !strings.Contains(err.Error(), "verifier-injected custom error") {
		t.Errorf("expected readErr in message, got %v", err)
	}
	if !errors.Is(err, statestore.ErrExists) {
		t.Errorf("expected statestore.ErrExists chain, got %v", err)
	}
}
// (writer.go:44-49) Read failure after ErrExists.
func TestWriteSourceSnapshot_PostExistsReadErrorSurfaces(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	src := makeSource()
	docPath, _ := catalogstore.SourceDocPath(testSrcKey)
	spy.preExisting[docPath] = []byte(`{"divergent":"body"}`)
	spy.readErr[docPath] = errCustom

	err := st.WriteSourceSnapshot(context.Background(), src)
	if err == nil {
		t.Fatalf("expected post-Exists Read error")
	}
	if !strings.Contains(err.Error(), "verifier-injected custom error") {
		t.Errorf("expected readErr in message, got %v", err)
	}
	if !errors.Is(err, statestore.ErrExists) {
		t.Errorf("expected statestore.ErrExists chain, got %v", err)
	}
}

// TestAppendComponentEvent_SeqLockNextZeroRejected — corrupt envelope.
func TestAppendComponentEvent_SeqLockNextZeroRejected(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	dir, _ := catalogstore.CatalogDir(testSrcKey, testCatKey)
	lockPath := dir + "/history/components/aaa/events/seq.lock"
	body, _ := json.Marshal(map[string]uint64{"next": 0})
	spy.preExisting[lockPath] = body

	err := st.AppendComponentEvent(context.Background(), makeEvent("aaa", catalogmodel.EventCatalogResolved))
	if err == nil {
		t.Fatalf("expected next=0 rejection")
	}
	if !strings.Contains(err.Error(), "next=0") {
		t.Errorf("expected next=0 error, got %v", err)
	}
}
