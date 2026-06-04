// Package execseal seals a finished execution into the content-addressed object
// graph (specs/orun-object-model runner-integration.md §1). It is the "commit"
// half of the runner's working-tree/seal model: given an in-memory description
// of a terminal run — its jobs, attempts, steps, logs, events, and artifacts —
// it assembles the immutable ExecutionRun tree under its revision and publishes
// refs/executions/latest. The runner's live working-tree writes (internal/objrun
// + internal/runworktree) drive this on terminal; this package is the
// self-contained, deterministic seal operation.
package execseal

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/nodewriter"
	"github.com/sourceplane/orun/internal/objectstore"
)

// refExecutionsLatest is the pointer to the most recently sealed (or live)
// execution. Logical name, relative to the ref store's refs/ dir.
const refExecutionsLatest = "executions/latest"

// refExecutionsByID is the prefix under which each sealed execution gets its own
// stable pointer (executions/by-id/<execId>). Unlike executions/latest, these
// are never overwritten by a newer run, so every execution stays reachable —
// enumerable by listing the prefix and safe from reachability GC.
const refExecutionsByIDPrefix = "executions/by-id/"

// sanitizeIDSeg folds an execution id into a single ref-path segment.
func sanitizeIDSeg(id string) string {
	var b strings.Builder
	for _, r := range id {
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

// Sealer seals executions over an object graph. It reuses a nodewriter.Writer
// for object storage and ref moves; only execution id minting is its own.
type Sealer struct {
	w     *nodewriter.Writer
	newID func() string
}

// Option configures a Sealer.
type Option func(*Sealer)

// WithExecIDGen overrides the execution id generator (default "exec_"+ULID).
func WithExecIDGen(fn func() string) Option { return func(s *Sealer) { s.newID = fn } }

// New constructs a Sealer over a writer.
func New(w *nodewriter.Writer, opts ...Option) *Sealer {
	s := &Sealer{
		w:     w,
		newID: func() string { return "exec_" + ulid.Make().String() },
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// SealInput describes a terminal run to seal. Jobs/Events/Artifacts use the
// node assembly input types directly.
type SealInput struct {
	RevisionID    objectstore.ObjectID
	TriggerID     string // trg_ id (optional)
	ExecutionID   string // exec_<ULID> or gh-<run>-<attempt>-<sha>; minted if empty
	ExecutionKey  string // run-NNN
	Status        string // must be terminal
	StartedAt     time.Time
	FinishedAt    time.Time
	DryRun        bool
	RunnerProfile nodes.RunnerProfile
	Links         []nodes.ExecLink
	Jobs          []nodes.JobInput
	Events        []nodes.NamedBlob
	Artifacts     []nodes.NamedBlob
}

// Seal computes the execution summary, assembles the immutable execution tree,
// and publishes refs/executions/latest at the sealed id. It is idempotent:
// sealing an identical run yields the same id (content addressing). The status
// must be terminal.
func (s *Sealer) Seal(ctx context.Context, in SealInput) (objectstore.ObjectID, error) {
	if !nodes.IsTerminalStatus(in.Status) {
		return "", fmt.Errorf("%w: execution status %q is not terminal", nodes.ErrInvalid, in.Status)
	}
	if err := objectstore.ValidateID(in.RevisionID); err != nil {
		return "", fmt.Errorf("execseal: revisionId: %w", err)
	}
	execID := in.ExecutionID
	if execID == "" {
		execID = s.newID()
	}

	exec := nodes.ExecutionRun{
		Kind:          nodes.KindExecutionRun,
		ExecutionID:   execID,
		ExecutionKey:  in.ExecutionKey,
		RevisionID:    string(in.RevisionID),
		TriggerID:     in.TriggerID,
		Status:        in.Status,
		StartedAt:     in.StartedAt,
		DryRun:        in.DryRun,
		RunnerProfile: in.RunnerProfile,
		Summary:       summarize(in.Jobs),
		Links:         in.Links,
	}
	if !in.FinishedAt.IsZero() {
		ft := in.FinishedAt
		exec.FinishedAt = &ft
	}

	id, err := nodes.AssembleExecution(ctx, s.w.Store(), nodes.ExecutionInput{
		Execution: exec,
		Jobs:      in.Jobs,
		Events:    in.Events,
		Artifacts: in.Artifacts,
	})
	if err != nil {
		return "", fmt.Errorf("execseal: assemble: %w", err)
	}
	refs := []string{
		refExecutionsLatest,
		refExecutionsByIDPrefix + sanitizeIDSeg(execID),
	}
	if err := s.w.MoveRefs(ctx, refs, id); err != nil {
		return "", fmt.Errorf("execseal: publish execution refs: %w", err)
	}
	return id, nil
}

// summarize rolls up job/step counts for the execution record.
func summarize(jobs []nodes.JobInput) nodes.ExecSummary {
	sum := nodes.ExecSummary{JobsTotal: len(jobs)}
	for _, j := range jobs {
		switch j.Record.Status {
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
