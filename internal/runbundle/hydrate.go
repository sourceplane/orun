package runbundle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sourceplane/orun/internal/state"
)

// HydrateOptions controls hydration behavior.
type HydrateOptions struct {
	ExecID     string
	Source     ShardSource
	Overwrite  bool
	IncludeRaw bool
}

// HydrateResult describes what was hydrated.
type HydrateResult struct {
	ExecDir  string
	ExecID   string
	JobCount int
	LogFiles int
}

// Hydrate reconstructs .orun/executions/{exec-id}/ from a plan shard and
// job shards. Uses existing state.Store types for compatibility with
// orun status, orun logs, and other commands.
func Hydrate(ctx context.Context, planShard *PlanShard, jobShards []*JobShard, opts HydrateOptions, orunDir string) (*HydrateResult, error) {
	if planShard == nil {
		return nil, fmt.Errorf("plan shard is required")
	}
	if opts.ExecID == "" {
		return nil, fmt.Errorf("exec ID is required")
	}
	if orunDir == "" {
		orunDir = ".orun"
	}

	// Synthesize execution state from shards
	exec, err := Synthesize(planShard, jobShards)
	if err != nil {
		return nil, fmt.Errorf("synthesize failed: %w", err)
	}

	execDir := filepath.Join(orunDir, "executions", opts.ExecID)
	logsDir := filepath.Join(execDir, "logs")

	// Check for existing directory
	if !opts.Overwrite {
		if _, err := os.Stat(execDir); err == nil {
			return nil, fmt.Errorf("execution directory already exists at %s (use Overwrite to replace)", execDir)
		}
	}

	// Create directories
	for _, dir := range []string{execDir, logsDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Write metadata.json
	metadata := &state.ExecMetadata{
		ExecID:     opts.ExecID,
		PlanID:     exec.PlanID,
		PlanName:   planShard.Plan.Metadata.Name,
		StartedAt:  exec.CreatedAt,
		FinishedAt: time.Now().UTC().Format(time.RFC3339),
		Status:     exec.Status,
		Trigger:    planShard.Manifest.Source.EventName,
		JobTotal:   exec.Counts.Total,
		JobDone:    exec.Counts.Completed,
		JobFailed:  exec.Counts.Failed,
	}
	store := state.NewStore(filepath.Dir(orunDir))
	if err := store.SaveMetadata(opts.ExecID, metadata); err != nil {
		return nil, fmt.Errorf("failed to write metadata: %w", err)
	}

	// Write state.json
	execState := &state.ExecState{
		ExecID:       opts.ExecID,
		PlanChecksum: planShard.Plan.Metadata.Checksum,
		Jobs:         make(map[string]*state.JobState),
	}
	for _, js := range jobShards {
		if js == nil || js.JobState == nil {
			continue
		}
		execState.Jobs[js.Manifest.JobID] = js.JobState
	}
	if err := store.SaveState(opts.ExecID, execState); err != nil {
		return nil, fmt.Errorf("failed to write state: %w", err)
	}

	// Write github.json (source provenance)
	sourceInfo := map[string]interface{}{
		"type":       opts.Source.Type,
		"repository": opts.Source.Repository,
		"runId":      opts.Source.RunID,
		"runAttempt": opts.Source.RunAttempt,
		"workflow":   opts.Source.Workflow,
		"sha":        opts.Source.SHA,
		"ref":        opts.Source.Ref,
		"eventName":  opts.Source.EventName,
	}
	if err := writeJSON(filepath.Join(execDir, "github.json"), sourceInfo); err != nil {
		return nil, fmt.Errorf("failed to write github.json: %w", err)
	}

	// Write plan.json (copy of plan)
	if planShard.Plan != nil {
		if err := writeJSON(filepath.Join(execDir, "plan.json"), planShard.Plan); err != nil {
			return nil, fmt.Errorf("failed to write plan.json: %w", err)
		}
	}

	// Write shards.json (provenance)
	shardsInfo := map[string]interface{}{
		"plan": map[string]string{
			"name": exec.PlanShard.Name,
			"role": exec.PlanShard.Role,
		},
		"jobs": exec.Jobs,
	}
	if err := writeJSON(filepath.Join(execDir, "shards.json"), shardsInfo); err != nil {
		return nil, fmt.Errorf("failed to write shards.json: %w", err)
	}

	// Update latest symlink
	latestLink := filepath.Join(orunDir, "executions", "latest")
	os.Remove(latestLink)
	os.Symlink(opts.ExecID, latestLink)

	// Copy logs from job shards
	var logFileCount int
	for _, js := range jobShards {
		if js == nil || js.Manifest == nil || js.Dir == "" {
			continue
		}
		jobID := js.Manifest.JobID
		if jobID == "" {
			continue
		}

		// Find log entries in the manifest and copy from shard dir
		var shardLogs []string
		for logical, relPath := range js.Manifest.Files {
			if len(logical) > 4 && logical[:4] == "log:" && relPath != "" {
				shardLogs = append(shardLogs, relPath)
			}
		}
		if len(shardLogs) == 0 {
			// Try the logs/ directory directly
			srcLogsDir := filepath.Join(js.Dir, "logs")
			if entries, err := os.ReadDir(srcLogsDir); err == nil {
				for _, e := range entries {
					if !e.IsDir() {
						shardLogs = append(shardLogs, filepath.Join("logs", e.Name()))
					}
				}
			}
		}

		if len(shardLogs) == 0 {
			continue
		}

		jobLogsDir := filepath.Join(logsDir, sanitizePathSegment(jobID))
		if err := os.MkdirAll(jobLogsDir, 0755); err != nil {
			continue
		}

		for _, relPath := range shardLogs {
			srcPath := filepath.Join(js.Dir, relPath)
			dstName := filepath.Base(relPath)
			dstPath := filepath.Join(jobLogsDir, dstName)

			data, err := os.ReadFile(srcPath)
			if err != nil {
				continue
			}
			if err := os.WriteFile(dstPath, data, 0644); err != nil {
				continue
			}
			logFileCount++
		}
	}

	result := &HydrateResult{
		ExecDir:  execDir,
		ExecID:   opts.ExecID,
		JobCount: len(jobShards),
		LogFiles: logFileCount,
	}

	return result, nil
}

// sanitizePathSegment replaces characters unsafe for file paths.
func sanitizePathSegment(s string) string {
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
			result = append(result, c)
		case c >= 'A' && c <= 'Z':
			result = append(result, c)
		case c >= '0' && c <= '9':
			result = append(result, c)
		case c == '-' || c == '_' || c == '.':
			result = append(result, c)
		default:
			result = append(result, '_')
		}
	}
	return string(result)
}