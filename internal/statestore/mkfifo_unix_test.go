//go:build !windows

package statestore_test

import (
	"syscall"
	"testing"
)

func mkfifoOrSkip(t *testing.T, path string) error {
	t.Helper()
	return syscall.Mkfifo(path, 0o600)
}
