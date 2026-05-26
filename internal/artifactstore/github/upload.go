package github

import (
	"archive/zip"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

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

// UploadPollInterval is how long to wait between verification polls.
var UploadPollInterval = 2 * time.Second

// UploadPollTimeout is how long to wait for artifact verification.
var UploadPollTimeout = 30 * time.Second

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

	if os.Getenv("ACTIONS_RUNTIME_TOKEN") == "" {
		return nil, fmt.Errorf("ACTIONS_RUNTIME_TOKEN not available; @actions/artifact requires it")
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

	// Inherit full environment — @actions/artifact needs ACTIONS_RUNTIME_TOKEN,
	// ACTIONS_RESULTS_URL, GITHUB_RUN_ID, GITHUB_WORKSPACE, and other runner vars.
	cmd.Env = os.Environ()

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

// UploadShard is a high-level orchestrator that packages a shard directory,
// uploads it as a named artifact, and verifies the artifact exists post-upload.
//
// When ACTIONS_RUNTIME_TOKEN is available (native GHA runtime), uses the
// @actions/artifact Node.js helper for direct upload.
// Otherwise, uses the GitHub REST API with GITHUB_TOKEN for artifact upload.
//
// Returns the upload result with artifact ID, name, and size.
func (c *Client) UploadShard(ctx context.Context, shard *runbundle.Shard) (*artifactstore.UploadResult, error) {
	if shard == nil {
		return nil, fmt.Errorf("shard is required")
	}
	if shard.Dir == "" {
		return nil, fmt.Errorf("shard directory is required")
	}

	name := runbundle.ArtifactName(shard.ExecID, shard.Role, shard.Suffix, shard.Status)

	// Check if runtime token is available for GHA upload.
	// GITHUB_ACTIONS=true is set in the plan job, but ACTIONS_RUNTIME_TOKEN
	// is only available during actual job step runtime, not arbitrary shell commands.
	if IsGitHubActions() && os.Getenv("ACTIONS_RUNTIME_TOKEN") != "" {
		result, err := c.UploadWithRetry(ctx, shard)
		if err != nil {
			return nil, fmt.Errorf("gha upload: %w", err)
		}

		// Verify the artifact exists
		if err := c.VerifyArtifactExists(ctx, 0, name); err != nil {
			return result, fmt.Errorf("upload succeeded but verification failed: %w", err)
		}

		return result, nil
	}

	// Use REST API (no runtime token or outside GHA)
	return c.uploadViaAPI(ctx, shard, name)
}

// UploadWithRetry wraps Upload with retry logic for transient failures.
// Retries up to 3 times with exponential backoff on the Node.js helper execution.
func (c *Client) UploadWithRetry(ctx context.Context, shard *runbundle.Shard) (*artifactstore.UploadResult, error) {
	if shard == nil {
		return nil, fmt.Errorf("shard is required")
	}

	cfg := c.retryConfig
	if cfg.MaxRetries == 0 {
		cfg = DefaultRetryConfig
	}

	var lastErr error
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("upload cancelled before retry %d: %w", attempt, err)
			}
			delay := backoffDuration(attempt, cfg)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, fmt.Errorf("upload cancelled during retry delay %d: %w", attempt, ctx.Err())
			}
		}

		result, err := c.Upload(ctx, shard)
		if err == nil {
			return result, nil
		}

		lastErr = err

		// Don't retry non-retryable errors
		if !isUploadRetryable(err) {
			return nil, fmt.Errorf("non-retryable upload error: %w", err)
		}
	}

	return nil, fmt.Errorf("upload failed after %d retries: %w", cfg.MaxRetries, lastErr)
}

