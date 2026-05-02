//go:build !unix

package statebackend

import "context"

func (fl *FileLock) lockImpl(_ context.Context) error {
	return nil
}

func (fl *FileLock) tryLockImpl() (bool, error) {
	return true, nil
}

func (fl *FileLock) unlockImpl() error {
	return nil
}
