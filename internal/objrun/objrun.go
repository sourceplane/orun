// Package objrun is the shared session glue that drives the content-addressed
// object model from a runner's lifecycle: it opens a live working tree for a
// run, projects the runner's authoritative job/step state into it on every
// state tick, streams each step's log as a content blob, and seals the working
// tree into an immutable ExecutionRun on terminal.
//
// Both `orun run` (cmd/orun) and the interactive TUI run path use this one
// implementation, so a TUI-initiated run is sealed exactly like a CLI run. The
// glue is best-effort from the caller's perspective: Begin/Finish return errors
// the caller may warn on, and InstallHooks/Finish are nil-safe so a caller can
// thread a possibly-nil session through unconditionally.
package objrun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/nodewriter"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/objplan"
	"github.com/sourceplane/orun/internal/runner"
	"github.com/sourceplane/orun/internal/runworktree"
)

// revByHashPrefix is the ref prefix under which `orun plan` publishes each
// revision keyed by its plan hash, enabling a cheap dedup lookup at run time.
const revByHashPrefix = "revisions/by-hash/"

// Session holds the live working tree for one object-model run.
type Session struct {
	mgr   *runworktree.Manager
	wt    *runworktree.WorkTree
	revID objectstore.ObjectID
}

// Begin resolves the revision the execution attaches to and opens a live
// working tree for it under root (the object-model root, e.g. .orun/objectmodel).
//
// The run path NEVER re-resolves the catalog — that is `orun plan`'s job, which
// already published the revision under revisions/by-hash/<planHash>. Begin first
// reads that ref (a single cheap lookup) and, only on a miss, materializes a
// catalog-free degenerate revision (plan only) cheaply. This keeps every run
// cheap, which is the prerequisite for the object model being the default.
//
// Returns (nil, nil) when plan or execID are empty (nothing to do). Returns
// (nil, err) on a store / revision / working-tree failure so the caller can warn
// without changing the run's exit code.
func Begin(ctx context.Context, root string, plan *model.Plan, execID string) (*Session, error) {
	if plan == nil || execID == "" {
		return nil, nil
	}

	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		return nil, fmt.Errorf("open object store: %w", err)
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Writer: "runner"})
	if err != nil {
		return nil, fmt.Errorf("open ref store: %w", err)
	}

	revID, err := resolveRunRevision(ctx, store, refs, root, plan)
	if err != nil {
		return nil, err
	}

	mgr := runworktree.NewManager(store, refs, root)
	// Seal any working trees orphaned by a prior crash before opening ours.
	// Recovery is best-effort: a failure here must not block the new run.
	_, _ = mgr.RecoverStale(ctx)

	wt, err := mgr.Open(ctx, runworktree.OpenInput{
		ExecutionID:  execID,
		ExecutionKey: deriveExecKey(execID),
		RevisionID:   revID,
	})
	if err != nil {
		return nil, fmt.Errorf("open working tree: %w", err)
	}
	return &Session{mgr: mgr, wt: wt, revID: revID}, nil
}

// deriveExecKey produces a stable execution key from the execution id. The
// object model addresses executions by id (objread.Get), so the key is a
// display / drill-down handle, not an identity. A content-derived "run-<hash>"
// keeps it deterministic and non-empty without the legacy stateful run-NNN
// sequence scan — so `orun catalog history`/`describe` (which read it off the
// sealed execution) have a real handle instead of a blank column.
func deriveExecKey(execID string) string {
	sum := sha256.Sum256([]byte(execID))
	return "run-" + hex.EncodeToString(sum[:])[:12]
}

// RevisionID returns the revision the session attached to ("" for a nil session).
func (s *Session) RevisionID() objectstore.ObjectID {
	if s == nil {
		return ""
	}
	return s.revID
}

// InstallHooks chains the live working-tree writes onto the runner's lifecycle
// hooks: each state tick projects the runner's authoritative job/step state into
// the working tree (and bumps the heartbeat), and each step log is streamed as a
// content blob. The chain preserves any previously-installed callbacks. Safe to
// call on a nil session (no-op).
func (s *Session) InstallHooks(r *runner.Runner) {
	if s == nil || r == nil {
		return
	}
	if r.Hooks == nil {
		r.Hooks = &runner.RunnerHooks{}
	}
	prev := r.Hooks.AfterStateUpdate
	r.Hooks.AfterStateUpdate = func() {
		if prev != nil {
			prev()
		}
		// Project the runner's authoritative in-memory state directly.
		if st := r.SnapshotState(); st != nil {
			_ = s.wt.Project(projectFromExecState(st))
		}
	}
	prevLog := r.Hooks.AfterStepLog
	r.Hooks.AfterStepLog = func(jobID, stepID, output string) {
		if prevLog != nil {
			prevLog(jobID, stepID, output)
		}
		if output != "" {
			_ = s.wt.SetStepLog(jobID, stepID, []byte(output))
		}
	}
}

