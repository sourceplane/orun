package cliauth

import (
	"os"
	"os/exec"
	"testing"
)

func TestResolvedBackendURL_PrefersCloud(t *testing.T) {
	cfg := &Config{
		Cloud:   CloudConfig{URL: "https://cloud.example.com"},
		Backend: BackendConfig{URL: "https://legacy.example.com"},
	}
	if got := cfg.ResolvedBackendURL(); got != "https://cloud.example.com" {
		t.Errorf("ResolvedBackendURL = %q, want cloud", got)
	}
	if cfg.UsesDeprecatedBackendURL() {
		t.Error("UsesDeprecatedBackendURL should be false when cloud.url set")
	}
}

func TestResolvedBackendURL_FallsBackToBackend(t *testing.T) {
	cfg := &Config{Backend: BackendConfig{URL: "https://legacy.example.com"}}
	if got := cfg.ResolvedBackendURL(); got != "https://legacy.example.com" {
		t.Errorf("ResolvedBackendURL = %q, want legacy", got)
	}
	if !cfg.UsesDeprecatedBackendURL() {
		t.Error("UsesDeprecatedBackendURL should be true when only backend.url set")
	}
}

func TestSetConfigBackendURL_KeepsAliasInSync(t *testing.T) {
	// When backend.url is already present, writes keep it in sync with cloud.url.
	cfg := &Config{Backend: BackendConfig{URL: "https://old.example.com"}}
	setConfigBackendURL(cfg, "https://new.example.com")
	if cfg.Cloud.URL != "https://new.example.com" {
		t.Errorf("cloud.url = %q, want new", cfg.Cloud.URL)
	}
	if cfg.Backend.URL != "https://new.example.com" {
		t.Errorf("backend.url alias = %q, want kept in sync", cfg.Backend.URL)
	}

	// When backend.url is absent, only cloud.url is written (no new alias).
	cfg2 := &Config{}
	setConfigBackendURL(cfg2, "https://new.example.com")
	if cfg2.Cloud.URL != "https://new.example.com" {
		t.Errorf("cloud.url = %q, want new", cfg2.Cloud.URL)
	}
	if cfg2.Backend.URL != "" {
		t.Errorf("backend.url should stay empty, got %q", cfg2.Backend.URL)
	}
}

func TestSaveSession_WritesCloudURL(t *testing.T) {
	tmp := t.TempDir()
	userHomeDir = func() (string, error) { return tmp, nil }
	lookPath = func(string) (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() {
		userHomeDir = os.UserHomeDir
		lookPath = exec.LookPath
	})

	if err := SaveSession(&Credentials{AccessToken: "tok", BackendURL: "https://api.example.com"}); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Cloud.URL != "https://api.example.com" {
		t.Errorf("cloud.url = %q, want api.example.com", cfg.Cloud.URL)
	}
}
