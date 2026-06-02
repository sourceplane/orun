package objectstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newLocal(t *testing.T) *LocalStore {
	t.Helper()
	ls, err := NewLocalStore(LocalConfig{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	return ls
}

// eachStore runs fn against both drivers so the conformance suite covers them
// identically.
func eachStore(t *testing.T, fn func(t *testing.T, s ObjectStore)) {
	t.Helper()
	t.Run("local", func(t *testing.T) { t.Parallel(); fn(t, newLocal(t)) })
	t.Run("mem", func(t *testing.T) { t.Parallel(); fn(t, NewMemStore("")) })
}

func TestPutBlobGetIdempotent(t *testing.T) {
	eachStore(t, func(t *testing.T, s ObjectStore) {
		ctx := context.Background()
		id1, err := s.PutBlob(ctx, []byte("alpha"))
		if err != nil {
			t.Fatalf("PutBlob: %v", err)
		}
		id2, err := s.PutBlob(ctx, []byte("alpha"))
		if err != nil {
			t.Fatalf("PutBlob (repeat): %v", err)
		}
		if id1 != id2 {
			t.Fatalf("idempotency broken: %s != %s", id1, id2)
		}
		kind, body, err := s.Get(ctx, id1)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if kind != KindBlob || string(body) != "alpha" {
			t.Fatalf("Get returned kind=%s body=%q", kind, body)
		}
		has, err := s.Has(ctx, id1)
		if err != nil || !has {
			t.Fatalf("Has = %v,%v", has, err)
		}
	})
}

func TestGetNotFoundAndInvalid(t *testing.T) {
	eachStore(t, func(t *testing.T, s ObjectStore) {
		ctx := context.Background()
		absent := ObjectID("sha256:" + strings.Repeat("0", 64))
		if _, _, err := s.Get(ctx, absent); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get(absent) = %v, want ErrNotFound", err)
		}
		if _, err := s.Has(ctx, "garbage"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("Has(garbage) = %v, want ErrInvalid", err)
		}
		if _, _, err := s.Get(ctx, "garbage"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("Get(garbage) = %v, want ErrInvalid", err)
		}
	})
}

func TestTreePutGetAndDedup(t *testing.T) {
	eachStore(t, func(t *testing.T, s ObjectStore) {
		ctx := context.Background()
		bx, _ := s.PutBlob(ctx, []byte("x"))
		by, _ := s.PutBlob(ctx, []byte("y"))
		sub, err := s.PutTree(ctx, []TreeEntry{{Name: "leaf", Kind: KindBlob, ID: bx}})
		if err != nil {
			t.Fatalf("PutTree sub: %v", err)
		}
		// Two parents sharing the same subtree.
		t1, err := s.PutTree(ctx, []TreeEntry{
			{Name: "sub", Kind: KindTree, ID: sub},
			{Name: "y", Kind: KindBlob, ID: by},
		})
		if err != nil {
			t.Fatalf("PutTree t1: %v", err)
		}
		// Same entries (unsorted) → same id (dedup).
		t1b, err := s.PutTree(ctx, []TreeEntry{
			{Name: "y", Kind: KindBlob, ID: by},
			{Name: "sub", Kind: KindTree, ID: sub},
		})
		if err != nil {
			t.Fatalf("PutTree t1b: %v", err)
		}
		if t1 != t1b {
			t.Fatalf("tree dedup broken: %s != %s", t1, t1b)
		}
		entries, err := s.GetTree(ctx, t1)
		if err != nil {
			t.Fatalf("GetTree: %v", err)
		}
		if len(entries) != 2 || entries[0].Name != "sub" || entries[1].Name != "y" {
			t.Fatalf("GetTree entries = %+v", entries)
		}
		// GetTree on a blob is ErrInvalid.
		if _, err := s.GetTree(ctx, bx); !errors.Is(err, ErrInvalid) {
			t.Fatalf("GetTree(blob) = %v, want ErrInvalid", err)
		}
	})
}

func TestPutTreeValidationAndEmpty(t *testing.T) {
	eachStore(t, func(t *testing.T, s ObjectStore) {
		ctx := context.Background()
		good := ObjectID("sha256:" + strings.Repeat("a", 64))
		if _, err := s.PutTree(ctx, []TreeEntry{{Name: "a/b", Kind: KindBlob, ID: good}}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("bad name = %v, want ErrInvalid", err)
		}
		// Empty tree is legal and round-trips.
		empty, err := s.PutTree(ctx, nil)
		if err != nil {
			t.Fatalf("empty tree: %v", err)
		}
		entries, err := s.GetTree(ctx, empty)
		if err != nil || len(entries) != 0 {
			t.Fatalf("GetTree(empty) = %+v, %v", entries, err)
		}
	})
}

func TestWalkDedupsAndIterate(t *testing.T) {
	eachStore(t, func(t *testing.T, s ObjectStore) {
		ctx := context.Background()
		shared, _ := s.PutBlob(ctx, []byte("shared"))
		// Tree references the same blob under two names → one visit.
		root, err := s.PutTree(ctx, []TreeEntry{
			{Name: "one", Kind: KindBlob, ID: shared},
			{Name: "two", Kind: KindBlob, ID: shared},
		})
		if err != nil {
			t.Fatalf("PutTree: %v", err)
		}
		visits := map[ObjectID]int{}
		if err := s.Walk(ctx, root, func(id ObjectID, _ Kind) error {
			visits[id]++
			return nil
		}); err != nil {
			t.Fatalf("Walk: %v", err)
		}
		if visits[shared] != 1 {
			t.Fatalf("shared blob visited %d times, want 1", visits[shared])
		}
		if visits[root] != 1 {
			t.Fatalf("root visited %d times, want 1", visits[root])
		}
		// Walk surfaces fn errors.
		sentinel := errors.New("stop")
		if err := s.Walk(ctx, root, func(ObjectID, Kind) error { return sentinel }); !errors.Is(err, sentinel) {
			t.Fatalf("Walk error not propagated: %v", err)
		}
		// Iterate enumerates exactly the present objects (root + shared).
		count := 0
		seenRoot, seenShared := false, false
		if err := s.Iterate(ctx, func(id ObjectID) error {
			count++
			if id == root {
				seenRoot = true
			}
			if id == shared {
				seenShared = true
			}
			return nil
		}); err != nil {
			t.Fatalf("Iterate: %v", err)
		}
		if count != 2 || !seenRoot || !seenShared {
			t.Fatalf("Iterate count=%d root=%v shared=%v", count, seenRoot, seenShared)
		}
	})
}

func TestDeleteRemoves(t *testing.T) {
	eachStore(t, func(t *testing.T, s ObjectStore) {
		ctx := context.Background()
		id, _ := s.PutBlob(ctx, []byte("doomed"))
		if err := s.Delete(ctx, id); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if has, _ := s.Has(ctx, id); has {
			t.Fatalf("object still present after Delete")
		}
		// Delete is a no-op when absent.
		if err := s.Delete(ctx, id); err != nil {
			t.Fatalf("Delete (absent): %v", err)
		}
	})
}

func TestConcurrentIdenticalPut(t *testing.T) {
	eachStore(t, func(t *testing.T, s ObjectStore) {
		ctx := context.Background()
		const n = 64
		ids := make([]ObjectID, n)
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				id, err := s.PutBlob(ctx, []byte("concurrent"))
				if err != nil {
					t.Errorf("PutBlob: %v", err)
					return
				}
				ids[i] = id
			}(i)
		}
		wg.Wait()
		for i := 1; i < n; i++ {
			if ids[i] != ids[0] {
				t.Fatalf("concurrent puts produced differing ids: %s != %s", ids[i], ids[0])
			}
		}
		kind, body, err := s.Get(ctx, ids[0])
		if err != nil || kind != KindBlob || string(body) != "concurrent" {
			t.Fatalf("Get after concurrent put: %s %q %v", kind, body, err)
		}
	})
}