// Finish applies a final projection (catching the post-shutdown state) and seals
// the working tree at the run's terminal status, returning the sealed execution
// id. It reads the runner's in-memory state, not any legacy store. Safe to call
// on a nil session (returns "", nil).
func (s *Session) Finish(ctx context.Context, r *runner.Runner, runErr error) (objectstore.ObjectID, error) {
	if s == nil {
		return "", nil
	}
	var st *execmodel.ExecState
	if r != nil {
		st = r.SnapshotState()
	}
	if st != nil {
		_ = s.wt.Project(projectFromExecState(st))
	}

	status := nodes.StatusSucceeded
	switch {
	case runErr != nil:
		status = nodes.StatusFailed
	case st != nil:
		if execmodel.SummarizeExecutionState(st).Failed > 0 {
			status = nodes.StatusFailed
		}
	}
	return s.wt.Seal(ctx, status, time.Time{})
}

// SealStep is one step's terminal record for an imported run (see Seal). Status
// is the source vocabulary (e.g. "success"/"failed"/"skipped"); Seal folds it
// onto the node status set. Log, when present, is stored as a content blob.
type SealStep struct {
	StepID   string
	Status   string
	ExitCode int
	Log      []byte
}

// SealJob is one job's terminal record for an imported run, with its steps in
// caller order.
type SealJob struct {
	JobID     string
	Status    string
	LastError string
	Steps     []SealStep
}

// SealLink is an external link recorded on the execution (e.g. the CI run page).
type SealLink struct {
	Label string
	URL   string
}

// ImportInput describes an already-terminal run to record into the object graph.
// Status is the source run status (folded onto the node vocabulary; a value that
// does not fold to a terminal status is sealed as failed, since an imported run
// is by definition finished).
type ImportInput struct {
	ExecID     string
	Status     string
	StartedAt  time.Time
	FinishedAt time.Time
	Jobs       []SealJob
	Links      []SealLink
}

// Seal records an already-terminal run into the content-addressed object graph
// under root, attaching it to the revision plan resolves to (the same dedup /
// degenerate-revision logic Begin uses), and returns the sealed execution id.
//
// It is the non-runner counterpart to Begin/Finish: callers that import a run
// that finished elsewhere — e.g. `orun github pull` recording a pulled GitHub
// Actions run — drive this instead of a live runner. The run is projected and
// sealed through the same runworktree path the live runner uses, so an imported
// execution is shaped identically to a natively-run one and is readable by
// `orun status`/`orun logs`. Idempotent: sealing identical content yields the
// same id.
func Seal(ctx context.Context, root string, plan *model.Plan, in ImportInput) (objectstore.ObjectID, error) {
	if plan == nil {
		return "", fmt.Errorf("objrun: plan is nil")
	}
	if in.ExecID == "" {
		return "", fmt.Errorf("objrun: exec id is required")
	}

	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		return "", fmt.Errorf("open object store: %w", err)
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Writer: "import"})
	if err != nil {
		return "", fmt.Errorf("open ref store: %w", err)
	}

	revID, err := resolveRunRevision(ctx, store, refs, root, plan)
	if err != nil {
		return "", err
	}

	mgr := runworktree.NewManager(store, refs, root)
	wt, err := mgr.Open(ctx, runworktree.OpenInput{
		ExecutionID: in.ExecID,
		RevisionID:  revID,
		StartedAt:   in.StartedAt,
	})
	if err != nil {
		return "", fmt.Errorf("open working tree: %w", err)
	}

	projected := make([]runworktree.ProjectedJob, 0, len(in.Jobs))
	for _, j := range in.Jobs {
		pj := runworktree.ProjectedJob{
			JobID:     j.JobID,
			Status:    runnerStatusToNode(j.Status),
			LastError: j.LastError,
		}
		for _, s := range j.Steps {
			pj.Steps = append(pj.Steps, runworktree.ProjectedStep{
				StepID:   s.StepID,
				Status:   runnerStatusToNode(s.Status),
				ExitCode: s.ExitCode,
			})
		}
		projected = append(projected, pj)
	}
	if err := wt.Project(projected); err != nil {
		return "", fmt.Errorf("project run: %w", err)
	}

	for _, j := range in.Jobs {
		for _, s := range j.Steps {
			if len(s.Log) == 0 {
				continue
			}
			if err := wt.SetStepLog(j.JobID, s.StepID, s.Log); err != nil {
				return "", fmt.Errorf("attach step log %s/%s: %w", j.JobID, s.StepID, err)
			}
		}
	}

	for _, l := range in.Links {
		_ = wt.AddLink(nodes.ExecLink{Label: l.Label, URL: l.URL})
	}

	status := runnerStatusToNode(in.Status)
	if !nodes.IsTerminalStatus(status) {
		status = nodes.StatusFailed
	}
	return wt.Seal(ctx, status, in.FinishedAt)
}

