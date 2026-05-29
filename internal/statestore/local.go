package statestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// orphanTempPrefix is the filename prefix used for in-flight tempfiles
// produced by Write. The suffix is filled in by os.CreateTemp.
const orphanTempPrefix = ".orun-tmp-"

// orphanSweepMaxAge is the cut-off age beyond which a leftover .orun-tmp-*
// file is considered orphaned and cleaned up at NewLocalStore time.
const orphanSweepMaxAge = time.Hour

// LocalConfig configures NewLocalStore. The zero value is invalid: Root must
// be set.
type LocalConfig struct {
	// Root is the absolute path to the .orun directory that backs the
	// store. The directory will be created (along with missing parents) if
	// it does not already exist.
	Root string

	// Clock returns the current wall-clock time used to stamp ObjectMeta
	// UpdatedAt values and to decide whether a leftover tempfile is past
	// the orphan-sweep age threshold. If nil, time.Now is used.
	//
	// Phase 1 deliberately keeps this as a func rather than a repo-wide
	// clock.Clock interface; the M0 follow-up will introduce that
	// abstraction across the codebase.
	Clock func() time.Time
}

// LocalStore is the local-filesystem driver for StateStore. Construct via
// NewLocalStore. LocalStore values are safe for concurrent use.
type LocalStore struct {
	root  string
	clock func() time.Time
}

// NewLocalStore returns a LocalStore rooted at cfg.Root, creating the root
// directory if it does not exist. It runs a best-effort orphan-tempfile
// sweep before returning; sweep failures never prevent construction.
//
// Returns an error wrapping ErrInvalid if cfg.Root is empty or not absolute.
func NewLocalStore(cfg LocalConfig) (*LocalStore, error) {
	if cfg.Root == "" {
		return nil, fmt.Errorf("%w: LocalConfig.Root is empty", ErrInvalid)
	}
	if !filepath.IsAbs(cfg.Root) {
		return nil, fmt.Errorf("%w: LocalConfig.Root %q is not absolute", ErrInvalid, cfg.Root)
	}
	if err := os.MkdirAll(cfg.Root, 0o755); err != nil {
		return nil, fmt.Errorf("statestore: mkdir root %s: %w", cfg.Root, err)
	}
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	s := &LocalStore{root: cfg.Root, clock: clock}
	// Best-effort: ignore sweep errors. The store is fully functional
	// regardless; orphans simply remain on disk and will be picked up on a
	// subsequent construction.
	_ = s.sweepOrphanTempfiles()
	return s, nil
}

// Root reports the absolute filesystem path the store is rooted at.
func (s *LocalStore) Root() string { return s.root }

// translate converts a logical path to an absolute filesystem path under the
// store root, validating the input first. The path is also passed through
// filepath.Clean and re-checked with strings.HasPrefix to defend against
// rooted/escape inputs that somehow slipped past ValidatePath.
func (s *LocalStore) translate(p string) (string, error) {
	if err := ValidatePath(p); err != nil {
		return "", err
	}
	abs := filepath.Join(s.root, filepath.FromSlash(p))
	cleaned := filepath.Clean(abs)
	// Defense in depth: cleaned must remain inside root.
	rootWithSep := s.root
	if !strings.HasSuffix(rootWithSep, string(os.PathSeparator)) {
		rootWithSep += string(os.PathSeparator)
	}
	if cleaned != s.root && !strings.HasPrefix(cleaned, rootWithSep) {
		return "", fmt.Errorf("%w: translated path %q escapes store root %q", ErrInvalid, cleaned, s.root)
	}
	return cleaned, nil
}

// Read implements StateStore.Read.
func (s *LocalStore) Read(ctx context.Context, p string) ([]byte, ObjectMeta, error) {
	if err := ctx.Err(); err != nil {
		return nil, ObjectMeta{}, err
	}
	abs, err := s.translate(p)
	if err != nil {
		return nil, ObjectMeta{}, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ObjectMeta{}, fmt.Errorf("%w: %s", ErrNotFound, p)
		}
		return nil, ObjectMeta{}, fmt.Errorf("statestore: read %s: %w", p, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, ObjectMeta{}, fmt.Errorf("statestore: stat %s: %w", p, err)
	}
	return data, ObjectMeta{
		Path:      p,
		Size:      info.Size(),
		Revision:  hashRevision(data),
		UpdatedAt: info.ModTime(),
	}, nil
}

