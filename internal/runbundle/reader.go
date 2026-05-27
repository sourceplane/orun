package runbundle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/state"
)

// ReadShardManifest reads and validates the manifest from a shard directory.
func ReadShardManifest(dir string) (*RunBundleShardManifest, error) {
	path := filepath.Join(dir, "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest at %s: %w", path, err)
	}

	var m RunBundleShardManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to parse manifest at %s: %w", path, err)
	}

	if err := ValidateShardManifest(&m); err != nil {
		return nil, fmt.Errorf("invalid manifest at %s: %w", path, err)
	}

	if err := ValidateShardFiles(dir, &m); err != nil {
		return nil, fmt.Errorf("file validation failed at %s: %w", dir, err)
	}

	return &m, nil
}

// PlanShard holds the contents of a parsed plan shard.
type PlanShard struct {
	Dir      string
	Manifest *RunBundleShardManifest
	Plan     *model.Plan
}

// ReadPlanShard reads a plan shard from disk.
func ReadPlanShard(dir string) (*PlanShard, error) {
	manifest, err := ReadShardManifest(dir)
	if err != nil {
		return nil, err
	}

	if manifest.Role != ShardRolePlan {
		return nil, fmt.Errorf("expected plan shard, got role %q", manifest.Role)
	}

	planPath := filepath.Join(dir, "plan.json")
	plan, err := loadPlanFile(planPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read plan: %w", err)
	}

	return &PlanShard{
		Dir:      dir,
		Manifest: manifest,
		Plan:     plan,
	}, nil
}

// JobShard holds the contents of a parsed job shard.
type JobShard struct {
	Dir      string
	Manifest *RunBundleShardManifest
	JobState *state.JobState
}

// ReadJobShard reads a job shard from disk.
func ReadJobShard(dir string) (*JobShard, error) {
	manifest, err := ReadShardManifest(dir)
	if err != nil {
		return nil, err
	}

	if manifest.Role != ShardRoleJob {
		return nil, fmt.Errorf("expected job shard, got role %q", manifest.Role)
	}

	statePath := filepath.Join(dir, "state.json")
	js, err := loadJobState(statePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read job state: %w", err)
	}

	return &JobShard{
		Dir:      dir,
		Manifest: manifest,
		JobState: js,
	}, nil
}

// loadPlanFile reads a Plan from a JSON file.
func loadPlanFile(path string) (*model.Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var plan model.Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

// loadJobState reads a JobState from a JSON file.
func loadJobState(path string) (*state.JobState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var js state.JobState
	if err := json.Unmarshal(data, &js); err != nil {
		return nil, err
	}
	return &js, nil
}