// --- LocalStore-specific ---

func TestNewLocalStoreErrors(t *testing.T) {
	t.Parallel()
	if _, err := NewLocalStore(LocalConfig{Root: ""}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty root = %v, want ErrInvalid", err)
	}
	if _, err := NewLocalStore(LocalConfig{Root: t.TempDir(), Algo: Algo("md5")}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad algo = %v, want ErrInvalid", err)
	}
}

func TestLocalCorruptionDetected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ls := newLocal(t)
	id, _ := ls.PutBlob(ctx, []byte("real content"))
	path, err := ls.objectPath(id)
	if err != nil {
		t.Fatalf("objectPath: %v", err)
	}
	// Garbage that is not valid zstd → decode failure → ErrCorrupt.
	if err := os.WriteFile(path, []byte("not zstd"), 0o644); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	if _, _, err := ls.Get(ctx, id); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Get(corrupt) = %v, want ErrCorrupt", err)
	}

	// Valid zstd but wrong content (hash mismatch) → ErrCorrupt.
	other, _ := ls.PutBlob(ctx, []byte("other content"))
	otherPath, _ := ls.objectPath(other)
	otherBytes, _ := os.ReadFile(otherPath)
	if err := os.WriteFile(path, otherBytes, 0o644); err != nil {
		t.Fatalf("swap: %v", err)
	}
	if _, _, err := ls.Get(ctx, id); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Get(swapped) = %v, want ErrCorrupt", err)
	}
}

