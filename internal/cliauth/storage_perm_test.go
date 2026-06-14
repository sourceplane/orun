package cliauth

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFileCredentialStore_RefusesWorldReadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not meaningful on Windows")
	}
	tmp := t.TempDir()
	userHomeDir = func() (string, error) { return tmp, nil }
	t.Cleanup(func() { userHomeDir = os.UserHomeDir })

	store := &fileCredentialStore{}
	if err := store.Save(&Credentials{AccessToken: "tok", BackendURL: "https://api.example.com"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	path := filepath.Join(tmp, ".orun", "credentials.json")

	// A 0600 file loads fine.
	if _, err := store.Load(); err != nil {
		t.Fatalf("Load with 0600: %v", err)
	}

	// Loosen to 0644 (world/group-readable) and expect a refusal with fix-it.
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod 0644: %v", err)
	}
	_, err := store.Load()
	if err == nil {
		t.Fatal("expected Load to refuse a world-readable credentials file")
	}
	if !strings.Contains(err.Error(), "chmod 600") {
		t.Errorf("error = %v, want a 'chmod 600' fix-it message", err)
	}

	// Group-readable only (0640) must also be refused.
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatalf("chmod 0640: %v", err)
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("expected Load to refuse a group-readable credentials file")
	}

	// Tightening back to 0600 restores access.
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("chmod 0600: %v", err)
	}
	if _, err := store.Load(); err != nil {
		t.Fatalf("Load after re-tightening to 0600: %v", err)
	}
}
