package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/execseal"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/nodewriter"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

// TestResumeJobsFromPriorRun seals a prior execution (one job succeeded, one
// failed) into a workspace's object graph and verifies resumeJobsFromPriorRun
// returns only the succeeded job, as a runner JobState with its step statuses.
func TestResumeJobsFromPriorRun(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "ws")
	omRoot := filepath.Join(root, ".orun", "objectmodel")
	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: omRoot})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: omRoot, Writer: "test"})
	if err != nil {
		t.Fatalf("refs: %v", err)
	}
	revID, err := nodes.AssembleRevision(ctx, store,
		nodes.PlanRevision{Scope: nodes.RevisionScope{Mode: "full"}, JobCount: 2}, []byte(`{"plan":"A"}`))
	if err != nil {
		t.Fatalf("AssembleRevision: %v", err)
	}
	if err := refs.Update(ctx, "revisions/latest", "", string(revID)); err != nil {
		t.Fatalf("ref: %v", err)
	}

	sealer := execseal.New(nodewriter.New(store, refs))
	if _, err := sealer.Seal(ctx, execseal.SealInput{
		RevisionID: revID, ExecutionID: "prior-1", Status: nodes.StatusFailed, StartedAt: time.Now(),
		Jobs: []nodes.JobInput{
			{Record: nodes.JobRun{JobID: "a@deploy", Folder: "j-a", Status: nodes.StatusSucceeded},
				Attempts: []nodes.AttemptInput{{Record: nodes.JobAttempt{Attempt: 1, Status: nodes.StatusSucceeded},
					Steps: []nodes.StepInput{{Record: nodes.StepAttempt{StepID: "build", Status: nodes.StatusSucceeded}, Log: []byte("prior build log")}}}}},
			{Record: nodes.JobRun{JobID: "b@deploy", Folder: "j-b", Status: nodes.StatusFailed},
				Attempts: []nodes.AttemptInput{{Record: nodes.JobAttempt{Attempt: 1, Status: nodes.StatusFailed},
					Steps: []nodes.StepInput{{Record: nodes.StepAttempt{StepID: "build", Status: nodes.StatusFailed}}}}}},
		},
	}); err != nil {
		t.Fatalf("seal: %v", err)
	}

	// storeDir() resolves to "." with no intent, so openObjectReader reads
	// ./.orun/objectmodel — chdir into the workspace.
	t.Chdir(root)

	resume, logs := resumeJobsFromPriorRun("prior-1")
	if len(resume) != 1 {
		t.Fatalf("resume = %v; want only the one succeeded job", resume)
	}
	js := resume["a@deploy"]
	if js == nil || js.Status != "completed" || js.Steps["build"] != "completed" {
		t.Fatalf("a@deploy resume entry = %+v; want completed with build=completed", js)
	}
	if _, ok := resume["b@deploy"]; ok {
		t.Fatalf("failed job b@deploy must not be resumed")
	}
	// The succeeded job's prior step log is carried forward for the resumed seal.
	if got := string(logs["a@deploy"]["build"]); got != "prior build log" {
		t.Fatalf("carried step log = %q; want %q", got, "prior build log")
	}
	if _, ok := logs["b@deploy"]; ok {
		t.Fatalf("failed job b@deploy must not carry logs")
	}

	// An unknown exec id has no prior run → nil (a fresh run, nothing skipped).
	if r, _ := resumeJobsFromPriorRun("does-not-exist"); r != nil {
		t.Fatalf("unknown id resume = %v; want nil", r)
	}
	// Empty exec id → nil.
	if r, _ := resumeJobsFromPriorRun(""); r != nil {
		t.Fatalf("empty id resume = %v; want nil", r)
	}
}
