package github

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/sourceplane/orun/internal/artifactstore"
	"github.com/sourceplane/orun/internal/runbundle"
)

//go:embed helper/package.json
var helperPackageJSON []byte

//go:embed helper/upload.mjs
var helperUploadMJS []byte

var (
	helperDir string
	helperMu  sync.Mutex
)

// DefaultRetentionDays is the default artifact retention in days.
const DefaultRetentionDays = 14

// IsGitHubActions returns true when running inside a GitHub Actions runner.
func IsGitHubActions() bool {
	return os.Getenv("GITHUB_ACTIONS") == "true"
}

// Upload uploads a shard as a GitHub Actions artifact using the embedded
// @actions/artifact Node.js helper. Only works inside GitHub Actions.
func (c *Client) Upload(ctx context.Context, shard *runbundle.Shard) (*artifactstore.UploadResult, error) {
	if !IsGitHubActions() {
		return nil, fmt.Errorf("github upload only supported inside GitHub Actions")
	}

	if shard == nil {
		return nil, fmt.Errorf("shard is required")
	}
	if shard.Dir == "" {
		return nil, fmt.Errorf("shard directory is required")
	}

	name := runbundle.ArtifactName(shard.ExecID, shard.Role, shard.Suffix, shard.Status)

	// Ensure the helper is extracted to a temp directory
	hd, err := ensureHelperExtracted(ctx)
	if err != nil {
		return nil, fmt.Errorf("extract upload helper: %w", err)
	}

	retentionDays := retentionDaysFromEnv()

	cmd := exec.CommandContext(ctx, "node", "upload.mjs", shard.Dir, name, strconv.Itoa(retentionDays))
	cmd.Dir = hd

	// Pass only the runtime token and results URL — not the full env
	cmd.Env = []string{
		"ACTIONS_RUNTIME_TOKEN=" + os.Getenv("ACTIONS_RUNTIME_TOKEN"),
		"ACTIONS_RESULTS_URL=" + os.Getenv("ACTIONS_RESULTS_URL"),
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("upload helper failed: %w\noutput: %s", err, strings.TrimSpace(string(output)))
	}

	var result artifactstore.UploadResult
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("parse upload helper output: %w\noutput: %s", err, string(output))
	}

	if result.ID == "" {
		return nil, fmt.Errorf("upload helper returned empty id\noutput: %s", string(output))
	}

	return &result, nil
}

// ensureHelperExtracted extracts the embedded helper files to a temp directory.
// The result is cached so extraction only happens once per process lifetime.
func ensureHelperExtracted(ctx context.Context) (string, error) {
	helperMu.Lock()
	defer helperMu.Unlock()

	if helperDir != "" {
		return helperDir, nil
	}

	dir, err := os.MkdirTemp("", "orun-github-upload-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}

	// Write package.json
	if err := os.WriteFile(filepath.Join(dir, "package.json"), helperPackageJSON, 0644); err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("write package.json: %w", err)
	}

	// Write upload.mjs
	if err := os.WriteFile(filepath.Join(dir, "upload.mjs"), helperUploadMJS, 0644); err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("write upload.mjs: %w", err)
	}

	// Install dependencies
	installCmd := exec.CommandContext(ctx, "npm", "install", "--no-package-lock", "--no-audit", "--no-fund")
	installCmd.Dir = dir
	if out, err := installCmd.CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("npm install failed: %w\noutput: %s", err, string(out))
	}

	helperDir = dir
	return helperDir, nil
}

// retentionDaysFromEnv reads ORUN_ARTIFACT_RETENTION_DAYS env var, defaulting to 14.
func retentionDaysFromEnv() int {
	s := os.Getenv("ORUN_ARTIFACT_RETENTION_DAYS")
	if s == "" {
		return DefaultRetentionDays
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return DefaultRetentionDays
	}
	return n
}