// Write implements StateStore.Write atomically: tempfile in the destination
// directory, fsync, rename. On EXDEV (cross-device rename) the implementation
// retries with a copy into a fresh tempfile inside the destination directory
// followed by an in-FS rename. Concurrent readers see either the previous
// bytes or the new bytes — never partial.
func (s *LocalStore) Write(ctx context.Context, p string, data []byte, _ WriteOptions) (ObjectMeta, error) {
	if err := ctx.Err(); err != nil {
		return ObjectMeta{}, err
	}
	abs, err := s.translate(p)
	if err != nil {
		return ObjectMeta{}, err
	}
	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ObjectMeta{}, fmt.Errorf("statestore: mkdir %s: %w", dir, err)
	}
	if err := writeAtomic(dir, abs, data); err != nil {
		return ObjectMeta{}, fmt.Errorf("statestore: write %s: %w", p, err)
	}
	now := s.clock()
	// Best-effort: keep on-disk mtime aligned with the configured clock so
	// tests that drive Clock observe deterministic UpdatedAt values when
	// they re-Stat the file directly.
	_ = os.Chtimes(abs, now, now)
	return ObjectMeta{
		Path:      p,
		Size:      int64(len(data)),
		Revision:  hashRevision(data),
		UpdatedAt: now,
	}, nil
}

// CreateIfAbsent implements StateStore.CreateIfAbsent via O_EXCL. Two
// concurrent CreateIfAbsent calls on the same path are guaranteed to result
// in exactly one success; the loser observes ErrExists.
func (s *LocalStore) CreateIfAbsent(ctx context.Context, p string, data []byte) (ObjectMeta, error) {
	if err := ctx.Err(); err != nil {
		return ObjectMeta{}, err
	}
	abs, err := s.translate(p)
	if err != nil {
		return ObjectMeta{}, err
	}
	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ObjectMeta{}, fmt.Errorf("statestore: mkdir %s: %w", dir, err)
	}
	f, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return ObjectMeta{}, fmt.Errorf("%w: %s", ErrExists, p)
		}
		return ObjectMeta{}, fmt.Errorf("statestore: create %s: %w", p, err)
	}
	if _, werr := writeFn(f, data); werr != nil {
		_ = closeFn(f)
		_ = os.Remove(abs)
		return ObjectMeta{}, fmt.Errorf("statestore: write %s: %w", p, werr)
	}
	if syncErr := syncFn(f); syncErr != nil {
		_ = closeFn(f)
		_ = os.Remove(abs)
		return ObjectMeta{}, fmt.Errorf("statestore: fsync %s: %w", p, syncErr)
	}
	if closeErr := closeFn(f); closeErr != nil {
		_ = os.Remove(abs)
		return ObjectMeta{}, fmt.Errorf("statestore: close %s: %w", p, closeErr)
	}
	now := s.clock()
	_ = os.Chtimes(abs, now, now)
	return ObjectMeta{
		Path:      p,
		Size:      int64(len(data)),
		Revision:  hashRevision(data),
		UpdatedAt: now,
	}, nil
}

// CompareAndSwap is not implemented in PR A. It is part of PR B and currently
// returns an error wrapping ErrInvalid. Higher layers MUST NOT call it until
// PR B lands; the interface signature is part of the M2 contract freeze.
func (s *LocalStore) CompareAndSwap(ctx context.Context, p string, oldRev string, data []byte) (ObjectMeta, error) {
	return ObjectMeta{}, fmt.Errorf("%w: CompareAndSwap not implemented in PR A (M2 PR B)", ErrInvalid)
}

// List is not implemented in PR A. It is part of PR B and currently returns
// an error wrapping ErrInvalid.
func (s *LocalStore) List(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	return nil, fmt.Errorf("%w: List not implemented in PR A (M2 PR B)", ErrInvalid)
}

// Delete implements StateStore.Delete: a no-op for absent files, ErrInvalid
// for non-empty directories, surfaces underlying errors otherwise.
func (s *LocalStore) Delete(ctx context.Context, p string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	abs, err := s.translate(p)
	if err != nil {
		return err
	}
	info, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("statestore: stat %s: %w", p, err)
	}
	if info.IsDir() {
		entries, derr := os.ReadDir(abs)
		if derr != nil {
			return fmt.Errorf("statestore: readdir %s: %w", p, derr)
		}
		if len(entries) > 0 {
			return fmt.Errorf("%w: refusing to delete non-empty directory %s", ErrInvalid, p)
		}
		// Empty directories under .orun are considered structural and not
		// owned by callers; refuse the delete to avoid surprising removals.
		return fmt.Errorf("%w: %s is a directory; recursive deletion is not supported", ErrInvalid, p)
	}
	if err := os.Remove(abs); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("statestore: remove %s: %w", p, err)
	}
	return nil
}

