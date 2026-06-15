//go:build windows

package cliauth

import (
	"context"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// flockExclusive takes an exclusive lock on the whole file via LockFileEx,
// polling with LOCKFILE_FAIL_IMMEDIATELY so ctx cancellation/deadline is
// honored (LockFileEx without the flag would block uninterruptibly).
func flockExclusive(ctx context.Context, f *os.File) error {
	h := windows.Handle(f.Fd())
	for {
		var overlapped windows.Overlapped
		err := windows.LockFileEx(
			h,
			windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
			0, 1, 0, &overlapped,
		)
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func flockUnlock(f *os.File) error {
	var overlapped windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &overlapped)
}
