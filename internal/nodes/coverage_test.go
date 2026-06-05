package nodes

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/objectstore"
)

var errBoom = errors.New("boom")

// failStore wraps a MemStore and starts failing PutBlob/PutTree at a configured
// call count, so the assemblers' error-return branches are exercised.
type failStore struct {
	real       *objectstore.MemStore
	blobFailAt int // 1-indexed; <=0 = never. Fails on this call and after.
	treeFailAt int
	nb, nt     int
}

func (f *failStore) PutBlob(ctx context.Context, data []byte) (objectstore.ObjectID, error) {
	f.nb++
	if f.blobFailAt > 0 && f.nb >= f.blobFailAt {
		return "", errBoom
	}
	return f.real.PutBlob(ctx, data)
}

func (f *failStore) PutTree(ctx context.Context, entries []objectstore.TreeEntry) (objectstore.ObjectID, error) {
	f.nt++
	if f.treeFailAt > 0 && f.nt >= f.treeFailAt {
		return "", errBoom
	}
	return f.real.PutTree(ctx, entries)
}

func newFail(blobAt, treeAt int) *failStore {
	return &failStore{real: objectstore.NewMemStore(""), blobFailAt: blobAt, treeFailAt: treeAt}
}

func TestAssembleValidationBranches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	plan := []byte(`{"p":1}`)
	mkExec := func(jobs []JobInput, status string) ExecutionInput {
		return ExecutionInput{
			Execution: ExecutionRun{ExecutionID: "exec_1", RevisionID: goodID("d"), Status: status, StartedAt: time.Now()},
			Jobs:      jobs,
		}
	}
	badStep := []JobInput{{Record: JobRun{JobID: "j", Folder: "j-1", Status: StatusSucceeded},
		Attempts: []AttemptInput{{Record: JobAttempt{Attempt: 1, Status: StatusSucceeded},
			Steps: []StepInput{{Record: StepAttempt{StepID: "s", Status: "weird"}}}}}}}
	badAttempt := []JobInput{{Record: JobRun{JobID: "j", Folder: "j-1", Status: StatusSucceeded},
		Attempts: []AttemptInput{{Record: JobAttempt{Attempt: 0, Status: StatusSucceeded}}}}}
	badJob := []JobInput{{Record: JobRun{JobID: "j", Folder: "j-1", Status: "weird"}}}

	checks := []func() error{
		func() error { _, e := AssembleTrigger(ctx, mem(), TriggerOccurrence{TriggerID: "no-prefix", TriggerName: "n", RevisionID: goodID("c"), CreatedAt: time.Now()}); return e },
		func() error { _, e := AssembleRevision(ctx, mem(), PlanRevision{Scope: RevisionScope{Mode: "weird"}}, plan); return e },
		func() error {
			_, e := AssembleCatalog(ctx, mem(), CatalogSnapshot{SourceID: "bad"},
				[]ComponentManifest{{Identity: ComponentIdentity{ComponentKey: "ns/repo/a", Name: "a"}}}, nil, ImpactOwnership{}, nil)
			return e
		},
		func() error {
			_, e := AssembleCatalog(ctx, mem(), CatalogSnapshot{SourceID: goodID("a")}, nil, []CatalogGraph{{EdgeKind: ""}}, ImpactOwnership{}, nil)
			return e
		},
		func() error { _, e := AssembleExecution(ctx, mem(), mkExec(badStep, StatusSucceeded)); return e },
		func() error { _, e := AssembleExecution(ctx, mem(), mkExec(badAttempt, StatusSucceeded)); return e },
		func() error { _, e := AssembleExecution(ctx, mem(), mkExec(badJob, StatusSucceeded)); return e },
		func() error { _, e := AssembleExecution(ctx, mem(), mkExec(nil, "weird")); return e },
	}
	for i, c := range checks {
		if err := c(); !errors.Is(err, ErrInvalid) {
			t.Fatalf("validation check[%d] = %v, want ErrInvalid", i, err)
		}
	}
}