// resolveRunRevision returns the revision the run attaches to: the one `orun
// plan` already published for this plan (revisions/by-hash/<planHash>) when
// present, else a freshly-materialized catalog-free degenerate revision.
func resolveRunRevision(ctx context.Context, store *objectstore.LocalStore, refs *refstore.LocalRefStore, root string, plan *model.Plan) (objectstore.ObjectID, error) {
	planHash, herr := PlanHash(plan)
	if herr == nil && planHash != "" {
		if r, rerr := refs.Read(ctx, revByHashPrefix+sanitizeRevSeg(planHash)); rerr == nil {
			return objectstore.ObjectID(r.Target), nil
		}
	}

	planBytes, err := CanonicalPlanJSON(plan)
	if err != nil {
		return "", fmt.Errorf("marshal plan: %w", err)
	}
	w := nodewriter.New(store, refs)
	res, err := objplan.Plan(ctx, w, store, objplan.NewResolveMemo(root), objplan.Input{
		PlanBytes:      planBytes,
		RevisionScope:  nodes.RevisionScope{Mode: "full"},
		JobCount:       len(plan.Jobs),
		LegacyChecksum: planHash,
		Trigger: nodes.TriggerOccurrence{
			Kind:        nodes.KindTriggerOccurrence,
			TriggerName: "system.run",
			Source:      nodes.TriggerSource{Flavor: "system", System: "run"},
			Scope:       nodes.RevisionScope{Mode: "full"},
			Actor:       "runner",
		},
	}, objplan.Options{NoCatalog: true})
	if err != nil {
		return "", fmt.Errorf("resolve revision: %w", err)
	}
	return res.RevisionID, nil
}

// projectFromExecState maps the runner state into the working-tree projection
// input, in deterministic (sorted) order so the sealed tree is reproducible.
func projectFromExecState(st *execmodel.ExecState) []runworktree.ProjectedJob {
	if st == nil || len(st.Jobs) == 0 {
		return nil
	}
	jobIDs := make([]string, 0, len(st.Jobs))
	for id := range st.Jobs {
		jobIDs = append(jobIDs, id)
	}
	sort.Strings(jobIDs)

	out := make([]runworktree.ProjectedJob, 0, len(jobIDs))
	for _, jid := range jobIDs {
		js := st.Jobs[jid]
		pj := runworktree.ProjectedJob{
			JobID:     jid,
			Status:    runnerStatusToNode(js.Status),
			LastError: js.LastError,
		}
		stepIDs := make([]string, 0, len(js.Steps))
		for sid := range js.Steps {
			stepIDs = append(stepIDs, sid)
		}
		sort.Strings(stepIDs)
		for _, sid := range stepIDs {
			pj.Steps = append(pj.Steps, runworktree.ProjectedStep{
				StepID: sid,
				Status: runnerStatusToNode(js.Steps[sid]),
			})
		}
		out = append(out, pj)
	}
	return out
}

// runnerStatusToNode folds the runner's status vocabulary onto the node status
// set (kept independent of the legacy bridge so it can be deleted separately).
func runnerStatusToNode(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "success", "succeeded", "completed", "complete", "ok", "passed":
		return nodes.StatusSucceeded
	case "failed", "failure", "error", "errored":
		return nodes.StatusFailed
	case "cancelled", "canceled", "skipped":
		return nodes.StatusCancelled
	case "running", "in_progress", "in-progress", "started":
		return nodes.StatusRunning
	default:
		return nodes.StatusPending
	}
}

// PlanHash returns the canonical "sha256:<hex>" digest of the plan's content
// with self-referential metadata (checksum, revision) cleared, so the hash is
// stable across re-runs of the same intent on the same SHA. It is the dedup key
// under revisions/by-hash/ shared by `orun plan` and the run path.
func PlanHash(plan *model.Plan) (string, error) {
	if plan == nil {
		return "", fmt.Errorf("plan is nil")
	}
	clone := *plan
	clone.Metadata.Checksum = ""
	clone.Metadata.Revision = nil
	payload, err := json.Marshal(&clone)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// CanonicalPlanJSON marshals plan as deterministic indented JSON suitable for
// persistence as the revision's canonical plan.json.
func CanonicalPlanJSON(plan *model.Plan) ([]byte, error) {
	if plan == nil {
		return nil, fmt.Errorf("plan is nil")
	}
	return json.MarshalIndent(plan, "", "  ")
}

// sanitizeRevSeg folds a checksum/ref into the ref-path alphabet (matches the
// objplan writer's sanitizer).
func sanitizeRevSeg(s string) string {
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
