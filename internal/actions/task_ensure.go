package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/taskfile"
)

// orun.task/ensure@v1 and orun.task/rollup@v1 — the bootstrap's hand on the
// task plane (orun-bootstrap-engine BE-O5).
//
// # Why "ensure" and not "create"
//
// A bootstrap is re-runnable by construction: a phase that half-finished is
// re-run, and a phase already done is re-run and does nothing. So every write
// it makes must be idempotent BY IDENTITY — an epic by slug, a milestone by
// name within its epic, a task by title within its epic. `create` is the wrong
// verb for that, and a baseline hand-rolls the difference in ~210 lines of
// shell today, list-then-create, for each of the three.
//
// The plane already meets this halfway: `CreateEpic` returns the existing epic
// rather than a 409, because "that slug is taken" and "that one, then" are the
// same answer to an idempotent caller. Milestones and tasks need the list-first
// read, which is what this does.
//
// # Why a bootstrap must create tasks CONTRACTED
//
// The plane parks a merged PR at `in_review` when its task never declared
// gates — gates unknown to us are not gates passed. A bootstrap creates its
// tasks seconds before their PRs, so it has to be able to say "merge alone
// finishes this" at birth, or every landing it makes sits un-folded forever.

func init() {
	register(Spec{
		ID:      "orun.task/ensure@v1",
		Summary: "Find-or-create an epic, a milestone or a task, by identity",
		Params: append(orgParams(),
			Param{Name: "kind", Type: ParamString, Required: true,
				Description: "epic | milestone | task"},
			Param{Name: "name", Type: ParamString, Required: true,
				Description: "epic name, milestone name, or task title"},
			Param{Name: "slug", Type: ParamString,
				Description: "epic only: the stable slug it is found by"},
			Param{Name: "epic", Type: ParamString,
				Description: "milestone and task: the epic (slug, EP-n or epc_…)"},
			Param{Name: "milestone", Type: ParamString,
				Description: "task only: the milestone to club under"},
			Param{Name: "brief", Type: ParamString,
				Description: "task only: what the task is for"},
			Param{Name: "prefix", Type: ParamString, Default: "ORUN",
				Description: "task only: key prefix when a key is minted"},
			Param{Name: "contract", Type: ParamString,
				Description: "task only: a TaskContract document to attach, relative to the blueprint"},
			Param{Name: "exitCriteria", Type: ParamStringList,
				Description: "milestone only: what finishing it means"},
		),
		Outputs: []string{"id", "key", "existed"},
	}, runTaskEnsure)

	register(Spec{
		ID:      "orun.task/rollup@v1",
		Summary: "Read an epic's progress — total, done, blocked",
		Params: append(orgParams(),
			Param{Name: "epic", Type: ParamString, Required: true,
				Description: "the epic (slug, EP-n or epc_…)"},
		),
		Outputs: []string{"total", "done", "blocked", "complete"},
	}, runTaskRollup)
}

// taskPlane is the slice of the platform client these actions use. Narrow on
// purpose: it is what makes the find-or-create logic — the part that is easy to
// get subtly wrong — testable without an auth handshake or a live backend.
// *remotestate.Client satisfies it.
type taskPlane interface {
	CreateEpic(ctx context.Context, org string, req remotestate.EpicCreateRequest) (*remotestate.PublicEpic, error)
	GetEpic(ctx context.Context, org, ref string) (*remotestate.EpicView, error)
	CreateMilestone(ctx context.Context, org, epicRef string, req remotestate.MilestoneCreateRequest) (*remotestate.PublicMilestone, error)
	ListTasksWhere(ctx context.Context, org string, filter remotestate.TaskListFilter) (*remotestate.TasksList, error)
	CreateTask(ctx context.Context, org string, req remotestate.TaskCreateRequest) (*remotestate.PublicTask, error)
	AttachTaskContract(ctx context.Context, org, keyOrID string, wire json.RawMessage, hash string) (*remotestate.TaskContractSeal, error)
}

func runTaskEnsure(ctx context.Context, in Input) (Result, error) {
	client, org, err := cloudClient(ctx, in)
	if err != nil {
		return Result{}, err
	}
	return ensureOn(ctx, client, org, in)
}

func ensureOn(ctx context.Context, client taskPlane, org string, in Input) (Result, error) {
	switch strings.TrimSpace(StringParam(in, "kind")) {
	case "epic":
		return ensureEpic(ctx, client, org, in)
	case "milestone":
		return ensureMilestone(ctx, client, org, in)
	case "task":
		return ensureTask(ctx, client, org, in)
	default:
		return Result{}, fmt.Errorf("kind must be epic, milestone or task, got %q", StringParam(in, "kind"))
	}
}

// ensureEpic finds or creates an epic by slug. A taken slug is an answer, not
// an error — the plane already returns the existing epic, which is exactly the
// idempotent caller's intent.
func ensureEpic(ctx context.Context, c taskPlane, org string, in Input) (Result, error) {
	slug := StringParam(in, "slug")
	name := StringParam(in, "name")
	epic, err := c.CreateEpic(ctx, org, remotestate.EpicCreateRequest{Name: name, Slug: slug})
	if existing := remotestate.ExistingEpicOf(err); existing != nil {
		return Result{Outputs: map[string]string{
			"id": existing.ID, "key": existing.Key, "existed": "true",
		}}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("ensuring epic %q: %w", nameOrSlug(name, slug), err)
	}
	return Result{Outputs: map[string]string{
		"id": epic.ID, "key": epic.Key, "existed": "false",
	}}, nil
}

