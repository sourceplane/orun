package runbundle

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/state"
)

const (
	schemaVersion      = "1.0.0"
	manifestAPIVersion = "orun.io/v1alpha1"
	manifestKind       = "RunBundleShard"
)

// WritePlanShardOptions configures plan shard creation.
type WritePlanShardOptions struct {
	ExecID    string
	Plan      *model.Plan
	Source    ShardSource
	OutputDir string
}

// WriteJobShardOptions configures job shard creation.
type WriteJobShardOptions struct {
	ExecID    string
	PlanID    string
	JobUID    string
	JobID     string
	Component string
	Env       string
	Profile   string
	Status    string
	Source    ShardSource
	State     *state.JobState
	LogsDir   string
	OutputDir string
}

// Shard represents a written shard on disk.
type Shard struct {
	Dir      string
	ExecID   string
	Role     ShardRole
	Suffix   string
	Status   string
	Manifest *RunBundleShardManifest
}

// WritePlanShard writes a plan shard directory and returns the shard reference.
func WritePlanShard(ctx context.Context, opts WritePlanShardOptions) (*Shard, error) {
	if opts.Plan == nil {
		return nil, fmt.Errorf("plan is required")
	}
	if opts.OutputDir == "" {
		return nil, fmt.Errorf("output directory is required")
	}

	shardDir := opts.OutputDir
	planChecksum := checksumShort(opts.Plan.Metadata.Checksum)
	if planChecksum == "" {
		planChecksum = "plan"
	}

	if err := os.MkdirAll(shardDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create shard dir: %w", err)
	}

	// Build complete file list upfront (including checksums reference).
	manifestFiles := map[string]string{
		"manifest": "manifest.json",
		"plan":     "plan.json",
		"checksums": "checksums.json",
	}
	if opts.Plan.Metadata.Trigger != nil {
		manifestFiles["trigger"] = "trigger.json"
	}
	if opts.Source.SHA != "" || opts.Source.Ref != "" {
		manifestFiles["git"] = "git.json"
	}

	manifest := &RunBundleShardManifest{
		APIVersion:    manifestAPIVersion,
		Kind:          manifestKind,
		SchemaVersion: schemaVersion,
		Role:          ShardRolePlan,
		ExecID:        opts.ExecID,
		PlanID:        planChecksum,
		Source:        opts.Source,
		Files:         manifestFiles,
	}

	// Write plan
	if err := writeJSON(filepath.Join(shardDir, "plan.json"), opts.Plan); err != nil {
		return nil, fmt.Errorf("failed to write plan: %w", err)
	}

	// Write trigger info if available
	if opts.Plan.Metadata.Trigger != nil {
		if err := writeJSON(filepath.Join(shardDir, "trigger.json"), opts.Plan.Metadata.Trigger); err != nil {
			return nil, fmt.Errorf("failed to write trigger: %w", err)
		}
	}

	// Write git info from source
	if opts.Source.SHA != "" || opts.Source.Ref != "" {
		gitInfo := map[string]string{
			"sha": opts.Source.SHA,
			"ref": opts.Source.Ref,
		}
		if err := writeJSON(filepath.Join(shardDir, "git.json"), gitInfo); err != nil {
			return nil, fmt.Errorf("failed to write git info: %w", err)
		}
	}

	// Write manifest with complete file list
	if err := writeJSON(filepath.Join(shardDir, "manifest.json"), manifest); err != nil {
		return nil, fmt.Errorf("failed to write manifest: %w", err)
	}

	// Compute checksums of all files (including manifest.json, excluding checksums.json)
	checksums, err := computeChecksums(shardDir, manifestFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to compute checksums: %w", err)
	}
	if err := writeJSON(filepath.Join(shardDir, "checksums.json"), checksums); err != nil {
		return nil, fmt.Errorf("failed to write checksums: %w", err)
	}

	return &Shard{
		Dir:      shardDir,
		ExecID:   opts.ExecID,
		Role:     ShardRolePlan,
		Suffix:   planChecksum,
		Status:   "created",
		Manifest: manifest,
	}, nil
}

