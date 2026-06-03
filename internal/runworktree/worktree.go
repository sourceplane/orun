package runworktree

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/sourceplane/orun/internal/clock"
	"github.com/sourceplane/orun/internal/execseal"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/nodewriter"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

// Errors, routed on the shared object-store taxonomy so callers handle them the
// same way they handle object/ref errors.
var (
	ErrInvalid  = objectstore.ErrInvalid
	ErrConflict = objectstore.ErrConflict
	ErrNotFound = objectstore.ErrNotFound
)

// snapshotKind / lockKind tag the working files so a stray reader can tell them
// apart from sealed content records.
const (
	snapshotKind = "RunWorkingTree"
	lockKind     = "RunLock"

	runDir       = "run"
	snapshotFile = "run.json"
	lockFile     = "run.lock"
	logsDir      = "logs"

	// refLivePrefix is the in-flight handle prefix (relative to refs/).
	refLivePrefix = "executions/live/"

	// defaultStaleAfter is how long without a heartbeat before a working tree is
	// considered crashed and eligible for recovery.
	defaultStaleAfter = 5 * time.Minute
)

// Snapshot is the live, atomically-rewritten working-tree state. It is exported
// so live readers (TUI / `orun objects log` for an in-flight run) can decode it
// without going through seal. Its shape mirrors the sealed ExecutionRun lineage.
type Snapshot struct {
	Kind          string              `json:"kind"`
	ExecutionID   string              `json:"executionId"`
	ExecutionKey  string              `json:"executionKey"`
	RevisionID    string              `json:"revisionId"`
	TriggerID     string              `json:"triggerId,omitempty"`
	Status        string              `json:"status"`
	StartedAt     time.Time           `json:"startedAt"`
	FinishedAt    *time.Time          `json:"finishedAt,omitempty"`
	DryRun        bool                `json:"dryRun"`
	RunnerProfile nodes.RunnerProfile `json:"runnerProfile"`
	Links         []nodes.ExecLink    `json:"links,omitempty"`
	Jobs          []SnapshotJob       `json:"jobs"`
	UpdatedAt     time.Time           `json:"updatedAt"`
}

// SnapshotJob/Attempt/Step are the live lower lineage.
type SnapshotJob struct {
	JobID      string            `json:"jobId"`
	Folder     string            `json:"folder"`
	Status     string            `json:"status"`
	StartedAt  *time.Time        `json:"startedAt,omitempty"`
	FinishedAt *time.Time        `json:"finishedAt,omitempty"`
	LastError  string            `json:"lastError,omitempty"`
	Attempts   []SnapshotAttempt `json:"attempts"`
}

type SnapshotAttempt struct {
	Attempt    int            `json:"attempt"`
	Status     string         `json:"status"`
	StartedAt  *time.Time     `json:"startedAt,omitempty"`
	FinishedAt *time.Time     `json:"finishedAt,omitempty"`
	Steps      []SnapshotStep `json:"steps"`
}

