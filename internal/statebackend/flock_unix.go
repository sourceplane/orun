//go:build unix

package statebackend

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

func (fl *FileLock) openFD() error {
	if fl.fd >= 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(fl.path), 0755); err != nil {
		return fmt.Errorf("creating lock directory: %w", err)
	}
	fd, err := unix.Open(fl.path, unix.O_CREAT|unix.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("opening lock file %s: %w", fl.path, err)
	}
	fl.fd = fd
	return nil
}

func (fl *FileLock) lockImpl(ctx context.Context) error {
	if err := fl.openFD(); err != nil {
		return err
	}
	for {
		err := unix.Flock(fl.fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			unix.Close(fl.fd)
			fl.fd = -1
			return fmt.Errorf("acquiring lock %s: %w", fl.path, ctx.Err())
		default:
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (fl *FileLock) tryLockImpl() (bool, error) {
	if err := fl.openFD(); err != nil {
		return false, err
	}
	err := unix.Flock(fl.fd, unix.LOCK_EX|unix.LOCK_NB)
	if err != nil {
		unix.Close(fl.fd)
		fl.fd = -1
		return false, nil
	}
	return true, nil
}

func (fl *FileLock) unlockImpl() error {
	if fl.fd < 0 {
		return nil
	}
	unix.Flock(fl.fd, unix.LOCK_UN)
	unix.Close(fl.fd)
	fl.fd = -1
	return nil
}