// WriteJobShard writes a job shard directory and returns the shard reference.
func WriteJobShard(ctx context.Context, opts WriteJobShardOptions) (*Shard, error) {
	if opts.State == nil {
		return nil, fmt.Errorf("job state is required")
	}
	if opts.OutputDir == "" {
		return nil, fmt.Errorf("output directory is required")
	}

	shardDir := opts.OutputDir
	hasSteps := len(opts.State.Steps) > 0

	if err := os.MkdirAll(shardDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create shard dir: %w", err)
	}

	// Build complete file list upfront.
	manifestFiles := map[string]string{
		"manifest": "manifest.json",
		"job":      "job.json",
		"state":    "state.json",
		"checksums": "checksums.json",
	}
	if hasSteps {
		manifestFiles["steps"] = "steps.jsonl"
	}

	// Check for logs before writing manifest (to include in Files map).
	var hasLogs bool
	if opts.LogsDir != "" {
		entries, err := os.ReadDir(opts.LogsDir)
		if err == nil && len(entries) > 0 {
			hasLogs = true
			manifestFiles["logs"] = "logs/"
		}
	}

	manifest := &RunBundleShardManifest{
		APIVersion:    manifestAPIVersion,
		Kind:          manifestKind,
		SchemaVersion: schemaVersion,
		Role:          ShardRoleJob,
		ExecID:        opts.ExecID,
		PlanID:        opts.PlanID,
		JobUID:        opts.JobUID,
		JobID:         opts.JobID,
		Component:     opts.Component,
		Environment:   opts.Env,
		Profile:       opts.Profile,
		Status:        opts.Status,
		StartedAt:     opts.State.StartedAt,
		FinishedAt:    opts.State.FinishedAt,
		Source:        opts.Source,
		Files:         manifestFiles,
	}

	// Write job info
	jobInfo := map[string]string{
		"jobUid":      opts.JobUID,
		"jobId":       opts.JobID,
		"component":   opts.Component,
		"environment": opts.Env,
		"profile":     opts.Profile,
		"status":      opts.Status,
	}
	if err := writeJSON(filepath.Join(shardDir, "job.json"), jobInfo); err != nil {
		return nil, fmt.Errorf("failed to write job info: %w", err)
	}

	// Write state
	if err := writeJSON(filepath.Join(shardDir, "state.json"), opts.State); err != nil {
		return nil, fmt.Errorf("failed to write state: %w", err)
	}

	// Write steps as JSONL
	if hasSteps {
		sf, err := os.Create(filepath.Join(shardDir, "steps.jsonl"))
		if err != nil {
			return nil, fmt.Errorf("failed to create steps file: %w", err)
		}
		defer sf.Close()

		enc := json.NewEncoder(sf)
		for stepID, stepStatus := range opts.State.Steps {
			entry := map[string]string{
				"stepId": stepID,
				"status": stepStatus,
			}
			if err := enc.Encode(entry); err != nil {
				return nil, fmt.Errorf("failed to write step entry: %w", err)
			}
		}
	}

	// Copy logs if available
	if hasLogs {
		logDir := filepath.Join(shardDir, "logs")
		logEntries, err := copyLogs(opts.LogsDir, logDir)
		if err != nil {
			return nil, fmt.Errorf("failed to copy logs: %w", err)
		}
		for _, entry := range logEntries {
			manifest.Files["log:"+entry.LogicalName] = entry.RelativePath
		}
	}

	// Write manifest with complete file list
	if err := writeJSON(filepath.Join(shardDir, "manifest.json"), manifest); err != nil {
		return nil, fmt.Errorf("failed to write manifest: %w", err)
	}

	// Compute checksums of all files (including manifest.json, excluding checksums.json)
	checksums, err := computeChecksums(shardDir, manifestFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to compute checksums: %w", err)
	}
	if err := writeJSON(filepath.Join(shardDir, "checksums.json"), checksums); err != nil {
		return nil, fmt.Errorf("failed to write checksums: %w", err)
	}

	suffix := opts.JobUID
	if suffix == "" {
		suffix = opts.JobID
	}

	return &Shard{
		Dir:      shardDir,
		ExecID:   opts.ExecID,
		Role:     ShardRoleJob,
		Suffix:   suffix,
		Status:   opts.Status,
		Manifest: manifest,
	}, nil
}

// logEntry tracks a copied log file for the manifest.
type logEntry struct {
	LogicalName  string
	RelativePath string
}

// copyLogs copies step logs from source to destination directory.
func copyLogs(srcDir, dstDir string) ([]logEntry, error) {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read logs dir %s: %w", srcDir, err)
	}

	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create logs dir: %w", err)
	}

	var logEntries []logEntry
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".log" {
			continue
		}
		src := filepath.Join(srcDir, entry.Name())
		dst := filepath.Join(dstDir, entry.Name())

		data, err := os.ReadFile(src)
		if err != nil {
			return nil, fmt.Errorf("failed to read log %s: %w", src, err)
		}
		if err := os.WriteFile(dst, data, 0644); err != nil {
			return nil, fmt.Errorf("failed to write log %s: %w", dst, err)
		}

		stepID := entry.Name()[:len(entry.Name())-len(".log")]
		logEntries = append(logEntries, logEntry{
			LogicalName:  stepID,
			RelativePath: filepath.Join("logs", entry.Name()),
		})
	}

	return logEntries, nil
}

// computeChecksums computes SHA256 digests for tracked files.
func computeChecksums(shardDir string, files map[string]string) (*Checksums, error) {
	checksums := &Checksums{
		Algorithm: "sha256",
		Files:     make(map[string]string),
	}

	for logical, relPath := range files {
		if logical == "checksums" || logical == "logs" {
			continue
		}
		fullPath := filepath.Join(shardDir, relPath)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s for checksum: %w", relPath, err)
		}
		h := sha256.Sum256(data)
		checksums.Files[relPath] = fmt.Sprintf("%x", h)
	}

	return checksums, nil
}

// writeJSON marshals v as indented JSON and writes to path.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// checksumShort extracts the short form of a plan checksum.
func checksumShort(checksum string) string {
	if len(checksum) > 12 {
		return checksum[:12]
	}
	return checksum
}