// uploadViaAPI uploads a shard directory as a GitHub artifact using the
// GitHub REST API. This works with a regular GITHUB_TOKEN/GH_TOKEN and
// does not require running inside a GitHub Actions runner.
//
// The endpoint differs from the GHA runtime upload:
//   - We create a signed upload URL via the GitHub artifacts API
//   - Upload the zip content to that URL
//   - Finalize the artifact
func (c *Client) uploadViaAPI(ctx context.Context, shard *runbundle.Shard, name string) (*artifactstore.UploadResult, error) {
	// Package the shard directory into a zip
	zipData, _, err := PackageShardAsZip(shard.Dir)
	if err != nil {
		return nil, fmt.Errorf("package shard: %w", err)
	}

	// Determine the run ID from the exec ID or source
	runID, err := resolveRunIDFromShard(shard)
	if err != nil {
		return nil, fmt.Errorf("resolve run id from shard: %w", err)
	}

	// Step 1: Create the artifact on GitHub and get an upload URL
	// POST /repos/{owner}/{repo}/actions/runs/{run_id}/artifacts
	createURL := c.apiURL(fmt.Sprintf("/repos/%s/actions/runs/%d/artifacts", c.repo, runID))

	createPayload := struct {
		Name         string `json:"name"`
		Size         int64  `json:"size"`
		RetentionDays int   `json:"retention_days"`
	}{
		Name:         name,
		Size:         int64(len(zipData)),
		RetentionDays: retentionDaysFromEnv(),
	}

	payloadBytes, _ := json.Marshal(createPayload)
	req, err := c.newPostRequest(ctx, createURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("create artifact request: %w", err)
	}

	resp, err := c.doRequest(req)
	if err != nil {
		return nil, fmt.Errorf("create artifact: %w", err)
	}
	defer resp.Body.Close()

	var createResult struct {
		ID         int64  `json:"id"`
		Name       string `json:"name"`
		Size       int64  `json:"size"`
		UploadURL  string `json:"upload_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&createResult); err != nil {
		return nil, fmt.Errorf("decode create response: %w", err)
	}

	// Step 2: Upload the zip content to the signed upload URL
	putReq, err := c.newPutRequest(ctx, createResult.UploadURL, "application/zip", bytes.NewReader(zipData))
	if err != nil {
		return nil, fmt.Errorf("create upload request: %w", err)
	}

	uploadResp, err := c.doRequest(putReq)
	if err != nil {
		return nil, fmt.Errorf("upload artifact content: %w", err)
	}
	defer uploadResp.Body.Close()

	// Step 3: Verify the artifact is accessible
	if err := c.VerifyArtifactExists(ctx, runID, name); err != nil {
		return &artifactstore.UploadResult{
			ID:   fmt.Sprintf("%d", createResult.ID),
			Name: name,
			Size: int64(len(zipData)),
		}, fmt.Errorf("upload succeeded but verification failed: %w", err)
	}

	return &artifactstore.UploadResult{
		ID:     fmt.Sprintf("%d", createResult.ID),
		Name:   name,
		Size:   int64(len(zipData)),
		Digest: "", // GitHub doesn't return a digest via the REST API
	}, nil
}

// PackageShardAsZip packages a shard directory into an in-memory zip archive.
// Returns the zip bytes and the total uncompressed size.
// Verifies that required manifest files exist in the shard directory.
func PackageShardAsZip(shardDir string) ([]byte, int64, error) {
	if shardDir == "" {
		return nil, 0, fmt.Errorf("shard directory is required")
	}

	// Verify the shard directory exists and contains at least a manifest
	manifestPath := filepath.Join(shardDir, "manifest.json")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		return nil, 0, fmt.Errorf("shard directory %s does not contain manifest.json", shardDir)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	var totalSize int64
	err := filepath.Walk(shardDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		// Compute relative path within the zip
		relPath, err := filepath.Rel(shardDir, path)
		if err != nil {
			return fmt.Errorf("compute relative path: %w", err)
		}

		// Normalize to forward slashes for cross-platform compatibility
		relPath = filepath.ToSlash(relPath)

		// Path traversal defense
		if strings.HasPrefix(relPath, "..") || strings.HasPrefix(relPath, "/") {
			return fmt.Errorf("path traversal detected: %q", relPath)
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return fmt.Errorf("create zip header for %s: %w", relPath, err)
		}
		header.Name = relPath
		header.Method = zip.Deflate

		w, err := zw.CreateHeader(header)
		if err != nil {
			return fmt.Errorf("create zip entry for %s: %w", relPath, err)
		}

		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open %s: %w", path, err)
		}
		defer f.Close()

		written, err := io.Copy(w, f)
		if err != nil {
			return fmt.Errorf("write %s to zip: %w", path, err)
		}
		totalSize += written

		return nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("walk shard directory: %w", err)
	}

	if err := zw.Close(); err != nil {
		return nil, 0, fmt.Errorf("finalize zip: %w", err)
	}

	return buf.Bytes(), totalSize, nil
}

// VerifyArtifactExists checks that an artifact with the given name exists
// for the specified workflow run. It polls the ListArtifacts endpoint until
// the artifact is found or the timeout is reached.
//
// If runID is 0, all runs containing the artifact will be searched.
// Uses the client's retry configuration for the underlying API calls.
func (c *Client) VerifyArtifactExists(ctx context.Context, runID int64, name string) error {
	if name == "" {
		return fmt.Errorf("artifact name is required")
	}

	deadline := time.Now().Add(UploadPollTimeout)

	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("verification cancelled: %w", err)
		}

		if runID > 0 {
			shards, err := c.ListOrunArtifacts(ctx, runID)
			if err != nil {
				// Retry on transient errors
				time.Sleep(UploadPollInterval)
				continue
			}
			for _, s := range shards {
				if s.Name == name {
					return nil
				}
			}
		} else {
			// Search across runs (up to the last 10)
			runs, err := c.ListWorkflowRuns(ctx, ListRunOptions{PerPage: 10})
			if err != nil {
				time.Sleep(UploadPollInterval)
				continue
			}
			for _, run := range runs {
				shards, err := c.ListOrunArtifacts(ctx, run.ID)
				if err != nil {
					continue
				}
				for _, s := range shards {
					if s.Name == name {
						return nil
					}
				}
			}
		}

		select {
		case <-time.After(UploadPollInterval):
		case <-ctx.Done():
			return fmt.Errorf("verification cancelled: %w", ctx.Err())
		}
	}

	return fmt.Errorf("artifact %q not found after polling for %v", name, UploadPollTimeout)
}

// UploadRunResultArtifact packages and uploads a run result bundle as a
// named GitHub Actions artifact. This is the primary entry point for
// uploading structured run results from CI pipelines.
//
// The shard is first written to a temporary directory using the runbundle
// writer, then packaged and uploaded as a named artifact.
func (c *Client) UploadRunResultArtifact(ctx context.Context, shard *runbundle.Shard) (*artifactstore.UploadResult, error) {
	if shard == nil {
		return nil, fmt.Errorf("shard is required")
	}
	if shard.Dir == "" {
		return nil, fmt.Errorf("shard directory is required")
	}

	return c.UploadShard(ctx, shard)
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

// resolveRunIDFromShard extracts the GitHub run ID from a shard's exec ID
// (format: gh-{run_id}-{attempt}-{sha}) or source metadata.
func resolveRunIDFromShard(shard *runbundle.Shard) (int64, error) {
	// Try exec ID first (format: gh-{run_id}-...)
	if shard.ExecID != "" {
		parts := strings.SplitN(shard.ExecID, "-", 4)
		if len(parts) >= 4 && parts[0] == "gh" {
			runID, err := strconv.ParseInt(parts[1], 10, 64)
			if err == nil {
				return runID, nil
			}
		}
	}

	// Try source metadata
	if shard.Manifest != nil {
		source := shard.Manifest.Source
		if source.RunID != "" {
			runID, err := strconv.ParseInt(source.RunID, 10, 64)
			if err == nil {
				return runID, nil
			}
		}
	}

	return 0, fmt.Errorf("unable to determine GitHub run ID from shard; exec ID and source metadata are missing")
}

// isUploadRetryable returns true if the error from an upload attempt
// should trigger a retry (transient network issue, timeout, etc.).
func isUploadRetryable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())

	// Network-level retryable errors
	if IsRetryableError(err) {
		return true
	}

	// Node.js helper specific retryable errors
	retryablePhrases := []string{
		"request timeout",
		"connection timeout",
		"artifact upload failed",
		"network error",
		"econnreset",
		"econnrefused",
		"etimedout",
		"enotfound",
		"the operation timed out",
	}
	for _, phrase := range retryablePhrases {
		if strings.Contains(msg, phrase) {
			return true
		}
	}

	return false
}

// normalizeZipPath normalizes a file path within a zip archive to use
// forward slashes and removes any leading separators.
func normalizeZipPath(path string) string {
	normalized := strings.ReplaceAll(path, "\\", "/")
	normalized = strings.TrimPrefix(normalized, "/")
	normalized = strings.TrimPrefix(normalized, "./")
	return normalized
}