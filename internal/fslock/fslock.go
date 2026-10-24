// Package fslock holds advisory file locks that every orun process on a
// machine respects: the tool cache a setup action writes into and the
// Terraform plugin cache a job holds are shared by the matrix-mode processes
// and the co-hosted lanes of one host, not only by the jobs of one process
// (specs/orun-cache-efficiency CE2). A lock is flock(2) on Unix and
// LockFileEx on Windows, released when the holder closes the file or exits,
// so a crashed holder never leaves one behind.
package fslock

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// Lock is a held lock. Release it when done; a nil Lock releases nothing.
type Lock struct {
	f    *os.File
	path string
}

// Acquire takes the exclusive lock at path, creating the file and its
// directory as needed, and waits until the lock is free or ctx is done.
func Acquire(ctx context.Context, path string) (*Lock, error) {
	f, err := open(path)
	if err != nil {
		return nil, err
	}
	for {
		ok, err := tryLockExclusive(f)
		if err != nil {
			_ = f.Close()
			return nil, err
		}
		if ok {
			return &Lock{f: f, path: path}, nil
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// TryAcquire takes the exclusive lock at path if it is free and reports
// whether it did; it never waits.
func TryAcquire(path string) (*Lock, bool, error) {
	f, err := open(path)
	if err != nil {
		return nil, false, err
	}
	ok, err := tryLockExclusive(f)
	if err != nil {
		_ = f.Close()
		return nil, false, err
	}
	if !ok {
		_ = f.Close()
		return nil, false, nil
	}
	return &Lock{f: f, path: path}, true, nil
}

// Path is the lock file's path.
func (l *Lock) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Release unlocks and closes the lock file. Safe on a nil or released Lock.
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	unlockErr := unlock(l.f)
	closeErr := l.f.Close()
	l.f = nil
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}

func open(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
}