// hashRevision returns the lowercase-hex sha256 of data — the content-derived
// revision identifier referenced by ObjectMeta.Revision and CompareAndSwap.
func hashRevision(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// renameFunc is the rename primitive used by writeAtomic. It defaults to
// os.Rename and is overridden in tests to exercise the EXDEV fallback path
// without requiring a real cross-device mount.
var renameFunc = os.Rename

// writeFn / syncFn / closeFn are file-op primitives indirected for test
// fault injection. They default to direct method dispatch on *os.File.
var (
	writeFn = func(f *os.File, data []byte) (int, error) { return f.Write(data) }
	syncFn  = func(f *os.File) error { return f.Sync() }
	closeFn = func(f *os.File) error { return f.Close() }
)

// writeAtomic writes data to dst via a tempfile inside dir, fsync, rename.
// On EXDEV (cross-device) the implementation copies into a fresh tempfile
// inside dir and renames again — by construction dir is the same FS as dst,
// so the second rename cannot EXDEV.
func writeAtomic(dir, dst string, data []byte) error {
	tmp, err := os.CreateTemp(dir, orphanTempPrefix+"*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, werr := writeFn(tmp, data); werr != nil {
		_ = closeFn(tmp)
		cleanup()
		return fmt.Errorf("write temp: %w", werr)
	}
	if syncErr := syncFn(tmp); syncErr != nil {
		_ = closeFn(tmp)
		cleanup()
		return fmt.Errorf("fsync temp: %w", syncErr)
	}
	if closeErr := closeFn(tmp); closeErr != nil {
		cleanup()
		return fmt.Errorf("close temp: %w", closeErr)
	}
	if rerr := renameFunc(tmpName, dst); rerr != nil {
		// Cross-device fallback. The tempfile was created inside dir, so
		// EXDEV here is exotic — but the spec calls out "cross-device
		// rename failure" as a possibility on certain mounts, so handle
		// it by re-copying into a fresh tempfile inside dir.
		if isCrossDeviceErr(rerr) {
			cleanup()
			return crossDeviceCopyRename(dir, dst, data)
		}
		cleanup()
		return fmt.Errorf("rename temp -> dst: %w", rerr)
	}
	return nil
}

// crossDeviceCopyRename handles the EXDEV path by writing data into a fresh
// tempfile inside dir (same filesystem as dst) via a buffered io.Copy, then
// renaming. Because dir is the destination directory, the rename cannot
// EXDEV again.
func crossDeviceCopyRename(dir, dst string, data []byte) error {
	tmp, err := os.CreateTemp(dir, orphanTempPrefix+"*")
	if err != nil {
		return fmt.Errorf("cross-device fallback: create temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, cerr := writeFn(tmp, data); cerr != nil {
		_ = closeFn(tmp)
		cleanup()
		return fmt.Errorf("cross-device fallback: copy: %w", cerr)
	}
	if syncErr := syncFn(tmp); syncErr != nil {
		_ = closeFn(tmp)
		cleanup()
		return fmt.Errorf("cross-device fallback: fsync: %w", syncErr)
	}
	if closeErr := closeFn(tmp); closeErr != nil {
		cleanup()
		return fmt.Errorf("cross-device fallback: close: %w", closeErr)
	}
	if rerr := os.Rename(tmpName, dst); rerr != nil {
		cleanup()
		return fmt.Errorf("cross-device fallback: rename: %w", rerr)
	}
	return nil
}

// isCrossDeviceErr reports whether err is the EXDEV "invalid cross-device
// link" error returned by os.Rename across filesystems.
func isCrossDeviceErr(err error) bool {
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return errors.Is(linkErr.Err, syscall.EXDEV)
	}
	return errors.Is(err, syscall.EXDEV)
}

// sweepOrphanTempfiles removes .orun-tmp-* files anywhere under root that are
// older than orphanSweepMaxAge per the configured clock. It walks the tree
// best-effort: walk errors and per-file remove errors are swallowed so a
// cleanup hiccup never blocks NewLocalStore.
func (s *LocalStore) sweepOrphanTempfiles() error {
	now := s.clock()
	walkErr := filepath.WalkDir(s.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Skip unreadable subtrees rather than abort the sweep.
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasPrefix(d.Name(), orphanTempPrefix) {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		if now.Sub(info.ModTime()) >= orphanSweepMaxAge {
			_ = os.Remove(path)
		}
		return nil
	})
	return walkErr
}
