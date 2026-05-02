package statebackend

import "context"

// FileLock provides cross-process mutual exclusion using advisory file locks.
// The lock file is created if it does not exist. On Unix systems, the lock is
// automatically released when the process exits (even on crash).
type FileLock struct {
	path string
	fd   int
}

// NewFileLock returns a lock bound to the given path. The lock is not acquired
// until Lock or TryLock is called.
func NewFileLock(path string) *FileLock {
	return &FileLock{path: path, fd: -1}
}

// Lock acquires an exclusive lock, blocking until the lock is available or ctx
// is cancelled. Returns a context error on timeout/cancellation.
func (fl *FileLock) Lock(ctx context.Context) error {
	return fl.lockImpl(ctx)
}

// TryLock attempts to acquire the lock without blocking. Returns true if the
// lock was acquired, false if it is held by another process.
func (fl *FileLock) TryLock() (bool, error) {
	return fl.tryLockImpl()
}

// Unlock releases the lock. It is safe to call on an unlocked FileLock.
func (fl *FileLock) Unlock() error {
	return fl.unlockImpl()
}
