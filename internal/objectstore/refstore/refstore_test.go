package refstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/clock"
)

func id(n int) string { return fmt.Sprintf("sha256:%064x", n) }

func newStore(t *testing.T) *LocalRefStore {
	t.Helper()
	rs, err := NewLocalRefStore(LocalConfig{
		Root:   t.TempDir(),
		Writer: "cli",
		Clock:  clock.Fixed{T: time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("NewLocalRefStore: %v", err)
	}
	return rs
}

func TestNewLocalRefStoreDefaultsAndErrors(t *testing.T) {
	t.Parallel()
	if _, err := NewLocalRefStore(LocalConfig{Root: ""}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty root = %v, want ErrInvalid", err)
	}
	rs, err := NewLocalRefStore(LocalConfig{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("defaults: %v", err)
	}
	if rs.writer != "cli" || rs.clk == nil {
		t.Fatalf("defaults not applied: writer=%q clk=%v", rs.writer, rs.clk)
	}
}

func TestReadAbsentAndInvalidName(t *testing.T) {
	t.Parallel()
	rs := newStore(t)
	ctx := context.Background()
	if _, err := rs.Read(ctx, "refs/none"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Read(absent) = %v, want ErrNotFound", err)
	}
	if _, err := rs.Read(ctx, "/bad"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Read(bad) = %v, want ErrInvalid", err)
	}
}

func TestUpdateCreateAndReadBack(t *testing.T) {
	t.Parallel()
	rs := newStore(t)
	ctx := context.Background()
	if err := rs.Update(ctx, "refs/executions/latest", "", id(1)); err != nil {
		t.Fatalf("Update create: %v", err)
	}
	ref, err := rs.Read(ctx, "refs/executions/latest")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if ref.Kind != "Ref" || ref.Target != id(1) || ref.Writer != "cli" {
		t.Fatalf("ref = %+v", ref)
	}
	if !ref.UpdatedAt.Equal(time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("UpdatedAt = %v", ref.UpdatedAt)
	}
}

func TestUpdateCASChainAndConflict(t *testing.T) {
	t.Parallel()
	rs := newStore(t)
	ctx := context.Background()
	if err := rs.Update(ctx, "k", "", id(1)); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := rs.Update(ctx, "k", id(1), id(2)); err != nil {
		t.Fatalf("cas advance: %v", err)
	}
	// Stale oldTarget loses.
	if err := rs.Update(ctx, "k", id(1), id(3)); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale cas = %v, want ErrConflict", err)
	}
	// Creating over an existing ref (expect-absent) loses.
	if err := rs.Update(ctx, "k", "", id(4)); !errors.Is(err, ErrConflict) {
		t.Fatalf("create-over-existing = %v, want ErrConflict", err)
	}
}

func TestUpdateValidation(t *testing.T) {
	t.Parallel()
	rs := newStore(t)
	ctx := context.Background()
	if err := rs.Update(ctx, "bad/", "", id(1)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad name = %v, want ErrInvalid", err)
	}
	if err := rs.Update(ctx, "k", "", "not-an-id"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad target = %v, want ErrInvalid", err)
	}
}

func TestListAndDelete(t *testing.T) {
	t.Parallel()
	rs := newStore(t)
	ctx := context.Background()
	for _, n := range []string{"a/b", "a/c", "d"} {
		if err := rs.Update(ctx, n, "", id(1)); err != nil {
			t.Fatalf("seed %s: %v", n, err)
		}
	}
	all, err := rs.List(ctx, "")
	if err != nil {
		t.Fatalf("List(all): %v", err)
	}
	if len(all) != 3 || all[0] != "a/b" || all[1] != "a/c" || all[2] != "d" {
		t.Fatalf("List(all) = %v", all)
	}
	under, err := rs.List(ctx, "a/")
	if err != nil {
		t.Fatalf("List(a/): %v", err)
	}
	if len(under) != 2 {
		t.Fatalf("List(a/) = %v", under)
	}
	if err := rs.Delete(ctx, "d"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := rs.Read(ctx, "d"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Read after delete = %v, want ErrNotFound", err)
	}
	if err := rs.Delete(ctx, "d"); err != nil {
		t.Fatalf("Delete(absent): %v", err)
	}
	if err := rs.Delete(ctx, "/bad"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Delete(bad) = %v, want ErrInvalid", err)
	}
}

func TestConcurrentCASExactlyOneWinner(t *testing.T) {
	t.Parallel()
	rs := newStore(t)
	ctx := context.Background()
	const n = 50
	var success int64
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := rs.Update(ctx, "race", "", id(i+1))
			if err == nil {
				atomic.AddInt64(&success, 1)
			} else if !errors.Is(err, ErrConflict) {
				t.Errorf("unexpected Update error: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if success != 1 {
		t.Fatalf("expected exactly 1 CAS winner, got %d", success)
	}
}

func TestValidRefName(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"a", "refs/x", "a/b/c", "rev-1.json-ish_x"} {
		if !validRefName(ok) {
			t.Fatalf("validRefName(%q) = false", ok)
		}
	}
	for _, bad := range []string{"", "/x", "x/", "a//b", "a/../b", "a/.", "a b", "a\x00"} {
		if validRefName(bad) {
			t.Fatalf("validRefName(%q) = true", bad)
		}
	}
}

func TestReadDecodeError(t *testing.T) {
	t.Parallel()
	rs := newStore(t)
	ctx := context.Background()
	path := rs.refPath("broken")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := rs.Read(ctx, "broken"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Read(garbage) = %v, want ErrInvalid", err)
	}
}

func TestWriteAtomicMkdirFailure(t *testing.T) {
	t.Parallel()
	rs := newStore(t)
	ctx := context.Background()
	// Block the parent dir for "blocked/x" by placing a file at refs/blocked.
	if err := os.WriteFile(filepath.Join(rs.root, "refs", "blocked"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed blocker: %v", err)
	}
	if err := rs.Update(ctx, "blocked/x", "", id(1)); err == nil {
		t.Fatalf("Update succeeded despite blocked parent dir")
	}
}