type SnapshotStep struct {
	StepID      string     `json:"stepId"`
	Status      string     `json:"status"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
	ExitCode    int        `json:"exitCode"`
	HeartbeatAt *time.Time `json:"heartbeatAt,omitempty"`
	LogFile     string     `json:"logFile,omitempty"` // relative to the working-tree dir
}

// lockRecord is the crash sentinel written alongside the snapshot.
type lockRecord struct {
	Kind          string    `json:"kind"`
	PID           int       `json:"pid"`
	StartedAt     time.Time `json:"startedAt"`
	LastHeartbeat time.Time `json:"lastHeartbeat"`
	CurrentJob    string    `json:"currentJob,omitempty"`
}

// Manager opens and recovers working trees over one object/ref store pair.
type Manager struct {
	store      objectstore.ObjectStore
	refs       refstore.RefStore
	sealer     *execseal.Sealer
	root       string // .orun/objectmodel
	clk        clock.Clock
	newID      func() string
	staleAfter time.Duration
}

// Option configures a Manager.
type Option func(*Manager)

// WithClock overrides the clock (default clock.New()).
func WithClock(c clock.Clock) Option { return func(m *Manager) { m.clk = c } }

// WithStaleAfter overrides the heartbeat-staleness window used by recovery.
func WithStaleAfter(d time.Duration) Option {
	return func(m *Manager) {
		if d > 0 {
			m.staleAfter = d
		}
	}
}

// WithExecIDGen overrides the execution id generator used when Open is given an
// empty ExecutionID (default "exec_"+ULID).
func WithExecIDGen(fn func() string) Option { return func(m *Manager) { m.newID = fn } }

// NewManager constructs a Manager. root is the object-model root (the directory
// that holds objects/ and refs/, e.g. .orun/objectmodel); working trees live
// under root/run/.
func NewManager(store objectstore.ObjectStore, refs refstore.RefStore, root string, opts ...Option) *Manager {
	m := &Manager{
		store:      store,
		refs:       refs,
		root:       root,
		clk:        clock.New(),
		newID:      func() string { return "exec_" + ulid.Make().String() },
		staleAfter: defaultStaleAfter,
	}
	for _, o := range opts {
		o(m)
	}
	w := nodewriter.New(store, refs, nodewriter.WithClock(m.clk))
	m.sealer = execseal.New(w)
	return m
}

// OpenInput describes an execution about to start.
type OpenInput struct {
	ExecutionID   string // minted if empty
	ExecutionKey  string // run-NNN
	RevisionID    objectstore.ObjectID
	TriggerID     string
	DryRun        bool
	RunnerProfile nodes.RunnerProfile
	StartedAt     time.Time // optional; clock now if zero
}

// WorkTree is a single live execution being written before seal. Its methods
// are safe for concurrent use by parallel jobs (each flush is serialized).
type WorkTree struct {
	mgr    *Manager
	dir    string
	execID string
	revID  objectstore.ObjectID

	mu     sync.Mutex
	snap   *Snapshot
	jobIdx map[string]int // jobID -> index into snap.Jobs
	sealed bool
}

// Open creates a fresh working tree for the execution and marks it in-flight.
// It fails with ErrConflict if a non-stale working tree already exists for the
// same execution id; a stale one is reclaimed.
func (m *Manager) Open(ctx context.Context, in OpenInput) (*WorkTree, error) {
	if err := objectstore.ValidateID(in.RevisionID); err != nil {
		return nil, fmt.Errorf("runworktree: revisionId: %w", err)
	}
	execID := in.ExecutionID
	if execID == "" {
		execID = m.newID()
	}
	dir := m.execDir(execID)

	if existing, err := readLock(dir); err == nil {
		if m.clk.Now().Sub(existing.LastHeartbeat) < m.staleAfter {
			return nil, fmt.Errorf("runworktree: execution %q already live: %w", execID, ErrConflict)
		}
		// Stale: reclaim by removing the prior working tree.
		_ = os.RemoveAll(dir)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("runworktree: create working dir: %w", err)
	}

	now := m.clk.Now()
	started := in.StartedAt
	if started.IsZero() {
		started = now
	}
	wt := &WorkTree{
		mgr:    m,
		dir:    dir,
		execID: execID,
		revID:  in.RevisionID,
		snap: &Snapshot{
			Kind:          snapshotKind,
			ExecutionID:   execID,
			ExecutionKey:  in.ExecutionKey,
			RevisionID:    string(in.RevisionID),
			TriggerID:     in.TriggerID,
			Status:        nodes.StatusRunning,
			StartedAt:     started.UTC(),
			DryRun:        in.DryRun,
			RunnerProfile: in.RunnerProfile,
			UpdatedAt:     now,
		},
		jobIdx: map[string]int{},
	}
	if err := wt.persist(now, ""); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	// Mark in-flight: a live handle pointing at the revision (a valid object id),
	// enumerable for crash recovery. Created with old="" to assert freshness.
	if err := m.refs.Update(ctx, liveRefName(execID), "", string(in.RevisionID)); err != nil && !isConflict(err) {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("runworktree: mark live: %w", err)
	}
	return wt, nil
}

// ExecutionID returns the (possibly minted) execution id.
func (wt *WorkTree) ExecutionID() string { return wt.execID }

// Dir returns the working-tree directory.
func (wt *WorkTree) Dir() string { return wt.dir }

// Snapshot returns a deep-ish copy of the current live state for readers.
func (wt *WorkTree) Snapshot() Snapshot {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	return cloneSnapshot(wt.snap)
}

// StartJob records a job (and its first attempt) as running.
func (wt *WorkTree) StartJob(jobID string) error {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	now := wt.mgr.clk.Now()
	j := wt.job(jobID)
	if j.StartedAt == nil {
		t := now
		j.StartedAt = &t
	}
	j.Status = nodes.StatusRunning
	if len(j.Attempts) == 0 {
		j.Attempts = append(j.Attempts, SnapshotAttempt{Attempt: 1, Status: nodes.StatusRunning, StartedAt: ptr(now)})
	}
	return wt.persist(now, jobID)
}

// StartAttempt begins a new attempt for a job (for whole-job retries). Steps
// recorded afterwards target the new attempt.
func (wt *WorkTree) StartAttempt(jobID string) error {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	now := wt.mgr.clk.Now()
	j := wt.job(jobID)
	n := len(j.Attempts) + 1
	j.Attempts = append(j.Attempts, SnapshotAttempt{Attempt: n, Status: nodes.StatusRunning, StartedAt: ptr(now)})
	j.Status = nodes.StatusRunning
	return wt.persist(now, jobID)
}

// StartStep records a step as running under the job's current attempt.
func (wt *WorkTree) StartStep(jobID, stepID string) error {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	now := wt.mgr.clk.Now()
	a := wt.currentAttempt(jobID, now)
	s := stepRef(a, stepID)
	if s.StartedAt == nil {
		s.StartedAt = ptr(now)
	}
	s.Status = nodes.StatusRunning
	return wt.persist(now, jobID)
}

// FinishStep records a step's terminal status, exit code, and (optional) log.
// The log is streamed to the working tree and becomes a content blob at seal;
// identical logs dedup automatically.
func (wt *WorkTree) FinishStep(jobID, stepID, status string, exitCode int, log []byte) error {
	if !nodes.IsTerminalStatus(status) {
		return fmt.Errorf("runworktree: step %q status %q not terminal: %w", stepID, status, ErrInvalid)
	}
	wt.mu.Lock()
	defer wt.mu.Unlock()
	now := wt.mgr.clk.Now()
	a := wt.currentAttempt(jobID, now)
	s := stepRef(a, stepID)
	if s.StartedAt == nil {
		s.StartedAt = ptr(now)
	}
	s.Status = status
	s.ExitCode = exitCode
	s.FinishedAt = ptr(now)
	if len(log) > 0 {
		j := wt.job(jobID)
		rel := filepath.Join(logsDir, j.Folder, sanitizeName(stepID)+".log")
		if err := writeFileAtomic(filepath.Join(wt.dir, rel), log); err != nil {
			return fmt.Errorf("runworktree: write step log: %w", err)
		}
		s.LogFile = rel
	}
	return wt.persist(now, jobID)
}

// FinishJob records a job's terminal status (and closes its current attempt).
func (wt *WorkTree) FinishJob(jobID, status, lastErr string) error {
	if !nodes.IsTerminalStatus(status) {
		return fmt.Errorf("runworktree: job %q status %q not terminal: %w", jobID, status, ErrInvalid)
	}
	wt.mu.Lock()
	defer wt.mu.Unlock()
	now := wt.mgr.clk.Now()
	j := wt.job(jobID)
	j.Status = status
	j.LastError = lastErr
	j.FinishedAt = ptr(now)
	if n := len(j.Attempts); n > 0 {
		a := &j.Attempts[n-1]
		a.Status = status
		a.FinishedAt = ptr(now)
	}
	return wt.persist(now, jobID)
}

// AddLink attaches an external link (CI page, etc.) to the execution.
func (wt *WorkTree) AddLink(link nodes.ExecLink) error {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	wt.snap.Links = append(wt.snap.Links, link)
	return wt.persist(wt.mgr.clk.Now(), "")
}

// Heartbeat bumps the liveness timestamp (and the current-job marker) without
// changing run content.
func (wt *WorkTree) Heartbeat(currentJob string) error {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	return wt.persist(wt.mgr.clk.Now(), currentJob)
}

// Seal writes the full execution tree as immutable objects, publishes
// refs/executions/latest (and executions/by-id/<execId>), drops the live handle,
// and removes the working tree. It is idempotent: sealing identical content
// yields the same id. status must be terminal.
func (wt *WorkTree) Seal(ctx context.Context, status string, finishedAt time.Time) (objectstore.ObjectID, error) {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	if wt.sealed {
		return "", fmt.Errorf("runworktree: execution %q already sealed: %w", wt.execID, ErrConflict)
	}
	if !nodes.IsTerminalStatus(status) {
		return "", fmt.Errorf("runworktree: seal status %q not terminal: %w", status, ErrInvalid)
	}
	now := wt.mgr.clk.Now()
	if finishedAt.IsZero() {
		finishedAt = now
	}
	wt.snap.Status = status
	ft := finishedAt.UTC()
	wt.snap.FinishedAt = &ft
	if err := wt.persist(now, ""); err != nil {
		return "", err
	}

	id, err := wt.mgr.sealSnapshot(ctx, wt.dir, wt.snap)
	if err != nil {
		return "", err
	}
	// Published. Drop the in-flight handle, then the working tree. A crash
	// between here and removal leaves an idempotent re-seal for recovery.
	_ = wt.mgr.refs.Delete(ctx, liveRefName(wt.execID))
	_ = os.RemoveAll(wt.dir)
	wt.sealed = true
	return id, nil
}

// ---- internal helpers ----

// job returns (creating if needed) the live job record. Caller holds wt.mu.
func (wt *WorkTree) job(jobID string) *SnapshotJob {
	if i, ok := wt.jobIdx[jobID]; ok {
		return &wt.snap.Jobs[i]
	}
	wt.snap.Jobs = append(wt.snap.Jobs, SnapshotJob{
		JobID:    jobID,
		Folder:   jobFolder(jobID),
		Status:   nodes.StatusPending,
		Attempts: nil,
	})
	wt.jobIdx[jobID] = len(wt.snap.Jobs) - 1
	return &wt.snap.Jobs[len(wt.snap.Jobs)-1]
}

// currentAttempt returns the job's latest attempt, creating attempt 1 if none.
// Caller holds wt.mu.
func (wt *WorkTree) currentAttempt(jobID string, now time.Time) *SnapshotAttempt {
	j := wt.job(jobID)
	if len(j.Attempts) == 0 {
		j.Attempts = append(j.Attempts, SnapshotAttempt{Attempt: 1, Status: nodes.StatusRunning, StartedAt: ptr(now)})
	}
	return &j.Attempts[len(j.Attempts)-1]
}

// persist flushes the snapshot and refreshes the lockfile heartbeat. Caller
// holds wt.mu.
func (wt *WorkTree) persist(now time.Time, currentJob string) error {
	wt.snap.UpdatedAt = now
	if err := writeJSONAtomic(filepath.Join(wt.dir, snapshotFile), wt.snap); err != nil {
		return fmt.Errorf("runworktree: flush snapshot: %w", err)
	}
	lr := lockRecord{
		Kind:          lockKind,
		PID:           os.Getpid(),
		StartedAt:     wt.snap.StartedAt,
		LastHeartbeat: now,
		CurrentJob:    currentJob,
	}
	if err := writeJSONAtomic(filepath.Join(wt.dir, lockFile), lr); err != nil {
		return fmt.Errorf("runworktree: flush lock: %w", err)
	}
	return nil
}

// execDir returns the working-tree directory for an execution id.
func (m *Manager) execDir(execID string) string {
	return filepath.Join(m.root, runDir, sanitizeName(execID))
}

// sealSnapshot converts a live snapshot into a seal input (reading step logs
// from the working tree) and seals it. Shared by Seal and recovery.
func (m *Manager) sealSnapshot(ctx context.Context, dir string, snap *Snapshot) (objectstore.ObjectID, error) {
	jobs := make([]nodes.JobInput, 0, len(snap.Jobs))
	for _, sj := range snap.Jobs {
		jr := nodes.JobRun{
			Kind:       nodes.KindJobRun,
			JobID:      sj.JobID,
			Folder:     sj.Folder,
			Status:     sealStatus(sj.Status),
			StartedAt:  sj.StartedAt,
			FinishedAt: sj.FinishedAt,
			LastError:  sj.LastError,
		}
		attempts := make([]nodes.AttemptInput, 0, len(sj.Attempts))
		for _, sa := range sj.Attempts {
			att := nodes.JobAttempt{
				Kind:       nodes.KindJobAttempt,
				Attempt:    sa.Attempt,
				Status:     sealStatus(sa.Status),
				StartedAt:  sa.StartedAt,
				FinishedAt: sa.FinishedAt,
			}
			steps := make([]nodes.StepInput, 0, len(sa.Steps))
			for _, ss := range sa.Steps {
				rec := nodes.StepAttempt{
					Kind:        nodes.KindStepAttempt,
					StepID:      ss.StepID,
					Status:      sealStatus(ss.Status),
					StartedAt:   ss.StartedAt,
					FinishedAt:  ss.FinishedAt,
					ExitCode:    ss.ExitCode,
					HeartbeatAt: ss.HeartbeatAt,
				}
				var logBytes []byte
				if ss.LogFile != "" {
					if b, err := os.ReadFile(filepath.Join(dir, ss.LogFile)); err == nil {
						logBytes = b
					}
				}
				steps = append(steps, nodes.StepInput{Record: rec, Log: logBytes})
			}
			attempts = append(attempts, nodes.AttemptInput{Record: att, Steps: steps})
		}
		jobs = append(jobs, nodes.JobInput{Record: jr, Attempts: attempts})
	}

	in := execseal.SealInput{
		RevisionID:    objectstore.ObjectID(snap.RevisionID),
		TriggerID:     snap.TriggerID,
		ExecutionID:   snap.ExecutionID,
		ExecutionKey:  snap.ExecutionKey,
		Status:        snap.Status,
		StartedAt:     snap.StartedAt,
		DryRun:        snap.DryRun,
		RunnerProfile: snap.RunnerProfile,
		Links:         snap.Links,
		Jobs:          jobs,
	}
	if snap.FinishedAt != nil {
		in.FinishedAt = *snap.FinishedAt
	}
	return m.sealer.Seal(ctx, in)
}

// sealStatus folds a non-terminal live status onto a terminal one so a sealed
// record always validates. A running/pending leaf at seal time means the run was
// interrupted, so it is recorded as failed.
func sealStatus(s string) string {
	if nodes.IsTerminalStatus(s) {
		return s
	}
	return nodes.StatusFailed
}

func liveRefName(execID string) string { return refLivePrefix + sanitizeName(execID) }

// jobFolder derives the sanitized j-<shortHash> folder for a job id (the
// original id is preserved in the record). Matches the sealed convention.
func jobFolder(jobID string) string {
	sum := sha256.Sum256([]byte(jobID))
	return "j-" + hex.EncodeToString(sum[:])[:8]
}

// sanitizeName folds an arbitrary id into a single path/ref segment.
func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-._")
	if out == "" {
		return "x"
	}
	return out
}

func ptr(t time.Time) *time.Time { u := t.UTC(); return &u }

func cloneSnapshot(s *Snapshot) Snapshot {
	out := *s
	out.Links = append([]nodes.ExecLink(nil), s.Links...)
	out.Jobs = make([]SnapshotJob, len(s.Jobs))
	for i, j := range s.Jobs {
		nj := j
		nj.Attempts = make([]SnapshotAttempt, len(j.Attempts))
		for k, a := range j.Attempts {
			na := a
			na.Steps = append([]SnapshotStep(nil), a.Steps...)
			nj.Attempts[k] = na
		}
		out.Jobs[i] = nj
	}
	return out
}

func stepRef(a *SnapshotAttempt, stepID string) *SnapshotStep {
	for i := range a.Steps {
		if a.Steps[i].StepID == stepID {
			return &a.Steps[i]
		}
	}
	a.Steps = append(a.Steps, SnapshotStep{StepID: stepID, Status: nodes.StatusPending})
	return &a.Steps[len(a.Steps)-1]
}

// readLock reads the lock sentinel for a working-tree dir.
func readLock(dir string) (lockRecord, error) {
	b, err := os.ReadFile(filepath.Join(dir, lockFile))
	if err != nil {
		return lockRecord{}, err
	}
	var lr lockRecord
	if err := json.Unmarshal(b, &lr); err != nil {
		return lockRecord{}, err
	}
	return lr, nil
}

// readSnapshot reads the live snapshot for a working-tree dir.
func readSnapshot(dir string) (*Snapshot, error) {
	b, err := os.ReadFile(filepath.Join(dir, snapshotFile))
	if err != nil {
		return nil, err
	}
	var s Snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func writeJSONAtomic(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, b)
}

// writeFileAtomic writes via a temp file in the same directory + rename.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func isConflict(err error) bool { return errors.Is(err, ErrConflict) }