// ensureMilestone finds a milestone by NAME WITHIN ITS EPIC, or creates it.
// Name-within-epic is the identity a bootstrap can reproduce: it re-runs a
// phase called 03-infrastructure and must find the milestone it made last time.
func ensureMilestone(ctx context.Context, c taskPlane, org string, in Input) (Result, error) {
	epicRef := StringParam(in, "epic")
	if epicRef == "" {
		return Result{}, fmt.Errorf("a milestone needs its `epic`")
	}
	name := StringParam(in, "name")
	view, err := c.GetEpic(ctx, org, epicRef)
	if err != nil {
		return Result{}, fmt.Errorf("reading epic %q: %w", epicRef, err)
	}
	for _, m := range view.Milestones {
		if m.Name == name {
			return Result{Outputs: map[string]string{"id": m.ID, "key": m.ID, "existed": "true"}}, nil
		}
	}
	created, err := c.CreateMilestone(ctx, org, epicRef, remotestate.MilestoneCreateRequest{
		Name:         name,
		ExitCriteria: StringListParam(in, "exitCriteria"),
	})
	if err != nil {
		return Result{}, fmt.Errorf("creating milestone %q in %q: %w", name, epicRef, err)
	}
	return Result{Outputs: map[string]string{"id": created.ID, "key": created.ID, "existed": "false"}}, nil
}

// ensureTask finds a task by TITLE WITHIN ITS EPIC, or creates it contracted.
//
// The contract is not an embellishment. A task created without one parks at
// `in_review` after its merge — gates unknown to us are not gates passed — so
// a bootstrap that creates its tasks seconds before their PRs would leave
// every landing it makes un-folded forever. `contract:` names the document
// that says what finishing means, and a baseline keeps that document beside
// its blueprint, which is why the path resolves against the BASELINE.
func ensureTask(ctx context.Context, c taskPlane, org string, in Input) (Result, error) {
	title := StringParam(in, "name")
	epicRef := StringParam(in, "epic")

	// Read the document BEFORE anything is created. A malformed contract must
	// not cost a minted key — the same discipline `orun task create` keeps.
	var template *taskfile.Document
	if path := PathParam(in, "contract"); path != "" {
		doc, err := taskfile.LoadTemplate(path)
		if err != nil {
			return Result{}, fmt.Errorf("task %q: contract: %w", title, err)
		}
		template = doc
	}

	if epicRef != "" {
		list, err := c.ListTasksWhere(ctx, org, remotestate.TaskListFilter{Epic: epicRef})
		if err == nil && list != nil {
			for _, t := range list.Tasks {
				if t.TitleMirror == title {
					// Re-attach on the found path too. A bootstrap re-runs, and
					// the run that made this task may have predated its
					// contract — or failed between the create and the attach.
					// Attaching the same bytes is a no-op by content hash, so
					// the re-run heals instead of leaving a parked landing.
					if err := attachContract(ctx, c, org, t.Key, template); err != nil {
						return Result{}, fmt.Errorf("task %q: %w", title, err)
					}
					return Result{Outputs: map[string]string{"id": t.ID, "key": t.Key, "existed": "true"}}, nil
				}
			}
		}
	}
	created, err := c.CreateTask(ctx, org, remotestate.TaskCreateRequest{
		MintPrefix:  StringParam(in, "prefix"),
		TitleMirror: title,
		Brief:       StringParam(in, "brief"),
		Epic:        epicRef,
		Milestone:   StringParam(in, "milestone"),
	})
	if err != nil {
		return Result{}, fmt.Errorf("creating task %q: %w", title, err)
	}
	if err := attachContract(ctx, c, org, created.Key, template); err != nil {
		return Result{}, fmt.Errorf("task %q: %w", created.Key, err)
	}
	return Result{Outputs: map[string]string{"id": created.ID, "key": created.Key, "existed": "false"}}, nil
}

// attachContract seals the authored document and attaches it to the key,
// through the same helper `orun task create` uses. A nil template means the
// caller declared none, which stays legal — an uncontracted task is an honest
// state, just not one a bootstrap wants.
func attachContract(ctx context.Context, c taskPlane, org, key string, template *taskfile.Document) error {
	_, err := taskfile.Attach(ctx, c, org, key, template)
	return err
}

func runTaskRollup(ctx context.Context, in Input) (Result, error) {
	client, org, err := cloudClient(ctx, in)
	if err != nil {
		return Result{}, err
	}
	return rollupOn(ctx, client, org, in)
}

func rollupOn(ctx context.Context, client taskPlane, org string, in Input) (Result, error) {
	epicRef := StringParam(in, "epic")
	view, err := client.GetEpic(ctx, org, epicRef)
	if err != nil {
		return Result{}, fmt.Errorf("reading epic %q: %w", epicRef, err)
	}
	out := map[string]string{"total": "0", "done": "0", "blocked": "0", "complete": "false"}
	if view.Rollup != nil {
		r := view.Rollup
		out["total"] = fmt.Sprint(r.Total)
		out["done"] = fmt.Sprint(r.Done)
		out["blocked"] = fmt.Sprint(r.Blocked)
		// A rollup is never an error, even when work is outstanding. The
		// bootstrap's own gate is whether its phases placed and verified; a
		// landing the observation drain has not folded yet is a cron that has
		// not run, not a failed build.
		out["complete"] = fmt.Sprint(r.Total > 0 && r.Done == r.Total)
	}
	return Result{Outputs: out}, nil
}

func nameOrSlug(name, slug string) string {
	if slug != "" {
		return slug
	}
	return name
}
