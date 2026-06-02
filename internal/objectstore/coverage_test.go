package objectstore

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteCanonicalRejectsUnsupportedDirect(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := writeCanonical(&buf, 42); !errors.Is(err, ErrInvalid) {
		t.Fatalf("writeCanonical(int) = %v, want ErrInvalid", err)
	}
}

func TestUnknownAlgoMethods(t *testing.T) {
	t.Parallel()
	bad := Algo("md5")
	if _, err := bad.hexLen(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("hexLen: %v", err)
	}
	if _, err := bad.sum([]byte("x")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("sum: %v", err)
	}
	if _, err := bad.idFor([]byte("x")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("idFor: %v", err)
	}
}

func TestZstdLevelFromEnv(t *testing.T) {
	t.Setenv("ORUN_OBJECT_ZSTD_LEVEL", "5")
	if got := zstdLevelFromEnv(); got != 5 {
		t.Fatalf("level = %d, want 5", got)
	}
	t.Setenv("ORUN_OBJECT_ZSTD_LEVEL", "not-a-number")
	if got := zstdLevelFromEnv(); got != 3 {
		t.Fatalf("invalid level = %d, want fallback 3", got)
	}
	os.Unsetenv("ORUN_OBJECT_ZSTD_LEVEL")
	if got := zstdLevelFromEnv(); got != 3 {
		t.Fatalf("unset level = %d, want 3", got)
	}
}

func TestWalkCancelledAndDangling(t *testing.T) {
	eachStore(t, func(t *testing.T, s ObjectStore) {
		bg := context.Background()
		id, _ := s.PutBlob(bg, []byte("root"))

		ctx, cancel := context.WithCancel(bg)
		cancel()
		if err := s.Walk(ctx, id, func(ObjectID, Kind) error { return nil }); !errors.Is(err, context.Canceled) {
			t.Fatalf("Walk(cancelled) = %v, want context.Canceled", err)
		}

		fake := ObjectID("sha256:" + strings.Repeat("b", 64))
		treeID, err := s.PutTree(bg, []TreeEntry{{Name: "missing", Kind: KindBlob, ID: fake}})
		if err != nil {
			t.Fatalf("PutTree: %v", err)
		}
		if err := s.Walk(bg, treeID, func(ObjectID, Kind) error { return nil }); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Walk(dangling) = %v, want ErrNotFound", err)
		}
	})
}

func TestIterateFnError(t *testing.T) {
	eachStore(t, func(t *testing.T, s ObjectStore) {
		bg := context.Background()
		s.PutBlob(bg, []byte("one"))
		sentinel := errors.New("stop iterate")
		if err := s.Iterate(bg, func(ObjectID) error { return sentinel }); !errors.Is(err, sentinel) {
			t.Fatalf("Iterate fn error = %v, want sentinel", err)
		}
	})
}

func TestGetTreeInvalidAndAbsentAndDelete(t *testing.T) {
	eachStore(t, func(t *testing.T, s ObjectStore) {
		bg := context.Background()
		if _, err := s.GetTree(bg, "garbage"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("GetTree(garbage) = %v, want ErrInvalid", err)
		}
		absent := ObjectID("sha256:" + strings.Repeat("c", 64))
		if _, err := s.GetTree(bg, absent); !errors.Is(err, ErrNotFound) {
			t.Fatalf("GetTree(absent) = %v, want ErrNotFound", err)
		}
		// Has on an absent (well-formed) id is false,nil.
		if has, err := s.Has(bg, absent); err != nil || has {
			t.Fatalf("Has(absent) = %v,%v", has, err)
		}
	})
}

func TestLocalDeleteInvalidID(t *testing.T) {
	t.Parallel()
	ls := newLocal(t)
	if err := ls.Delete(context.Background(), "garbage"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Delete(garbage) = %v, want ErrInvalid", err)
	}
}

func TestNewLocalStoreMkdirFailure(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Block the objects/ path with a regular file so MkdirAll fails.
	if err := os.WriteFile(filepath.Join(root, "objects"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if _, err := NewLocalStore(LocalConfig{Root: root}); err == nil {
		t.Fatalf("NewLocalStore succeeded despite blocked objects/ path")
	}
}

func TestHexTailNoColon(t *testing.T) {
	t.Parallel()
	if got := hexTail(ObjectID("nocolon")); got != 0 {
		t.Fatalf("hexTail(nocolon) = %d, want 0", got)
	}
}

func TestDecodeTreeBodyBadAlgo(t *testing.T) {
	t.Parallel()
	if _, err := decodeTreeBody([]byte("blob x\x00"), Algo("md5")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("decodeTreeBody(bad algo) = %v, want ErrInvalid", err)
	}
}

func TestLocalIterateSkipsMalformedEntries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ls := newLocal(t)
	real, _ := ls.PutBlob(ctx, []byte("real"))
	algoDir := filepath.Join(ls.root, "objects", string(ls.algo))
	// (a) file directly under the algo dir → parent base is not a 2-char fanout.
	if err := os.WriteFile(filepath.Join(algoDir, "loose"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed loose: %v", err)
	}
	// (b) file in a valid 2-char fanout but with a wrong-length hex tail.
	short := filepath.Join(algoDir, "ab")
	if err := os.MkdirAll(short, 0o755); err != nil {
		t.Fatalf("mkdir short: %v", err)
	}
	if err := os.WriteFile(filepath.Join(short, "short"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed short: %v", err)
	}
	var got []ObjectID
	if err := ls.Iterate(ctx, func(o ObjectID) error { got = append(got, o); return nil }); err != nil {
		t.Fatalf("Iterate: %v", err)
	}
	if len(got) != 1 || got[0] != real {
		t.Fatalf("Iterate = %v, want only %s", got, real)
	}
}

func TestLocalWriteMkdirFailure(t *testing.T) {
	t.Parallel()
	ls := newLocal(t)
	data := []byte("blocked-fanout")
	_, id, err := computeBlobID(ls.algo, data)
	if err != nil {
		t.Fatalf("computeBlobID: %v", err)
	}
	path, err := ls.objectPath(id)
	if err != nil {
		t.Fatalf("objectPath: %v", err)
	}
	// Place a regular file where the fanout directory must be created.
	fanout := filepath.Dir(path)
	if err := os.WriteFile(fanout, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed fanout file: %v", err)
	}
	if _, err := ls.PutBlob(context.Background(), data); err == nil {
		t.Fatalf("PutBlob succeeded despite blocked fanout dir")
	}
}
