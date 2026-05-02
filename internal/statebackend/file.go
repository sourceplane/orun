package statebackend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/state"
)

// FileStateBackend implements Backend using the local .orun/ filesystem store.
// When InitRun is called with a plan, the backend provides real cross-process
// claim semantics using advisory file locks scoped to the execution ID.
type FileStateBackend struct {
	Store *state.Store
	plan  *model.Plan
	jobs  map[string]model.PlanJob
}

// NewFileStateBackend wraps an existing state.Store.
func NewFileStateBackend(store *state.Store) *FileStateBackend {
	return &FileStateBackend{Store: store}
}

// InitRunPlan stores the plan for dependency checking without creating
// execution directories. Use this when the runner creates the execution
// directory separately via Store.CreateExecution.
func (f *FileStateBackend) InitRunPlan(plan *model.Plan) {
	if plan == nil {
		return
	}
	f.plan = plan
	f.jobs = make(map[string]model.PlanJob, len(plan.Jobs))
	for _, j := range plan.Jobs {
		f.jobs[j.ID] = j
	}
}

func (f *FileStateBackend) InitRun(_ context.Context, plan *model.Plan, opts InitRunOptions) (*RunHandle, error) {
	if err := f.Store.EnsureDirs(); err != nil {
		return nil, err
	}
	if _, err := f.Store.CreateExecution(opts.RunID, plan); err != nil {
		return nil, err
	}
	if plan != nil {
		f.plan = plan
		f.jobs = make(map[string]model.PlanJob, len(plan.Jobs))
		for _, j := range plan.Jobs {
			f.jobs[j.ID] = j
		}
	}
	return &RunHandle{RunID: opts.RunID}, nil
}

func (f *FileStateBackend) lockPath(runID string) string {
	return filepath.Join(f.Store.ExecDir(), runID, ".lock")
}

// ClaimJob acquires an execution-scoped file lock, checks the current state,
// and atomically marks the job as running if it can be claimed.
func (f *FileStateBackend) ClaimJob(ctx context.Context, runID string, job model.PlanJob, _ string) (*ClaimResult, error) {
	fl := NewFileLock(f.lockPath(runID))
	if err := fl.Lock(ctx); err != nil {
		return nil, err
	}
	defer fl.Unlock()

	st, err := f.Store.LoadState(runID)
	if err != nil {
		return nil, err
	}

	if js, ok := st.Jobs[job.ID]; ok && js != nil {
		switch js.Status {
		case "completed":
			return &ClaimResult{Claimed: false, CurrentStatus: "completed"}, nil
		case "running":
			return &ClaimResult{Claimed: false, CurrentStatus: "running"}, nil
		case "failed":
			return &ClaimResult{Claimed: false, CurrentStatus: "failed"}, nil
		}
	}

	for _, depID := range job.DependsOn {
		ds, ok := st.Jobs[depID]
		if !ok || ds == nil || ds.Status == "pending" || ds.Status == "running" || ds.Status == "" {
			return &ClaimResult{Claimed: false, DepsWaiting: true}, nil
		}
		if ds.Status == "failed" {
			return &ClaimResult{Claimed: false, DepsBlocked: true}, nil
		}
	}

	if st.Jobs == nil {
		st.Jobs = map[string]*state.JobState{}
	}
	js := st.Jobs[job.ID]
	if js == nil {
		js = &state.JobState{Steps: map[string]string{}}
		st.Jobs[job.ID] = js
	}
	js.Status = "running"
	js.StartedAt = time.Now().UTC().Format(time.RFC3339)

	if err := f.Store.SaveState(runID, st); err != nil {
		return nil, err
	}
	return &ClaimResult{Claimed: true}, nil
}

// Heartbeat is a no-op locally.
func (f *FileStateBackend) Heartbeat(_ context.Context, _, _, _ string) (*HeartbeatResult, error) {
	return &HeartbeatResult{OK: true}, nil
}

// UpdateJob persists the terminal job status under the file lock.
func (f *FileStateBackend) UpdateJob(ctx context.Context, runID, jobID, _ string, status JobStatus, errText string) error {
	fl := NewFileLock(f.lockPath(runID))
	if err := fl.Lock(ctx); err != nil {
		return err
	}
	defer fl.Unlock()

	st, err := f.Store.LoadState(runID)
	if err != nil {
		return err
	}
	js := st.Jobs[jobID]
	if js == nil {
		return nil
	}
	switch status {
	case JobStatusSuccess:
		js.Status = "completed"
	case JobStatusFailed:
		js.Status = "failed"
		js.LastError = errText
	}
	js.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	return f.Store.SaveState(runID, st)
}

// AppendStepLog is a no-op locally — the runner writes logs via writeStepLog.
func (f *FileStateBackend) AppendStepLog(_ context.Context, _, _, _ string) error {
	return nil
}

// RunnableJobs returns job IDs whose dependencies are all completed.
func (f *FileStateBackend) RunnableJobs(_ context.Context, runID string) ([]string, error) {
	if f.plan == nil {
		return nil, nil
	}
	st, err := f.Store.LoadState(runID)
	if err != nil {
		return nil, err
	}
	var runnable []string
	for _, job := range f.plan.Jobs {
		js := st.Jobs[job.ID]
		if js != nil && (js.Status == "completed" || js.Status == "running" || js.Status == "failed") {
			continue
		}
		allDepsMet := true
		for _, dep := range job.DependsOn {
			ds := st.Jobs[dep]
			if ds == nil || ds.Status != "completed" {
				allDepsMet = false
				break
			}
		}
		if allDepsMet {
			runnable = append(runnable, job.ID)
		}
	}
	return runnable, nil
}

func (f *FileStateBackend) LoadRunState(_ context.Context, runID string) (*state.ExecState, *state.ExecMetadata, error) {
	st, err := f.Store.LoadState(runID)
	if err != nil {
		return nil, nil, err
	}
	meta, err := f.Store.LoadMetadata(runID)
	if err != nil {
		return nil, nil, err
	}
	return st, meta, nil
}

// ReadJobLog concatenates all per-step log files for the given job.
func (f *FileStateBackend) ReadJobLog(_ context.Context, runID string, jobID string) (string, error) {
	logDir := f.Store.LogDir(runID, jobID)
	entries, err := os.ReadDir(logDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	var sb strings.Builder
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(logDir, entry.Name()))
		if readErr == nil && len(data) > 0 {
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			sb.Write(data)
		}
	}
	return sb.String(), nil
}

func (f *FileStateBackend) Close(_ context.Context) error { return nil }