func TestLocalIterateSkipsTempFiles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ls := newLocal(t)
	id, _ := ls.PutBlob(ctx, []byte("keep"))
	// Drop a stray temp file in the algo dir; Iterate must skip it.
	stray := filepath.Join(ls.root, "objects", string(ls.algo), "tmp-stray")
	if err := os.WriteFile(stray, []byte("junk"), 0o644); err != nil {
		t.Fatalf("write stray: %v", err)
	}
	var got []ObjectID
	if err := ls.Iterate(ctx, func(o ObjectID) error { got = append(got, o); return nil }); err != nil {
		t.Fatalf("Iterate: %v", err)
	}
	if len(got) != 1 || got[0] != id {
		t.Fatalf("Iterate = %v, want [%s]", got, id)
	}
}

func TestMemCorruptionDetected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := NewMemStore("")
	id, _ := m.PutBlob(ctx, []byte("real"))
	// White-box tamper: keep the key, change the bytes.
	m.objs[id] = memObject{kind: KindBlob, body: []byte("tampered")}
	if _, _, err := m.Get(ctx, id); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Get(tampered mem) = %v, want ErrCorrupt", err)
	}
}

func TestRootAndAlgoAccessors(t *testing.T) {
	t.Parallel()
	ls := newLocal(t)
	if ls.Root() == "" || ls.Algo() != AlgoSHA256 {
		t.Fatalf("local accessors: root=%q algo=%q", ls.Root(), ls.Algo())
	}
	m := NewMemStore("")
	if m.Root() != "mem://" || m.Algo() != AlgoSHA256 {
		t.Fatalf("mem accessors: root=%q algo=%q", m.Root(), m.Algo())
	}
}

func TestLocalModTime(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ls := newLocal(t)
	id, _ := ls.PutBlob(ctx, []byte("timed"))
	mt, err := ls.ModTime(ctx, id)
	if err != nil {
		t.Fatalf("ModTime: %v", err)
	}
	if time.Since(mt) > time.Minute {
		t.Fatalf("ModTime looks stale: %v", mt)
	}
	absent := ObjectID("sha256:" + strings.Repeat("0", 64))
	if _, err := ls.ModTime(ctx, absent); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ModTime(absent) = %v, want ErrNotFound", err)
	}
	if _, err := ls.ModTime(ctx, "garbage"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("ModTime(garbage) = %v, want ErrInvalid", err)
	}
}
