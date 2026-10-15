//go:build !windows

package fslock

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// tryLockExclusive takes flock LOCK_EX without waiting; false when another
// open file description (another process, or another open in this one)
// holds it.
func tryLockExclusive(f *os.File) (bool, error) {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, unix.EWOULDBLOCK) {
		return false, nil
	}
	return false, err
}

func unlock(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
