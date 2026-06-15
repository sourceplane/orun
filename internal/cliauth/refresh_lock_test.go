package cliauth

import (
	"context"
	"runtime"
	"testing"
	"time"
)

// TestRefreshLock_SerializesAcquire proves the advisory lock is mutually
// exclusive: while one holder has it, a second acquire blocks until the holder
// releases (here surfaced as a context-deadline error, then success after
// release). This is what serializes refresh across concurrent orun processes.
func TestRefreshLock_SerializesAcquire(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("flock semantics differ on Windows; covered by LockFileEx in CI for that GOOS")
	}
	t.Setenv("HOME", t.TempDir())

	first, err := AcquireRefreshLock(context.Background())
	if err != nil {
		t.Fatalf("first AcquireRefreshLock: %v", err)
	}

	// A second acquire must NOT succeed while the first is held.
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := AcquireRefreshLock(ctx); err == nil {
		t.Fatal("second AcquireRefreshLock succeeded while the lock was held; want it to block")
	}

	if err := first.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	// After release the lock is free again.
	second, err := AcquireRefreshLock(context.Background())
	if err != nil {
		t.Fatalf("AcquireRefreshLock after release: %v", err)
	}
	if err := second.Release(); err != nil {
		t.Fatalf("second Release: %v", err)
	}
}

// TestRefreshLock_ReleaseNilSafe guards the defer in remotestate against a nil
// lock (best-effort acquisition path).
func TestRefreshLock_ReleaseNilSafe(t *testing.T) {
	var l *RefreshLock
	if err := l.Release(); err != nil {
		t.Fatalf("nil Release: %v", err)
	}
}
