package objread

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/runworktree"
)

// Errors are routed on the shared object-store taxonomy.
var (
	ErrNotFound = objectstore.ErrNotFound
	ErrInvalid  = objectstore.ErrInvalid
)

// Canonical execution ref names / prefixes (relative to refs/).
const (
	refExecutionsLatest = "executions/latest"
	refExecByIDPrefix   = "executions/by-id/"
)

// ExecutionView is a presentation-neutral execution, sealed or live.
type ExecutionView struct {
	ExecutionID  string
	ExecutionKey string
	RevisionID   string
	TriggerID    string
	Status       string
	StartedAt    time.Time
	FinishedAt   *time.Time
	DryRun       bool
	Live         bool                 // reconstructed from an in-flight working tree
	Summary      nodes.ExecSummary    // sealed summary, or computed for a live run
	Links        []nodes.ExecLink     // optional
	Jobs         []JobView            // populated by Get; nil for List headers
	ObjectID     objectstore.ObjectID // sealed Merkle root; empty when Live
}

// JobView / AttemptView / StepView mirror the sealed lineage, flattened.
type JobView struct {
	JobID      string
	Folder     string
	Status     string
	LastError  string
	StartedAt  *time.Time
	FinishedAt *time.Time
	Attempts   []AttemptView
}

type AttemptView struct {
	Attempt    int
	Status     string
	StartedAt  *time.Time
	FinishedAt *time.Time
	Steps      []StepView
}

type StepView struct {
	StepID     string
	Status     string
	ExitCode   int
	StartedAt  *time.Time
	FinishedAt *time.Time
	LogID      string // sealed log blob id ("" if none / live)
	HasLog     bool
	logFolder  string // internal: job folder, for live log lookup
}

// Reader reconstructs views over one object/ref store pair rooted at root (the
// object-model root holding objects/, refs/, run/).
type Reader struct {
	store objectstore.ObjectStore
	refs  refstore.RefStore
	root  string
}

// New constructs a Reader.
func New(store objectstore.ObjectStore, refs refstore.RefStore, root string) *Reader {
	return &Reader{store: store, refs: refs, root: root}
}

// List returns execution headers newest-first: live (in-flight) runs first,
// then sealed executions. Jobs are not populated (use Get). A live run that has
// the same id as a sealed one (a brief window during seal) is reported once, as
// live.
func (r *Reader) List(ctx context.Context) ([]ExecutionView, error) {
	var out []ExecutionView
	seen := map[string]bool{}

	live, err := runworktree.ListLive(r.root)
	if err != nil {
		return nil, err
	}
	for i := range live {
		v := headerFromSnapshot(&live[i])
		out = append(out, v)
		seen[v.ExecutionID] = true
	}

	names, err := r.refs.List(ctx, refExecByIDPrefix)
	if err != nil {
		return nil, fmt.Errorf("objread: list executions: %w", err)
	}
	sealed := make([]ExecutionView, 0, len(names))
	for _, name := range names {
		ref, rerr := r.refs.Read(ctx, name)
		if rerr != nil {
			continue
		}
		ex, _, derr := r.readExecutionRecord(ctx, objectstore.ObjectID(ref.Target))
		if derr != nil {
			continue
		}
		if seen[ex.ExecutionID] {
			continue
		}
		sealed = append(sealed, headerFromRecord(ex, objectstore.ObjectID(ref.Target)))
	}
	sort.SliceStable(sealed, func(i, j int) bool {
		if !sealed[i].StartedAt.Equal(sealed[j].StartedAt) {
			return sealed[i].StartedAt.After(sealed[j].StartedAt)
		}
		return sealed[i].ExecutionID < sealed[j].ExecutionID
	})
	return append(out, sealed...), nil
}

// Get returns one execution with full job/attempt/step detail. ref may be:
// "executions/latest", "executions/by-id/<id>", a full ref path, or a bare
// execution id (resolved as a live working tree first, else executions/by-id).
func (r *Reader) Get(ctx context.Context, ref string) (ExecutionView, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		ref = refExecutionsLatest
	}

	// A bare id (no slash) may be an in-flight run; prefer the live tree.
	if !strings.Contains(ref, "/") {
		if snap, ok, err := runworktree.LoadLive(r.root, ref); err != nil {
			return ExecutionView{}, err
		} else if ok {
			return viewFromSnapshot(snap), nil
		}
	}

	target, err := r.resolve(ctx, ref)
	if err != nil {
		return ExecutionView{}, err
	}
	return r.readSealed(ctx, target)
}

