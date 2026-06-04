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
	"sync"
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

	// casLocks serializes CompareAndSwap operations on the same logical
	// path within this process. The local Phase-1 contract is "best-effort
	// on local; native on remote (future)" (state-store.md §6); the
	// per-path mutex tightens the in-process behavior so the property test
	// for two-CAS-one-wins is deterministic without breaking the
	// documented cross-process race in §3.3.
	casLocks sync.Map // map[string]*sync.Mutex
}

// casMutex returns (creating if necessary) the per-path mutex used to
// serialize in-process CompareAndSwap on a single logical path.
func (s *LocalStore) casMutex(p string) *sync.Mutex {
	if v, ok := s.casLocks.Load(p); ok {
		return v.(*sync.Mutex)
	}
	m := &sync.Mutex{}
	actual, _ := s.casLocks.LoadOrStore(p, m)
	return actual.(*sync.Mutex)
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
	info, err := statFn(abs)
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

// CompareAndSwap implements StateStore.CompareAndSwap as a Read-then-Write
// pair per state-store.md §3.3. It returns an error wrapping ErrNotFound if
// the target object does not exist, and an error wrapping ErrConflict if the
// current revision does not match oldRev.
//
// Phase-1 caveat: the read-then-write window is not atomic across processes;
// a concurrent writer between the two calls may cause a successful CAS to
// race with a non-CAS Write. This is documented as acceptable in §3.3
// because CAS is used only on refs/indexes where loser-retries are cheap.
// The future remote driver will use the object store's native conditional
// update.
//
// Within a single process the implementation takes a per-path mutex so two
// goroutines calling CompareAndSwap with the same oldRev see a deterministic
// winner; this strengthens (not weakens) the documented contract.
func (s *LocalStore) CompareAndSwap(ctx context.Context, p string, oldRev string, data []byte) (ObjectMeta, error) {
	if err := ctx.Err(); err != nil {
		return ObjectMeta{}, err
	}
	if err := ValidatePath(p); err != nil {
		return ObjectMeta{}, err
	}
	mu := s.casMutex(p)
	mu.Lock()
	defer mu.Unlock()

	_, meta, err := s.Read(ctx, p)
	if err != nil {
		// Read already wraps ErrNotFound / ErrInvalid appropriately;
		// pass through unchanged so callers can errors.Is.
		return ObjectMeta{}, err
	}
	if meta.Revision != oldRev {
		return ObjectMeta{}, fmt.Errorf("%w: path %s: have %s, want %s", ErrConflict, p, meta.Revision, oldRev)
	}
	return s.Write(ctx, p, data, WriteOptions{})
}

// List implements StateStore.List by walking the translated directory tree
// rooted at the prefix. Per state-store.md §3.4: order is unspecified,
// symlinks are not followed (Phase-1 layout introduces none), `.orun-tmp-*`
// orphan tempfiles are filtered out, and returned paths are logical
// (forward-slash, root-relative, no leading slash).
//
// An empty prefix lists every object under the store root. A non-empty
// prefix is validated as a logical path and may name either a directory or
// a single file; if it names a missing path, an empty slice (no error) is
// returned, matching the "list-as-scan" semantics callers expect.
func (s *LocalStore) List(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var startAbs string
	if prefix == "" {
		startAbs = s.root
	} else {
		abs, err := s.translate(prefix)
		if err != nil {
			return nil, err
		}
		startAbs = abs
	}

	info, err := os.Stat(startAbs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []ObjectInfo{}, nil
		}
		return nil, fmt.Errorf("statestore: stat %s: %w", prefix, err)
	}

	out := make([]ObjectInfo, 0)

	// If prefix names a single file, return just that entry.
	if !info.IsDir() {
		if strings.HasPrefix(filepath.Base(startAbs), orphanTempPrefix) {
			return out, nil
		}
		logical := s.logicalPath(startAbs)
		return []ObjectInfo{{
			Path:      logical,
			Size:      info.Size(),
			UpdatedAt: info.ModTime(),
		}}, nil
	}

	walkErr := filepath.WalkDir(startAbs, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// Skip symlinks per §3.4 ("symlinks not followed"). DirEntry.Type
		// reports the lstat-derived mode bits without following links.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		// Skip non-regular entries (sockets, devices, pipes) — Phase-1
		// layout never produces these, so they are necessarily noise.
		if !d.Type().IsRegular() {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, orphanTempPrefix) {
			return nil
		}
		fi, ierr := d.Info()
		if ierr != nil {
			// Skip vanished entries (race with concurrent Delete);
			// surface other stat errors.
			if errors.Is(ierr, fs.ErrNotExist) {
				return nil
			}
			return ierr
		}
		logical := s.logicalPath(path)
		out = append(out, ObjectInfo{
			Path:      logical,
			Size:      fi.Size(),
			UpdatedAt: fi.ModTime(),
		})
		return nil
	})
	if walkErr != nil {
		if errors.Is(walkErr, context.Canceled) || errors.Is(walkErr, context.DeadlineExceeded) {
			return nil, walkErr
		}
		return nil, fmt.Errorf("statestore: list %s: %w", prefix, walkErr)
	}
	return out, nil
}

// logicalPath converts an absolute filesystem path under s.root back to a
// logical (forward-slash, root-relative, no leading slash) path. The caller
// is responsible for only passing paths that come from a WalkDir rooted at
// s.root, so the "outside root" / "is root" cases are unreachable here and
// we keep the function trivially correct.
func (s *LocalStore) logicalPath(abs string) string {
	rel, _ := filepath.Rel(s.root, abs)
	return filepath.ToSlash(rel)
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
		entries, derr := readDirFn(abs)
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
	if err := removeFn(abs); err != nil {
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

// readDirFn / removeFn are the directory-read and unlink primitives used by
// Delete, indirected for fault injection (matching the write-path seams above)
// so the readdir-error and remove-error branches are testable without staging a
// real filesystem fault.
var (
	readDirFn = os.ReadDir
	removeFn  = os.Remove
	statFn    = os.Stat
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
