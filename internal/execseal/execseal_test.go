package execseal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/clock"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/nodewriter"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

func revID() objectstore.ObjectID { return objectstore.ObjectID("sha256:" + strings.Repeat("a", 64)) }

func rig(t *testing.T) (*Sealer, *objectstore.MemStore, *refstore.LocalRefStore) {
	t.Helper()
	store := objectstore.NewMemStore("")
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: t.TempDir(), Clock: clock.Fixed{}})
	if err != nil {
		t.Fatalf("refs: %v", err)
	}
	w := nodewriter.New(store, refs)
	var n int
	return New(w, WithExecIDGen(func() string { n++; return fmt.Sprintf("exec_%03d", n) })), store, refs
}

func sampleJobs() []nodes.JobInput {
	return []nodes.JobInput{
		{
			Record:   nodes.JobRun{JobID: "a@deploy", Folder: "j-1", Status: nodes.StatusSucceeded},
			Attempts: []nodes.AttemptInput{{Record: nodes.JobAttempt{Attempt: 1, Status: nodes.StatusSucceeded}, Steps: []nodes.StepInput{{Record: nodes.StepAttempt{StepID: "build", Status: nodes.StatusSucceeded}, Log: []byte("log output")}}}},
		},
		{
			Record: nodes.JobRun{JobID: "b@deploy", Folder: "j-2", Status: nodes.StatusFailed},
			Attempts: []nodes.AttemptInput{{Record: nodes.JobAttempt{Attempt: 1, Status: nodes.StatusFailed}, Steps: []nodes.StepInput{
				{Record: nodes.StepAttempt{StepID: "x", Status: nodes.StatusFailed}},
				{Record: nodes.StepAttempt{StepID: "y", Status: nodes.StatusSucceeded}},
			}}},
		},
	}
}

func blobBody(t *testing.T, s *objectstore.MemStore, tree objectstore.ObjectID, name string) string {
	t.Helper()
	ctx := context.Background()
	entries, err := s.GetTree(ctx, tree)
	if err != nil {
		t.Fatalf("GetTree: %v", err)
	}
	for _, e := range entries {
		if e.Name == name {
			_, body, err := s.Get(ctx, e.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			return string(body)
		}
	}
	t.Fatalf("entry %q not found", name)
	return ""
}

func TestSealHappyPath(t *testing.T) {
	t.Parallel()
	s, store, refs := rig(t)
	ctx := context.Background()
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	id, err := s.Seal(ctx, SealInput{
		RevisionID: revID(), TriggerID: "trg_1", ExecutionKey: "run-001",
		Status: nodes.StatusSucceeded, StartedAt: now, FinishedAt: now, Jobs: sampleJobs(),
	})
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	body := blobBody(t, store, id, "execution.json")
	for _, want := range []string{
		`"executionId":"exec_001"`, `"jobsTotal":2`, `"jobsSucceeded":1`, `"jobsFailed":1`, `"stepsTotal":3`, `"j-1"`, `"j-2"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("execution.json missing %q:\n%s", want, body)
		}
	}
	// executions/latest points at the sealed id.
	if r, _ := refs.Read(ctx, "executions/latest"); r.Target != string(id) {
		t.Fatalf("executions/latest = %s, want %s", r.Target, id)
	}
}

func TestSealIdempotent(t *testing.T) {
	t.Parallel()
	s, _, _ := rig(t)
	ctx := context.Background()
	in := SealInput{RevisionID: revID(), ExecutionID: "exec_fixed", ExecutionKey: "run-001", Status: nodes.StatusSucceeded, Jobs: sampleJobs()}
	a, err := s.Seal(ctx, in)
	if err != nil {
		t.Fatalf("a: %v", err)
	}
	b, err := s.Seal(ctx, in)
	if err != nil {
		t.Fatalf("b: %v", err)
	}
	if a != b {
		t.Fatalf("re-seal not idempotent: %s vs %s", a, b)
	}
}

func TestSealRejectsNonTerminalAndBadRevision(t *testing.T) {
	t.Parallel()
	s, _, _ := rig(t)
	ctx := context.Background()
	if _, err := s.Seal(ctx, SealInput{RevisionID: revID(), Status: nodes.StatusRunning}); !errors.Is(err, nodes.ErrInvalid) {
		t.Fatalf("non-terminal status = %v, want ErrInvalid", err)
	}
	if _, err := s.Seal(ctx, SealInput{RevisionID: "bad", Status: nodes.StatusSucceeded}); !errors.Is(err, objectstore.ErrInvalid) {
		t.Fatalf("bad revisionId = %v, want ErrInvalid", err)
	}
}

func TestSealAssembleErrorPropagates(t *testing.T) {
	t.Parallel()
	s, _, _ := rig(t)
	// A job with an invalid status makes AssembleExecution validation fail.
	jobs := []nodes.JobInput{{Record: nodes.JobRun{JobID: "x", Folder: "j-1", Status: "weird"}}}
	if _, err := s.Seal(context.Background(), SealInput{RevisionID: revID(), Status: nodes.StatusSucceeded, Jobs: jobs}); err == nil {
		t.Fatalf("expected assemble error for bad job status")
	}
}

// failRefs errors on Read to make ref publication fail.
type failRefs struct{}

func (failRefs) Read(context.Context, string) (refstore.Ref, error) {
	return refstore.Ref{}, errors.New("read boom")
}
func (failRefs) Update(context.Context, string, string, string) error { return nil }
func (failRefs) List(context.Context, string) ([]string, error)       { return nil, nil }
func (failRefs) Delete(context.Context, string) error                 { return nil }

func TestSealRefPublishErrorPropagates(t *testing.T) {
	t.Parallel()
	w := nodewriter.New(objectstore.NewMemStore(""), failRefs{})
	s := New(w)
	if _, err := s.Seal(context.Background(), SealInput{RevisionID: revID(), Status: nodes.StatusSucceeded, Jobs: sampleJobs()}); err == nil {
		t.Fatalf("expected ref publish error")
	}
}

func TestSummarizeEmpty(t *testing.T) {
	t.Parallel()
	if got := summarize(nil); got.JobsTotal != 0 || got.StepsTotal != 0 {
		t.Fatalf("empty summary = %+v", got)
	}
}