func TestPutNamedTreeBlobError(t *testing.T) {
	t.Parallel()
	// An execution with an event blob, failing the first PutBlob, drives the
	// putNamedTree PutBlob error path once the step/job blobs are bypassed by
	// putting events first via a high fail point is awkward — instead fail every
	// blob and route through an events-only execution (no jobs).
	exec := ExecutionInput{
		Execution: ExecutionRun{ExecutionID: "exec_1", RevisionID: goodID("d"), Status: StatusSucceeded, StartedAt: time.Now()},
		Events:    []NamedBlob{{Name: "1-x.json", Data: []byte("{}")}},
	}
	if _, e := AssembleExecution(context.Background(), newFail(1, 0), exec); !errors.Is(e, errBoom) {
		t.Fatalf("events blob fail = %v, want errBoom", e)
	}
}

func TestSanitizeSegment(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"build":   "build",
		"a/b:c":   "a-b-c",
		"api@v.1": "api-v.1",
		"***":     "x",
		"":        "x",
		"-._x_-":  "x",
	}
	for in, want := range cases {
		if got := sanitizeSegment(in); got != want {
			t.Fatalf("sanitizeSegment(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAssembleErrorPropagation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := SourceSnapshot{Scope: ScopeMain}
	trg := TriggerOccurrence{TriggerID: "trg_1", TriggerName: "n", RevisionID: goodID("c"), CreatedAt: time.Now()}
	rev := PlanRevision{Scope: RevisionScope{Mode: "full"}}
	plan := []byte(`{"p":1}`)
	manifests := []ComponentManifest{{Identity: ComponentIdentity{ComponentKey: "ns/repo/a", Name: "a"}}}
	graphs := []CatalogGraph{{EdgeKind: "dependencies"}}
	cat := CatalogSnapshot{SourceID: goodID("a")}
	exec := ExecutionInput{
		Execution: ExecutionRun{ExecutionID: "exec_1", RevisionID: goodID("d"), Status: StatusSucceeded, StartedAt: time.Now()},
		Jobs: []JobInput{{
			Record:   JobRun{JobID: "j", Folder: "j-1", Status: StatusSucceeded},
			Attempts: []AttemptInput{{Record: JobAttempt{Attempt: 1, Status: StatusSucceeded}, Steps: []StepInput{{Record: StepAttempt{StepID: "s", Status: StatusSucceeded}, Log: []byte("log")}}}},
		}},
	}

	type tc struct {
		name string
		run  func(s store) error
	}
	run := func(name string, fn func(s store) error) tc { return tc{name, fn} }
	cases := []tc{
		run("source-blob", func(s store) error { _, e := AssembleSource(ctx, s, src); return e }),
		run("trigger-blob", func(s store) error { _, e := AssembleTrigger(ctx, s, trg); return e }),
		run("revision-blob", func(s store) error { _, e := AssembleRevision(ctx, s, rev, plan); return e }),
		run("catalog-blob", func(s store) error { _, e := AssembleCatalog(ctx, s, cat, manifests, graphs, ImpactOwnership{}, nil); return e }),
		run("execution-blob", func(s store) error { _, e := AssembleExecution(ctx, s, exec); return e }),
	}
	// Blob failures (fail from the first PutBlob onward).
	for _, c := range cases {
		if err := c.run(newFail(1, 0)); !errors.Is(err, errBoom) {
			t.Fatalf("%s blob-fail = %v, want errBoom", c.name, err)
		}
	}
	// Tree failures (blobs succeed, first PutTree fails). Source/Trigger have no
	// trees, so they succeed here — only the tree-building assemblers error.
	for _, c := range []tc{cases[2], cases[3], cases[4]} {
		if err := c.run(newFail(0, 1)); !errors.Is(err, errBoom) {
			t.Fatalf("%s tree-fail = %v, want errBoom", c.name, err)
		}
	}
	// Deeper blob failure points to cover later PutBlob returns.
	if _, e := AssembleRevision(ctx, newFail(2, 0), rev, plan); !errors.Is(e, errBoom) {
		t.Fatalf("revision second-blob fail = %v", e)
	}
	if _, e := AssembleCatalog(ctx, newFail(3, 0), cat, manifests, graphs, ImpactOwnership{}, nil); !errors.Is(e, errBoom) {
		t.Fatalf("catalog cat-blob fail = %v", e)
	}
	if _, e := AssembleExecution(ctx, newFail(2, 0), exec); !errors.Is(e, errBoom) {
		t.Fatalf("execution step-blob fail = %v", e)
	}
	if _, e := AssembleExecution(ctx, newFail(0, 2), exec); !errors.Is(e, errBoom) {
		t.Fatalf("execution attempt-tree fail = %v", e)
	}
}
