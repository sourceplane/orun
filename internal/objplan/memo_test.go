package objplan

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/objectstore"
)

func id(c string) objectstore.ObjectID {
	return objectstore.ObjectID("sha256:" + strings.Repeat(c, 64))
}

func TestResolveMemoRoundTrip(t *testing.T) {
	t.Parallel()
	m := NewResolveMemo(t.TempDir())
	src := id("a")
	if _, ok := m.Get(src, 1); ok {
		t.Fatalf("expected miss before Put")
	}
	if err := m.Put(src, 1, id("b")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok := m.Get(src, 1)
	if !ok || got != id("b") {
		t.Fatalf("Get = %s,%v", got, ok)
	}
	// Different resolver version is a distinct key (miss).
	if _, ok := m.Get(src, 2); ok {
		t.Fatalf("rv=2 should miss")
	}
}

func TestResolveMemoBadCatalogID(t *testing.T) {
	t.Parallel()
	m := NewResolveMemo(t.TempDir())
	if err := m.Put(id("a"), 1, objectstore.ObjectID("not-an-id")); !errors.Is(err, objectstore.ErrInvalid) {
		t.Fatalf("Put(bad id) = %v, want ErrInvalid", err)
	}
}

func TestResolveMemoGarbageAndEmptyFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	m := NewResolveMemo(root)
	src := id("c")
	// Write a garbage memo file directly; Get must treat it as a miss.
	path := m.key(src, 1)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, ok := m.Get(src, 1); ok {
		t.Fatalf("garbage memo should be a miss")
	}
	// Empty catalogId is also a miss.
	if err := os.WriteFile(path, []byte(`{"catalogId":""}`), 0o644); err != nil {
		t.Fatalf("seed empty: %v", err)
	}
	if _, ok := m.Get(src, 1); ok {
		t.Fatalf("empty catalogId should be a miss")
	}
}
