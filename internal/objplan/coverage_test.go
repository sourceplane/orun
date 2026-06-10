package objplan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/clock"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/nodewriter"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

func TestResolveMemoPutMkdirFailure(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Block <root>/cache so MkdirAll(<root>/cache/resolve) fails.
	if err := os.WriteFile(filepath.Join(root, "cache"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	m := NewResolveMemo(root)
	if err := m.Put(id("a"), 1, id("b")); err == nil {
		t.Fatalf("Put should fail when cache dir is blocked")
	}
}

func TestRefsRemainingCases(t *testing.T) {
	t.Parallel()
	if got := SourceRefs(nodes.SourceSnapshot{Scope: nodes.ScopePR, PR: "7"}); got[1] != "sources/prs/7" {
		t.Fatalf("source pr refs = %v", got)
	}
	if got := CatalogRefs(nodes.SourceSnapshot{Scope: nodes.ScopeBranch, Branch: "dev"}); got[1] != "catalogs/branches/dev" {
		t.Fatalf("catalog branch refs = %v", got)
	}
	// branch/pr without identifiers collapse to the current pointer only.
	if got := CatalogRefs(nodes.SourceSnapshot{Scope: nodes.ScopePR}); len(got) != 1 {
		t.Fatalf("pr-no-number catalog refs = %v", got)
	}
}

// failBlobStore makes WriteSource fail to exercise Plan's error wrapping.
type failBlobStore struct{ *objectstore.MemStore }

func (f failBlobStore) PutBlob(context.Context, []byte) (objectstore.ObjectID, error) {
	return "", errBoom
}

func TestPlanSourceWriteError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store := failBlobStore{objectstore.NewMemStore("")}
	refs, _ := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Clock: clock.Fixed{}})
	w := nodewriter.New(store, refs, nodewriter.WithClock(clock.Fixed{}))
	_, err := Plan(context.Background(), w, store, NewResolveMemo(root),
		sampleInput(func() (*catalogresolve.CatalogView, error) { return sampleView(), nil }), Options{})
	if !errors.Is(err, errBoom) {
		t.Fatalf("Plan source error = %v, want errBoom", err)
	}
}

func TestPlanDefaultsScopeMode(t *testing.T) {
	t.Parallel()
	w, _, memo, _ := rig(t)
	in := sampleInput(nil)
	in.RevisionScope = nodes.RevisionScope{} // empty mode -> defaulted to "full"
	res, err := Plan(context.Background(), w, w.Store(), memo, in, Options{NoCatalog: true})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if res.RevisionID == "" {
		t.Fatalf("revision not written with defaulted scope")
	}
}

// TestResolveMemoInputsDigestKeysSeparately is the staleness regression: the
// same (sourceId, resolverVersion) with a different workspace-inputs digest
// (CODEOWNERS / composition lock changed) must MISS the memo — never serve the
// catalog resolved under the old inputs.
func TestResolveMemoInputsDigestKeysSeparately(t *testing.T) {
	t.Parallel()
	m := NewResolveMemo(t.TempDir())
	src, cat := id("a"), id("b")

	a := m.WithInputsDigest("aaaa1111")
	if err := a.Put(src, 9, cat); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if got, ok := a.Get(src, 9); !ok || got != cat {
		t.Fatalf("same-digest Get = %v,%v", got, ok)
	}
	// A different digest (the lock changed) misses.
	if _, ok := m.WithInputsDigest("bbbb2222").Get(src, 9); ok {
		t.Fatal("changed inputs digest must miss the memo")
	}
	// No digest (no CODEOWNERS/lock) is its own key space.
	if _, ok := m.Get(src, 9); ok {
		t.Fatal("empty-digest key must not see the digested entry")
	}
	// nil-memo WithInputsDigest is a safe no-op.
	var nilMemo *ResolveMemo
	if nilMemo.WithInputsDigest("x") != nil {
		t.Fatal("nil memo should stay nil")
	}
}