// resolve turns a ref or bare id into a sealed execution root id.
func (r *Reader) resolve(ctx context.Context, ref string) (objectstore.ObjectID, error) {
	candidates := []string{ref}
	if !strings.Contains(ref, "/") {
		candidates = append(candidates, refExecByIDPrefix+sanitizeIDSeg(ref))
	}
	for _, name := range candidates {
		got, err := r.refs.Read(ctx, name)
		if err == nil {
			return objectstore.ObjectID(got.Target), nil
		}
	}
	return "", fmt.Errorf("objread: execution %q: %w", ref, ErrNotFound)
}

// readExecutionRecord decodes just the execution.json (header) from a sealed
// tree root.
func (r *Reader) readExecutionRecord(ctx context.Context, root objectstore.ObjectID) (nodes.ExecutionRun, objectstore.ObjectID, error) {
	entries, err := r.store.GetTree(ctx, root)
	if err != nil {
		return nodes.ExecutionRun{}, "", err
	}
	for _, e := range entries {
		if e.Name == fileExecution {
			_, body, gerr := r.store.Get(ctx, e.ID)
			if gerr != nil {
				return nodes.ExecutionRun{}, "", gerr
			}
			rec, derr := nodes.Decode[nodes.ExecutionRun](body)
			return rec, e.ID, derr
		}
	}
	return nodes.ExecutionRun{}, "", fmt.Errorf("objread: %w: no execution.json in %s", ErrInvalid, root)
}

// readSealed reconstructs a full ExecutionView from a sealed tree root.
func (r *Reader) readSealed(ctx context.Context, root objectstore.ObjectID) (ExecutionView, error) {
	rec, _, err := r.readExecutionRecord(ctx, root)
	if err != nil {
		return ExecutionView{}, err
	}
	view := headerFromRecord(rec, root)

	jobsTree, err := r.subTree(ctx, root, dirJobs)
	if err != nil {
		return ExecutionView{}, err
	}
	for _, jobEntry := range jobsTree {
		jv, jerr := r.readJob(ctx, jobEntry.ID)
		if jerr != nil {
			return ExecutionView{}, jerr
		}
		view.Jobs = append(view.Jobs, jv)
	}
	sort.SliceStable(view.Jobs, func(i, j int) bool { return view.Jobs[i].JobID < view.Jobs[j].JobID })
	return view, nil
}

func (r *Reader) readJob(ctx context.Context, jobTreeID objectstore.ObjectID) (JobView, error) {
	entries, err := r.store.GetTree(ctx, jobTreeID)
	if err != nil {
		return JobView{}, err
	}
	var jv JobView
	for _, e := range entries {
		switch {
		case e.Name == fileJobRun:
			_, body, gerr := r.store.Get(ctx, e.ID)
			if gerr != nil {
				return JobView{}, gerr
			}
			rec, derr := nodes.Decode[nodes.JobRun](body)
			if derr != nil {
				return JobView{}, derr
			}
			jv.JobID = rec.JobID
			jv.Folder = rec.Folder
			jv.Status = rec.Status
			jv.LastError = rec.LastError
			jv.StartedAt = rec.StartedAt
			jv.FinishedAt = rec.FinishedAt
		case e.Name == dirAttempts && e.Kind == objectstore.KindTree:
			atts, aerr := r.store.GetTree(ctx, e.ID)
			if aerr != nil {
				return JobView{}, aerr
			}
			for _, attEntry := range atts {
				av, verr := r.readAttempt(ctx, attEntry.ID)
				if verr != nil {
					return JobView{}, verr
				}
				jv.Attempts = append(jv.Attempts, av)
			}
		}
	}
	for i := range jv.Attempts {
		for k := range jv.Attempts[i].Steps {
			jv.Attempts[i].Steps[k].logFolder = jv.Folder
		}
	}
	sort.SliceStable(jv.Attempts, func(i, j int) bool { return jv.Attempts[i].Attempt < jv.Attempts[j].Attempt })
	return jv, nil
}

