package cliauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	keychainService = "io.sourceplane.orun"
	keychainAccount = "cli-session"
)

var (
	userHomeDir = os.UserHomeDir
	lookPath    = exec.LookPath
	execCommand = exec.Command
)

// CredentialStore persists CLI session credentials.
type CredentialStore interface {
	Load() (*Credentials, error)
	Save(*Credentials) error
	Clear() error
}

// DefaultCredentialStore returns the standard keychain-or-file credential store.
func DefaultCredentialStore() CredentialStore {
	return &defaultCredentialStore{
		keychain: &keychainCredentialStore{},
		file:     &fileCredentialStore{},
	}
}

func configPath() (string, error) {
	base, err := configBaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "config.yaml"), nil
}

func credentialsPath() (string, error) {
	base, err := configBaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "credentials.json"), nil
}

func configBaseDir() (string, error) {
	home, err := userHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine user home directory: %w", err)
	}
	return filepath.Join(home, ".orun"), nil
}

func ensureConfigDir() (string, error) {
	base, err := configBaseDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w", base, err)
	}
	return base, nil
}

// LoadConfig reads ~/.orun/config.yaml.
func LoadConfig() (*Config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &cfg, nil
}

// SaveConfig writes ~/.orun/config.yaml.
func SaveConfig(cfg *Config) error {
	if cfg == nil {
		cfg = &Config{}
	}
	if _, err := ensureConfigDir(); err != nil {
		return err
	}
	path, err := configPath()
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// SaveSession stores session credentials and updates the default backend URL.
func SaveSession(creds *Credentials) error {
	if creds == nil {
		return fmt.Errorf("missing credentials")
	}
	if err := DefaultCredentialStore().Save(creds); err != nil {
		return err
	}
	if strings.TrimSpace(creds.BackendURL) == "" {
		return nil
	}
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	cfg.Backend.URL = strings.TrimSpace(creds.BackendURL)
	return SaveConfig(cfg)
}

// LoadSession reads stored CLI session credentials.
func LoadSession() (*Credentials, error) {
	return DefaultCredentialStore().Load()
}

// ClearSession removes stored CLI session credentials.
func ClearSession() error {
	return DefaultCredentialStore().Clear()
}

// UpsertRepoLink stores or updates a repo link in ~/.orun/config.yaml.
func UpsertRepoLink(link RepoLink) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	updated := false
	for i := range cfg.Repos {
		if sameRepoLink(cfg.Repos[i], link) {
			cfg.Repos[i] = link
			updated = true
			break
		}
	}
	if !updated {
		cfg.Repos = append(cfg.Repos, link)
	}
	if strings.TrimSpace(link.BackendURL) != "" {
		cfg.Backend.URL = strings.TrimSpace(link.BackendURL)
	}
	return SaveConfig(cfg)
}

// FindRepoLink returns a stored repo link for the backend/remote/repo tuple.
func FindRepoLink(backendURL, gitRemote, repoFullName string) (*RepoLink, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	backendURL = strings.TrimSpace(backendURL)
	gitRemote = strings.TrimSpace(gitRemote)
	repoFullName = strings.TrimSpace(repoFullName)
	for i := range cfg.Repos {
		link := cfg.Repos[i]
		if backendURL != "" && !sameURL(link.BackendURL, backendURL) {
			continue
		}
		if repoFullName != "" && strings.EqualFold(link.RepoFullName, repoFullName) {
			return &link, nil
		}
		if gitRemote != "" && sameURL(link.GitRemote, gitRemote) {
			return &link, nil
		}
	}
	return nil, nil
}

func sameRepoLink(a, b RepoLink) bool {
	if strings.TrimSpace(a.RepoFullName) != "" && strings.EqualFold(a.RepoFullName, b.RepoFullName) && sameURL(a.BackendURL, b.BackendURL) {
		return true
	}
	return strings.TrimSpace(a.GitRemote) != "" && sameURL(a.GitRemote, b.GitRemote) && sameURL(a.BackendURL, b.BackendURL)
}

func sameURL(a, b string) bool {
	return strings.EqualFold(strings.TrimRight(strings.TrimSpace(a), "/"), strings.TrimRight(strings.TrimSpace(b), "/"))
}

type defaultCredentialStore struct {
	keychain *keychainCredentialStore
	file     *fileCredentialStore
}

func (s *defaultCredentialStore) Load() (*Credentials, error) {
	if s.keychain.available() {
		creds, err := s.keychain.Load()
		if err == nil {
			return creds, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	return s.file.Load()
}

func (s *defaultCredentialStore) Save(creds *Credentials) error {
	if s.keychain.available() {
		if err := s.keychain.Save(creds); err == nil {
			_ = s.file.Clear()
			return nil
		}
	}
	return s.file.Save(creds)
}

func (s *defaultCredentialStore) Clear() error {
	var errs []error
	if s.keychain.available() {
		if err := s.keychain.Clear(); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	if err := s.file.Clear(); err != nil && !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		return nil
	}
	return errs[0]
}

type keychainCredentialStore struct{}

func (s *keychainCredentialStore) available() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	_, err := lookPath("security")
	return err == nil
}

func (s *keychainCredentialStore) Load() (*Credentials, error) {
	cmd := execCommand("security", "find-generic-password", "-a", keychainAccount, "-s", keychainService, "-w")
	output, err := cmd.Output()
	if err != nil {
		return nil, os.ErrNotExist
	}
	var creds Credentials
	if err := json.Unmarshal(output, &creds); err != nil {
		return nil, fmt.Errorf("parse credentials from keychain: %w", err)
	}
	return &creds, nil
}

func (s *keychainCredentialStore) Save(creds *Credentials) error {
	data, err := json.Marshal(creds)
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}
	cmd := execCommand("security", "add-generic-password", "-a", keychainAccount, "-s", keychainService, "-U", "-w", string(data))
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("save credentials to keychain: %v (%s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (s *keychainCredentialStore) Clear() error {
	cmd := execCommand("security", "delete-generic-password", "-a", keychainAccount, "-s", keychainService)
	if output, err := cmd.CombinedOutput(); err != nil {
		if strings.Contains(strings.ToLower(string(output)), "could not be found") {
			return os.ErrNotExist
		}
		return fmt.Errorf("delete credentials from keychain: %v (%s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}

type fileCredentialStore struct{}

func (s *fileCredentialStore) Load() (*Credentials, error) {
	path, err := credentialsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &creds, nil
}

func (s *fileCredentialStore) Save(creds *Credentials) error {
	if _, err := ensureConfigDir(); err != nil {
		return err
	}
	path, err := credentialsPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return os.Chmod(path, 0o600)
}

func (s *fileCredentialStore) Clear() error {
	path, err := credentialsPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return os.ErrNotExist
		}
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}
