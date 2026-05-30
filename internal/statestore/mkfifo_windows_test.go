//go:build windows

package statestore_test

import (
	"errors"
	"testing"
)

func mkfifoOrSkip(t *testing.T, path string) error {
	t.Helper()
	return errors.New("mkfifo unsupported on windows")
}
