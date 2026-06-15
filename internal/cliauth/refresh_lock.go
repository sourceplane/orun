package cliauth

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// refreshLockName is the advisory lock file that serializes session-token
// refresh across concurrent orun processes (the in-process singleflight in
// remotestate covers goroutines within one process).
//
// Why this exists: the platform refresh token is rotating and single-use. When
// the short-lived access token expires and two orun invocations both notice at
// once — two terminals, a script, or one command firing parallel state
// requests — they race to redeem the SAME refresh token. The first rotates it;
// the rest present a now-spent token, trip the platform's reuse-detection, and
// the whole token family is revoked. To the user that looks like a session that
// "expires" seconds after logging in. Serializing the refresh (this lock) plus
// a double-checked reload (remotestate) makes the losers reuse the winner's
// freshly rotated token instead of redeeming a spent one.
const refreshLockName = "refresh.lock"

// RefreshLock is a held cross-process advisory lock. Release must be called.
type RefreshLock struct {
	f *os.File
}

// AcquireRefreshLock takes the cross-process refresh lock, blocking until it is
// free or ctx is done (deadline/cancel). It is intentionally best-effort: the
// caller should treat an error as "proceed unlocked" rather than failing the
// command — an unserialized refresh is still correct, just no longer race-free.
func AcquireRefreshLock(ctx context.Context) (*RefreshLock, error) {
	base, err := ensureConfigDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(base, refreshLockName)
	// 0600: the lock file carries no secret, but ~/.orun is a 0700 dir and we
	// keep every file in it owner-only for consistency.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open refresh lock %s: %w", path, err)
	}
	if err := flockExclusive(ctx, f); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &RefreshLock{f: f}, nil
}

// Release unlocks and closes the lock file. Safe to call on a nil lock.
func (l *RefreshLock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	unlockErr := flockUnlock(l.f)
	closeErr := l.f.Close()
	l.f = nil
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
