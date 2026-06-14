package cliauth

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// withFileStore points the credential store at a temp HOME and disables the
// macOS keychain (which would prompt and hang in tests).
func withFileStore(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	userHomeDir = func() (string, error) { return tmp, nil }
	lookPath = func(string) (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() {
		userHomeDir = os.UserHomeDir
		lookPath = exec.LookPath
	})
	return tmp
}

// writeRawCreds writes a credentials.json with arbitrary fields, simulating an
// older on-disk format.
func writeRawCreds(t *testing.T, home string, raw map[string]interface{}) string {
	t.Helper()
	dir := filepath.Join(home, ".orun")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(dir, "credentials.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestLoadSession_MigratesNamespacesToOrgs(t *testing.T) {
	home := withFileStore(t)
	path := writeRawCreds(t, home, map[string]interface{}{
		"accessToken":         "tok",
		"allowedNamespaceIds": []string{"org-a", "org-b"},
		"backendUrl":          "https://api.example.com",
	})

	creds, err := LoadSession()
	if err != nil {
		t.Fatalf("LoadSession error: %v", err)
	}
	if len(creds.Orgs) != 2 {
		t.Fatalf("Orgs = %+v, want 2 entries", creds.Orgs)
	}
	if creds.Orgs[0].ID != "org-a" || creds.Orgs[1].ID != "org-b" {
		t.Errorf("Orgs ids = %+v, want [org-a org-b]", creds.Orgs)
	}
	// allowedNamespaceIds must remain readable for OSS back-compat.
	if len(creds.AllowedNamespaceIDs) != 2 {
		t.Errorf("AllowedNamespaceIDs dropped: %+v", creds.AllowedNamespaceIDs)
	}

	// The file should have been rewritten once with orgs persisted.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var onDisk Credentials
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("unmarshal back: %v", err)
	}
	if len(onDisk.Orgs) != 2 {
		t.Errorf("rewrite did not persist orgs: %+v", onDisk.Orgs)
	}
}

func TestLoadSession_AlreadyMigratedNoChange(t *testing.T) {
	home := withFileStore(t)
	writeRawCreds(t, home, map[string]interface{}{
		"accessToken": "tok",
		"orgs":        []map[string]string{{"id": "org-x", "slug": "x", "role": "owner"}},
	})

	creds, err := LoadSession()
	if err != nil {
		t.Fatalf("LoadSession error: %v", err)
	}
	if len(creds.Orgs) != 1 || creds.Orgs[0].ID != "org-x" {
		t.Fatalf("Orgs = %+v, want [org-x]", creds.Orgs)
	}
	if creds.Orgs[0].Role != "owner" {
		t.Errorf("Role = %q, want owner", creds.Orgs[0].Role)
	}
}

func TestMigrateCredentialsOrgs_NoNamespaces(t *testing.T) {
	creds := &Credentials{AccessToken: "tok"}
	if migrateCredentialsOrgs(creds) {
		t.Error("expected no migration when there are no namespaces")
	}
	creds2 := &Credentials{Orgs: []OrgRef{{ID: "o"}}, AllowedNamespaceIDs: []string{"n"}}
	if migrateCredentialsOrgs(creds2) {
		t.Error("expected no migration when Orgs already populated")
	}
}

func TestSessionResponseToCredentials_SynthesizesOrgsFromNamespaces(t *testing.T) {
	resp := &SessionResponse{
		AccessToken:         "tok",
		AllowedNamespaceIDs: []string{"ns-1"},
	}
	creds := sessionResponseToCredentials(resp, "https://api.example.com")
	if len(creds.Orgs) != 1 || creds.Orgs[0].ID != "ns-1" {
		t.Errorf("Orgs = %+v, want synthesized [ns-1]", creds.Orgs)
	}
}

func TestSessionResponseToCredentials_PrefersExplicitOrgs(t *testing.T) {
	resp := &SessionResponse{
		AccessToken:         "tok",
		Orgs:                []OrgRef{{ID: "org-real", Slug: "acme", Role: "admin"}},
		AllowedNamespaceIDs: []string{"ns-legacy"},
	}
	creds := sessionResponseToCredentials(resp, "https://api.example.com")
	if len(creds.Orgs) != 1 || creds.Orgs[0].ID != "org-real" {
		t.Errorf("Orgs = %+v, want explicit [org-real]", creds.Orgs)
	}
}
