package cliauth

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFileCredentialStoreRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	userHomeDir = func() (string, error) { return tmp, nil }
	t.Cleanup(func() { userHomeDir = os.UserHomeDir })

	store := &fileCredentialStore{}
	creds := &Credentials{
		AccessToken:        "access",
		AccessTokenExpiry:  "2026-01-01T00:00:00Z",
		RefreshToken:       "refresh",
		RefreshTokenExpiry: "2026-02-01T00:00:00Z",
		GitHubLogin:        "octocat",
		BackendURL:         "https://api.example.com",
	}
	if err := store.Save(creds); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.AccessToken != creds.AccessToken || loaded.RefreshToken != creds.RefreshToken {
		t.Fatalf("loaded credentials = %+v, want %+v", loaded, creds)
	}
	path := filepath.Join(tmp, ".orun", "credentials.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", path, err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("credentials mode = %o, want 0600", info.Mode().Perm())
	}
}

func TestUpsertRepoLinkAndFindRepoLink(t *testing.T) {
	tmp := t.TempDir()
	userHomeDir = func() (string, error) { return tmp, nil }
	t.Cleanup(func() { userHomeDir = os.UserHomeDir })

	err := UpsertRepoLink(RepoLink{
		BackendURL:   "https://api.example.com",
		GitRemote:    "git@github.com:sourceplane/orun.git",
		RepoFullName: "sourceplane/orun",
		NamespaceID:  "123",
		LinkedAt:     "2026-05-07T10:00:00Z",
	})
	if err != nil {
		t.Fatalf("UpsertRepoLink() error = %v", err)
	}
	link, err := FindRepoLink("https://api.example.com", "git@github.com:sourceplane/orun.git", "sourceplane/orun")
	if err != nil {
		t.Fatalf("FindRepoLink() error = %v", err)
	}
	if link == nil || link.NamespaceID != "123" {
		t.Fatalf("FindRepoLink() = %+v, want namespace 123", link)
	}

	err = UpsertRepoLink(RepoLink{
		BackendURL:   "https://api.example.com",
		GitRemote:    "git@github.com:sourceplane/orun.git",
		RepoFullName: "sourceplane/orun",
		NamespaceID:  "456",
		LinkedAt:     "2026-05-07T10:05:00Z",
	})
	if err != nil {
		t.Fatalf("UpsertRepoLink(update) error = %v", err)
	}
	link, err = FindRepoLink("https://api.example.com", "git@github.com:sourceplane/orun.git", "sourceplane/orun")
	if err != nil {
		t.Fatalf("FindRepoLink(update) error = %v", err)
	}
	if link == nil || link.NamespaceID != "456" {
		t.Fatalf("updated link = %+v, want namespace 456", link)
	}
}

func TestDefaultCredentialStoreFallsBackToFile(t *testing.T) {
	tmp := t.TempDir()
	userHomeDir = func() (string, error) { return tmp, nil }
	lookPath = func(file string) (string, error) { return "", errors.New("missing") }
	t.Cleanup(func() {
		userHomeDir = os.UserHomeDir
		lookPath = exec.LookPath
	})

	store := DefaultCredentialStore()
	creds := &Credentials{AccessToken: "access", BackendURL: "https://api.example.com"}
	if err := store.Save(creds); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.AccessToken != "access" {
		t.Fatalf("AccessToken = %q, want access", loaded.AccessToken)
	}
}