func (r *Reader) readAttempt(ctx context.Context, attTreeID objectstore.ObjectID) (AttemptView, error) {
	entries, err := r.store.GetTree(ctx, attTreeID)
	if err != nil {
		return AttemptView{}, err
	}
	var av AttemptView
	for _, e := range entries {
		switch {
		case e.Name == fileAttempt:
			_, body, gerr := r.store.Get(ctx, e.ID)
			if gerr != nil {
				return AttemptView{}, gerr
			}
			rec, derr := nodes.Decode[nodes.JobAttempt](body)
			if derr != nil {
				return AttemptView{}, derr
			}
			av.Attempt = rec.Attempt
			av.Status = rec.Status
			av.StartedAt = rec.StartedAt
			av.FinishedAt = rec.FinishedAt
		case e.Name == dirSteps && e.Kind == objectstore.KindTree:
			steps, serr := r.store.GetTree(ctx, e.ID)
			if serr != nil {
				return AttemptView{}, serr
			}
			for _, stepEntry := range steps {
				_, body, gerr := r.store.Get(ctx, stepEntry.ID)
				if gerr != nil {
					return AttemptView{}, gerr
				}
				rec, derr := nodes.Decode[nodes.StepAttempt](body)
				if derr != nil {
					return AttemptView{}, derr
				}
				av.Steps = append(av.Steps, StepView{
					StepID:     rec.StepID,
					Status:     rec.Status,
					ExitCode:   rec.ExitCode,
					StartedAt:  rec.StartedAt,
					FinishedAt: rec.FinishedAt,
					LogID:      rec.LogID,
					HasLog:     rec.LogID != "",
				})
			}
		}
	}
	sort.SliceStable(av.Steps, func(i, j int) bool { return av.Steps[i].StepID < av.Steps[j].StepID })
	return av, nil
}

// StepLog returns a step's captured output. For a sealed execution it reads the
// log content blob; for a live run it reads the working-tree log file. view must
// come from Get.
func (r *Reader) StepLog(ctx context.Context, view ExecutionView, jobID, stepID string) ([]byte, error) {
	for _, j := range view.Jobs {
		if j.JobID != jobID && j.Folder != jobID {
			continue
		}
		for _, a := range j.Attempts {
			for _, s := range a.Steps {
				if s.StepID != stepID {
					continue
				}
				if view.Live {
					snap, ok, err := runworktree.LoadLive(r.root, view.ExecutionID)
					if err != nil || !ok {
						return nil, ErrNotFound
					}
					return os.ReadFile(snap.LogPath(r.root, j.Folder, stepID))
				}
				if s.LogID == "" {
					return nil, nil
				}
				_, body, err := r.store.Get(ctx, objectstore.ObjectID(s.LogID))
				return body, err
			}
		}
	}
	return nil, ErrNotFound
}

// PlanSummary reads an execution's compiled plan from its revision tree
// (the revision's plan.json) and returns the plan's display name and the unique
// component names it references, in plan order. It is best-effort: any error
// (no revision id, missing plan.json, decode failure) yields ("", nil) so
// callers degrade to substring matching / a blank column rather than failing.
func (r *Reader) PlanSummary(ctx context.Context, view ExecutionView) (name string, components []string) {
	if view.RevisionID == "" {
		return "", nil
	}
	entries, err := r.store.GetTree(ctx, objectstore.ObjectID(view.RevisionID))
	if err != nil {
		return "", nil
	}
	for _, e := range entries {
		if e.Name != filePlan {
			continue
		}
		_, body, gerr := r.store.Get(ctx, e.ID)
		if gerr != nil {
			return "", nil
		}
		var p struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Jobs []struct {
				Component string `json:"component"`
			} `json:"jobs"`
		}
		if uerr := json.Unmarshal(body, &p); uerr != nil {
			return "", nil
		}
		seen := make(map[string]struct{}, len(p.Jobs))
		comps := make([]string, 0, len(p.Jobs))
		for _, j := range p.Jobs {
			if j.Component == "" {
				continue
			}
			if _, ok := seen[j.Component]; ok {
				continue
			}
			seen[j.Component] = struct{}{}
			comps = append(comps, j.Component)
		}
		if len(comps) == 0 {
			comps = nil
		}
		return p.Metadata.Name, comps
	}
	return "", nil
}

