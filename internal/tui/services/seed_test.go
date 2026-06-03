package services

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/runworktree"
)

// seed_test.go provides the shared object-model seeding the TUI service tests
// use in place of the old legacy-store fixtures. It writes a real revision tree
// (carrying plan.json so PlanSummary resolves a name + components) and drives a
// runworktree working tree through project/log/seal — the same path the runner
// and `orun run` take. Tests configure the service with
// LiveServiceConfig{ObjectModelRoot: orunDir(dir)} and seed under it.

// orunDir returns the .orun directory the TUI service treats as ObjectModelRoot.
func orunDir(dir string) string { return filepath.Join(dir, ".orun") }

// objectModelRoot returns the object graph root (.orun/objectmodel) the seeding
// writes into — the directory holding objects/, refs/, and run/.
func objectModelRoot(dir string) string { return filepath.Join(orunDir(dir), "objectmodel") }

type seedStep struct {
	ID     string
	Status string // node status; defaults to succeeded
	Log    string // optional captured output
}

type seedJob struct {
	ID        string
	Component string
	Status    string // node status; defaults to succeeded
	Steps     []seedStep
}

type seedExec struct {
	ExecID    string
	PlanName  string
	Jobs      []seedJob
	StartedAt time.Time // optional; controls List ordering
	Live      bool      // leave the working tree unsealed (in-flight)
	Status    string    // terminal seal status; defaults to succeeded
}

// seedObjectExecution writes a revision (with plan.json) and an execution into
// the workspace's object graph under dir/.orun/objectmodel, returning the
// (possibly minted) execution id. A sealed execution by default; set Live=true
// to leave an in-flight working tree.
func seedObjectExecution(t *testing.T, dir string, spec seedExec) string {
	t.Helper()
	ctx := context.Background()
	root := objectModelRoot(dir)

	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		t.Fatalf("seed: open store: %v", err)
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Writer: "test"})
	if err != nil {
		t.Fatalf("seed: open refs: %v", err)
	}

	revID := seedRevision(t, store, spec)

	mgr := runworktree.NewManager(store, refs, root)
	wt, err := mgr.Open(ctx, runworktree.OpenInput{
		ExecutionID: spec.ExecID,
		RevisionID:  revID,
		StartedAt:   spec.StartedAt,
	})
	if err != nil {
		t.Fatalf("seed: open working tree: %v", err)
	}

	projected := make([]runworktree.ProjectedJob, 0, len(spec.Jobs))
	for _, j := range spec.Jobs {
		pj := runworktree.ProjectedJob{JobID: j.ID, Status: orDefault(j.Status, nodes.StatusSucceeded)}
		for _, s := range j.Steps {
			pj.Steps = append(pj.Steps, runworktree.ProjectedStep{
				StepID: s.ID,
				Status: orDefault(s.Status, nodes.StatusSucceeded),
			})
		}
		projected = append(projected, pj)
	}
	if err := wt.Project(projected); err != nil {
		t.Fatalf("seed: project: %v", err)
	}
	for _, j := range spec.Jobs {
		for _, s := range j.Steps {
			if s.Log == "" {
				continue
			}
			if err := wt.SetStepLog(j.ID, s.ID, []byte(s.Log)); err != nil {
				t.Fatalf("seed: set step log: %v", err)
			}
		}
	}

	if spec.Live {
		return wt.ExecutionID()
	}
	if _, err := wt.Seal(ctx, orDefault(spec.Status, nodes.StatusSucceeded), time.Time{}); err != nil {
		t.Fatalf("seed: seal: %v", err)
	}
	return wt.ExecutionID()
}

// seedRevision writes a revision tree containing a compiled plan.json built from
// the seed spec, so PlanSummary resolves the plan name + component set.
func seedRevision(t *testing.T, store *objectstore.LocalStore, spec seedExec) objectstore.ObjectID {
	t.Helper()
	ctx := context.Background()

	type planJob struct {
		ID        string `json:"id"`
		Component string `json:"component"`
	}
	plan := struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Jobs []planJob `json:"jobs"`
	}{}
	plan.Metadata.Name = spec.PlanName
	for _, j := range spec.Jobs {
		plan.Jobs = append(plan.Jobs, planJob{ID: j.ID, Component: j.Component})
	}
	body, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("seed: marshal plan: %v", err)
	}
	planID, err := store.PutBlob(ctx, body)
	if err != nil {
		t.Fatalf("seed: put plan blob: %v", err)
	}
	revID, err := store.PutTree(ctx, []objectstore.TreeEntry{
		{Name: "plan.json", Kind: objectstore.KindBlob, ID: planID},
	})
	if err != nil {
		t.Fatalf("seed: put revision tree: %v", err)
	}
	return revID
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
