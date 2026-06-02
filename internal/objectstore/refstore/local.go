package refstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/clock"
	"github.com/sourceplane/orun/internal/objectstore"
)

// LocalRefStore is the filesystem RefStore driver. Refs live at
// <root>/refs/<name>.json. Updates are serialized per-ref by an exclusive
// lockfile (cross-process and cross-goroutine) and written atomically
// (temp + rename), so a reader never observes a half-written ref and concurrent
// compare-and-swaps have exactly one winner.
type LocalRefStore struct {
	root   string
	writer string
	clk    clock.Clock
}

// LocalConfig configures a LocalRefStore.
type LocalConfig struct {
	// Root is the store root; refs live under <Root>/refs/.
	Root string
	// Writer labels who is moving refs ("cli"|"runner"|"tui"|"saas"|"migrate").
	Writer string
	// Clock supplies UpdatedAt timestamps; clock.New() when nil.
	Clock clock.Clock
}

// NewLocalRefStore constructs a LocalRefStore, creating the refs/ root.
func NewLocalRefStore(cfg LocalConfig) (*LocalRefStore, error) {
	if cfg.Root == "" {
		return nil, fmt.Errorf("%w: empty ref store root", ErrInvalid)
	}
	clk := cfg.Clock
	if clk == nil {
		clk = clock.New()
	}
	writer := cfg.Writer
	if writer == "" {
		writer = "cli"
	}
	if err := os.MkdirAll(filepath.Join(cfg.Root, "refs"), 0o755); err != nil {
		return nil, fmt.Errorf("refstore: mkdir refs: %w", err)
	}
	return &LocalRefStore{root: cfg.Root, writer: writer, clk: clk}, nil
}

func (r *LocalRefStore) refPath(name string) string {
	return filepath.Join(r.root, "refs", filepath.FromSlash(name)+".json")
}

// Read returns the ref at name.
func (r *LocalRefStore) Read(_ context.Context, name string) (Ref, error) {
	if !validRefName(name) {
		return Ref{}, fmt.Errorf("%w: ref name %q", ErrInvalid, name)
	}
	return r.read(name)
}

func (r *LocalRefStore) read(name string) (Ref, error) {
	data, err := os.ReadFile(r.refPath(name))
	if err != nil {
		if os.IsNotExist(err) {
			return Ref{}, ErrNotFound
		}
		return Ref{}, fmt.Errorf("refstore: read %s: %w", name, err)
	}
	var ref Ref
	if err := json.Unmarshal(data, &ref); err != nil {
		return Ref{}, fmt.Errorf("%w: decode ref %s: %v", ErrInvalid, name, err)
	}
	return ref, nil
}

// Update performs a compare-and-swap on the ref target.
func (r *LocalRefStore) Update(_ context.Context, name, oldTarget, newTarget string) error {
	if !validRefName(name) {
		return fmt.Errorf("%w: ref name %q", ErrInvalid, name)
	}
	if err := validTarget(newTarget); err != nil {
		return fmt.Errorf("ref %q new target: %w", name, err)
	}
	return r.withLock(name, func() error {
		cur := ""
		ref, err := r.read(name)
		switch {
		case err == nil:
			cur = ref.Target
		case errors.Is(err, ErrNotFound):
			cur = ""
		default:
			return err
		}
		if cur != oldTarget {
			return fmt.Errorf("%w: ref %q expected %q, found %q", ErrConflict, name, oldTarget, cur)
		}
		next := Ref{
			Kind:      "Ref",
			Target:    newTarget,
			UpdatedAt: r.clk.Now(),
			Writer:    r.writer,
		}
		return r.writeAtomic(name, next)
	})
}

func (r *LocalRefStore) writeAtomic(name string, ref Ref) error {
	path := r.refPath(name)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("refstore: mkdir %s: %w", dir, err)
	}
	data, err := objectstore.CanonicalEncode(ref)
	if err != nil {
		return fmt.Errorf("refstore: encode ref %s: %w", name, err)
	}
	tmp, err := os.CreateTemp(dir, "tmp-*")
	if err != nil {
		return fmt.Errorf("refstore: temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("refstore: write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("refstore: fsync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("refstore: close: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("refstore: rename: %w", err)
	}
	cleanup = false
	return nil
}

// withLock holds an exclusive lockfile for name while fn runs.
func (r *LocalRefStore) withLock(name string, fn func() error) error {
	lockPath := r.refPath(name) + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return fmt.Errorf("refstore: mkdir lock dir: %w", err)
	}
	const maxAttempts = 2000
	for attempt := 0; ; attempt++ {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_ = f.Close()
			break
		}
		if !os.IsExist(err) {
			return fmt.Errorf("refstore: acquire lock %s: %w", name, err)
		}
		if attempt >= maxAttempts {
			return fmt.Errorf("%w: ref %q lock contended", ErrConflict, name)
		}
		time.Sleep(time.Millisecond)
	}
	defer func() { _ = os.Remove(lockPath) }()
	return fn()
}

// List returns the sorted logical names of every ref under prefix.
func (r *LocalRefStore) List(_ context.Context, prefix string) ([]string, error) {
	base := filepath.Join(r.root, "refs")
	var names []string
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, "tmp-") || strings.HasSuffix(name, ".lock") || !strings.HasSuffix(name, ".json") {
			return nil
		}
		rel, rerr := filepath.Rel(base, path)
		if rerr != nil {
			return rerr
		}
		logical := strings.TrimSuffix(filepath.ToSlash(rel), ".json")
		if prefix == "" || strings.HasPrefix(logical, prefix) {
			names = append(names, logical)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	return names, nil
}

// Delete removes the ref at name (no-op if absent).
func (r *LocalRefStore) Delete(_ context.Context, name string) error {
	if !validRefName(name) {
		return fmt.Errorf("%w: ref name %q", ErrInvalid, name)
	}
	if err := os.Remove(r.refPath(name)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("refstore: delete %s: %w", name, err)
	}
	return nil
}

var _ RefStore = (*LocalRefStore)(nil)
