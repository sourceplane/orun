package sourcectx_test

import (
	"os"
	"os/exec"
	"path/filepath"
)

// execCommand wraps exec.Command so the tests don't import os/exec
// directly (keeps the file imports minimal and the production surface
// untouched).
func execCommand(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

// writeFileWithDir creates parent dirs as needed and writes content.
func writeFileWithDir(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}
