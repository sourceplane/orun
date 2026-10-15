package runner

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/sourceplane/orun/internal/executor"
	"github.com/sourceplane/orun/internal/model"
)

// cacheFillingExecutor is a build that reads its cache directory at the
// start of the step and fills it before the step ends, like a framework's
// build cache under the job's tree.
type cacheFillingExecutor struct {
	mu         sync.Mutex
	sawAtStart map[string]string // job id -> the cache entry present when the step began
	fail       bool
	content    string
}

func (*cacheFillingExecutor) Name() string                       { return "cache-filling" }
func (*cacheFillingExecutor) Prepare(executor.ExecContext) error { return nil }
func (*cacheFillingExecutor) Cleanup(executor.ExecContext) error { return nil }
func (e *cacheFillingExecutor) RunStep(ctx executor.ExecContext, job model.PlanJob, _ model.PlanStep) (string, error) {
	entry := filepath.Join(ctx.WorkDir, ".next", "cache", "entry")
	data, _ := os.ReadFile(entry)
	e.mu.Lock()
	if e.sawAtStart == nil {
		e.sawAtStart = map[string]string{}
	}
	e.sawAtStart[job.ID] = string(data)
	e.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(entry, []byte(e.content), 0o644); err != nil {
		return "", err
	}
	if e.fail {
		return "boom", fmt.Errorf("build failed")
	}
	return "ok", nil
}

func cachedBuildPlan(jobID string) *model.Plan {
	return &model.Plan{Jobs: []model.PlanJob{{ID: jobID, Name: jobID, Component: "web", Environment: "stage", Steps: []model.PlanStep{
		{ID: "build", Name: "build", Run: "next build", Caches: []model.CacheDecl{{Name: "next-web", Path: ".next/cache"}}},
	}}}}
}

func runCachedBuild(t *testing.T, store string, ex *cacheFillingExecutor, jobID string) (error, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(root, false, io.Discard, io.Discard, false, "", false, false,
		ex, executor.RuntimeContext{}, "exec_"+jobID, 1, nil, "")
	r.Isolation = IsolationNone
	r.DeclaredCacheRoot = store
	return r.Run(cachedBuildPlan(jobID)), root
}

func TestDeclaredCachesAreSavedAfterAJobAndRestoredIntoTheNext(t *testing.T) {
	store := t.TempDir()

	first := &cacheFillingExecutor{content: "built by run 1"}
	if err, _ := runCachedBuild(t, store, first, "a"); err != nil {
		t.Fatal(err)
	}
	if first.sawAtStart["a"] != "" {
		t.Fatalf("the first run starts with no cache: %q", first.sawAtStart["a"])
	}
	saved, err := os.ReadFile(filepath.Join(store, "next-web", "entry"))
	if err != nil || string(saved) != "built by run 1" {
		t.Fatalf("the cache the job left is the machine's copy: %q, %v", saved, err)
	}

	// A fresh checkout: the tree has no cache, the machine's copy fills it.
	second := &cacheFillingExecutor{content: "built by run 2"}
	if err, _ := runCachedBuild(t, store, second, "b"); err != nil {
		t.Fatal(err)
	}
	if second.sawAtStart["b"] != "built by run 1" {
		t.Fatalf("the second run finds the first's cache in its tree: %q", second.sawAtStart["b"])
	}
	saved, _ = os.ReadFile(filepath.Join(store, "next-web", "entry"))
	if string(saved) != "built by run 2" {
		t.Fatalf("the second run's cache replaces the copy: %q", saved)
	}

	// A failed job keeps nothing: the copy stays what the last success left.
	third := &cacheFillingExecutor{content: "half built by run 3", fail: true}
	_, _ = runCachedBuild(t, store, third, "c") // the job fails; the run reports it in its summary
	saved, _ = os.ReadFile(filepath.Join(store, "next-web", "entry"))
	if string(saved) != "built by run 2" {
		t.Fatalf("a failed job does not save its cache: %q", saved)
	}
}

func TestDeclaredCachesLeaveATreeThatBroughtItsOwn(t *testing.T) {
	store := t.TempDir()
	if err := os.MkdirAll(filepath.Join(store, "next-web"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, "next-web", "entry"), []byte("machine"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".next", "cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".next", "cache", "entry"), []byte("checkout"), 0o644); err != nil {
		t.Fatal(err)
	}
	ex := &cacheFillingExecutor{content: "built"}
	r := NewRunner(root, false, io.Discard, io.Discard, false, "", false, false,
		ex, executor.RuntimeContext{}, "exec_own", 1, nil, "")
	r.Isolation = IsolationNone
	r.DeclaredCacheRoot = store
	if err := r.Run(cachedBuildPlan("own")); err != nil {
		t.Fatal(err)
	}
	if ex.sawAtStart["own"] != "checkout" {
		t.Fatalf("a tree with its own cache keeps it: %q", ex.sawAtStart["own"])
	}
}
