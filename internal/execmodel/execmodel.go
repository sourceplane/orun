// Package execmodel holds the durable execution value types and pure helpers
// shared across orun — the in-memory shape of a run's per-job/step state and the
// small derivations over it. These were historically defined in internal/state
// alongside the legacy on-disk file store; they are factored out here so that
// consumers (the runner, presenters, view models, remote sync) depend only on
// the data shapes, not on the legacy store. internal/state keeps type aliases
// for backward compatibility while the object-model cutover repoints callers.
//
// This package performs no file I/O for execution/plan persistence — that is the
// object model's job (internal/objectstore + internal/runworktree). It depends
// only on internal/model.
package execmodel

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/model"
)

// ExecState is the in-memory state of one execution: per-job status keyed by
// job id.
type ExecState struct {
	ExecID       string               `json:"execId"`
	PlanChecksum string               `json:"planChecksum"`
	Jobs         map[string]*JobState `json:"jobs"`
}

// JobState is one job's status, timing, and per-step status map.
type JobState struct {
	Status      string            `json:"status"`
	StartedAt   string            `json:"startedAt,omitempty"`
	FinishedAt  string            `json:"finishedAt,omitempty"`
	HeartbeatAt string            `json:"heartbeatAt,omitempty"`
	Steps       map[string]string `json:"steps"`
	LastError   string            `json:"lastError,omitempty"`
}

// ExecutionLink is an external link surfaced for an execution (CI run page, etc.).
type ExecutionLink struct {
	Label  string `json:"label"`
	URL    string `json:"url"`
	JobID  string `json:"jobId,omitempty"`
	StepID string `json:"stepId,omitempty"`
}

// ExecMetadata is the execution header: identity, status, counts, and links.
type ExecMetadata struct {
	ExecID     string          `json:"execId"`
	PlanID     string          `json:"planId"`
	PlanName   string          `json:"planName"`
	StartedAt  string          `json:"startedAt"`
	FinishedAt string          `json:"finishedAt,omitempty"`
	Status     string          `json:"status"`
	Trigger    string          `json:"trigger"`
	User       string          `json:"user"`
	DryRun     bool            `json:"dryRun"`
	JobTotal   int             `json:"jobTotal"`
	JobDone    int             `json:"jobDone"`
	JobFailed  int             `json:"jobFailed"`
	Links      []ExecutionLink `json:"links,omitempty"`
}

// PlanEntry is a row in a plan listing.
type PlanEntry struct {
	Name      string
	Path      string
	Checksum  string
	Jobs      int
	CreatedAt time.Time
}

// ExecEntry is a row in an execution listing.
type ExecEntry struct {
	ID         string
	PlanName   string
	Status     string
	StartedAt  string
	FinishedAt string
	JobTotal   int
	JobDone    int
	JobFailed  int
}

// ExecutionCounts is the rolled-up tally of an ExecState.
type ExecutionCounts struct {
	Total     int
	Completed int
	Failed    int
	Running   int
	Pending   int
}

// GenerateExecID mints a human-readable execution id: <plan-name>-<YYYYMMDD>-<hex>.
func GenerateExecID(planName string) string {
	date := time.Now().Format("20060102")
	randBytes := make([]byte, 3)
	rand.Read(randBytes)
	suffix := hex.EncodeToString(randBytes)

	name := strings.ReplaceAll(planName, " ", "-")
	name = strings.ToLower(name)
	if name == "" {
		name = "run"
	}
	if len(name) > 30 {
		name = name[:30]
	}
	return fmt.Sprintf("%s-%s-%s", name, date, suffix)
}

// SummarizeExecutionState tallies job statuses for an execution.
func SummarizeExecutionState(st *ExecState) ExecutionCounts {
	if st == nil {
		return ExecutionCounts{}
	}
	counts := ExecutionCounts{}
	for _, job := range st.Jobs {
		if job == nil {
			continue
		}
		counts.Total++
		switch strings.ToLower(strings.TrimSpace(job.Status)) {
		case "completed":
			counts.Completed++
		case "failed":
			counts.Failed++
		case "running":
			counts.Running++
		default:
			counts.Pending++
		}
	}
	return counts
}

// PlanChecksumShort returns the short (12-char) form of a plan's checksum.
func PlanChecksumShort(plan *model.Plan) string {
	if plan == nil || plan.Metadata.Checksum == "" {
		return ""
	}
	cs := strings.TrimPrefix(plan.Metadata.Checksum, "sha256-")
	if len(cs) > 12 {
		return cs[:12]
	}
	return cs
}

// LoadPlanFile reads and decodes a plan JSON file.
func LoadPlanFile(path string) (*model.Plan, error) {
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