func (r *Reader) subTree(ctx context.Context, root objectstore.ObjectID, name string) ([]objectstore.TreeEntry, error) {
	entries, err := r.store.GetTree(ctx, root)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Name == name && e.Kind == objectstore.KindTree {
			return r.store.GetTree(ctx, e.ID)
		}
	}
	return nil, nil
}

// ---- view builders ----

func headerFromRecord(rec nodes.ExecutionRun, root objectstore.ObjectID) ExecutionView {
	return ExecutionView{
		ExecutionID:  rec.ExecutionID,
		ExecutionKey: rec.ExecutionKey,
		RevisionID:   rec.RevisionID,
		TriggerID:    rec.TriggerID,
		Status:       rec.Status,
		StartedAt:    rec.StartedAt,
		FinishedAt:   rec.FinishedAt,
		DryRun:       rec.DryRun,
		Summary:      rec.Summary,
		Links:        rec.Links,
		ObjectID:     root,
	}
}

func headerFromSnapshot(s *runworktree.Snapshot) ExecutionView {
	return ExecutionView{
		ExecutionID:  s.ExecutionID,
		ExecutionKey: s.ExecutionKey,
		RevisionID:   s.RevisionID,
		TriggerID:    s.TriggerID,
		Status:       s.Status,
		StartedAt:    s.StartedAt,
		FinishedAt:   s.FinishedAt,
		DryRun:       s.DryRun,
		Live:         true,
		Summary:      summarizeSnapshot(s),
		Links:        s.Links,
	}
}

func viewFromSnapshot(s *runworktree.Snapshot) ExecutionView {
	v := headerFromSnapshot(s)
	for _, sj := range s.Jobs {
		jv := JobView{
			JobID:      sj.JobID,
			Folder:     sj.Folder,
			Status:     sj.Status,
			LastError:  sj.LastError,
			StartedAt:  sj.StartedAt,
			FinishedAt: sj.FinishedAt,
		}
		for _, sa := range sj.Attempts {
			av := AttemptView{Attempt: sa.Attempt, Status: sa.Status, StartedAt: sa.StartedAt, FinishedAt: sa.FinishedAt}
			for _, ss := range sa.Steps {
				av.Steps = append(av.Steps, StepView{
					StepID:     ss.StepID,
					Status:     ss.Status,
					ExitCode:   ss.ExitCode,
					StartedAt:  ss.StartedAt,
					FinishedAt: ss.FinishedAt,
					HasLog:     ss.LogFile != "",
					logFolder:  sj.Folder,
				})
			}
			jv.Attempts = append(jv.Attempts, av)
		}
		v.Jobs = append(v.Jobs, jv)
	}
	sort.SliceStable(v.Jobs, func(i, j int) bool { return v.Jobs[i].JobID < v.Jobs[j].JobID })
	return v
}

// summarizeSnapshot computes counts for a live run (sealed runs carry their own).
func summarizeSnapshot(s *runworktree.Snapshot) nodes.ExecSummary {
	sum := nodes.ExecSummary{JobsTotal: len(s.Jobs)}
	for _, j := range s.Jobs {
		switch j.Status {
		case nodes.StatusSucceeded:
			sum.JobsSucceeded++
		case nodes.StatusFailed:
			sum.JobsFailed++
		}
		for _, a := range j.Attempts {
			sum.StepsTotal += len(a.Steps)
		}
	}
	return sum
}

// sanitizeIDSeg folds an execution id into a ref-path segment (matches execseal).
func sanitizeIDSeg(id string) string {
	var b strings.Builder
	for _, ch := range id {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9', ch == '.', ch == '_', ch == '-':
			b.WriteRune(ch)
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

// Canonical tree filenames mirrored from internal/nodes (unexported there).
const (
	fileExecution = "execution.json"
	fileJobRun    = "job-run.json"
	fileAttempt   = "attempt.json"
	filePlan      = "plan.json"
	dirJobs       = "jobs"
	dirAttempts   = "attempts"
	dirSteps      = "steps"
)
