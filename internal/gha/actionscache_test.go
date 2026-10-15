package gha

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

// actions/cache runs in the engine against a store under the shared cache
// (specs/orun-cache-efficiency CE7): a save in one workspace restores into
// another by its key, a restore-keys prefix finds the newest entry, the
// main action saves in its post step after a miss, and the outputs answer
// steps.<id>.outputs.* as the real action's do.
func TestEngineRunsActionsCacheNatively(t *testing.T) {
	root := t.TempDir()
	newEngine := func() *Engine {
		return NewEngine(Options{
			CacheDir:     filepath.Join(root, "actions"),
			ToolCacheDir: filepath.Join(root, "tools"),
		})
	}
	ctxFor := func(workspace string) ExecContext {
		if err := os.MkdirAll(workspace, 0o755); err != nil {
			t.Fatal(err)
		}
		return ExecContext{
			Context:      context.Background(),
			WorkspaceDir: workspace,
			WorkDir:      workspace,
			BaseEnv:      map[string]string{"PATH": os.Getenv("PATH"), "HOME": os.Getenv("HOME")},
		}
	}
	write := func(path, content string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(engine *Engine, ctx ExecContext, job model.PlanJob, step model.PlanStep) string {
		t.Helper()
		out, err := engine.RunStep(ctx, job, step)
		if err != nil {
			t.Fatalf("%s: %v\n%s", step.ID, err, out)
		}
		return out
	}

	// Job A builds and saves explicitly (actions/cache/save).
	engine := newEngine()
	wsA := filepath.Join(root, "ws-a")
	ctxA := ctxFor(wsA)
	jobA := model.PlanJob{ID: "a"}
	write(filepath.Join(wsA, "node_modules", "left-pad", "index.js"), "module.exports = 1\n")
	write(filepath.Join(wsA, "out", "a.txt"), "A\n")
	write(filepath.Join(wsA, "out", "skip.log"), "no\n")
	out := run(engine, ctxA, jobA, model.PlanStep{ID: "save", Use: "actions/cache/save@v4", With: map[string]interface{}{
		"key":  "deps-v1",
		"path": "node_modules\nout/*.txt\n",
	}})
	if !strings.Contains(out, "Cache saved with key: deps-v1") {
		t.Fatalf("save: %q", out)
	}
	if _, err := engine.FinalizeJob(ctxA, jobA); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "actions", "shared", "actions-cache")); len(entries) != 3 { // the entry, its lock and its .last-used stamp
		t.Fatalf("one entry in the store: %v", entries)
	}

	// Job B, in a fresh workspace, restores by the exact key.
	wsB := filepath.Join(root, "ws-b")
	ctxB := ctxFor(wsB)
	jobB := model.PlanJob{ID: "b"}
	out = run(engine, ctxB, jobB, model.PlanStep{ID: "deps", Use: "actions/cache@v4", With: map[string]interface{}{
		"key":          "deps-v1",
		"path":         "node_modules\nout/*.txt",
		"restore-keys": "deps-",
	}})
	if !strings.Contains(out, "Cache restored from key: deps-v1") {
		t.Fatalf("restore: %q", out)
	}
	if data, err := os.ReadFile(filepath.Join(wsB, "node_modules", "left-pad", "index.js")); err != nil || string(data) != "module.exports = 1\n" {
		t.Fatalf("node_modules restored into the other workspace: %v %q", err, data)
	}
	if _, err := os.Stat(filepath.Join(wsB, "out", "a.txt")); err != nil {
		t.Fatal("the glob's match was restored")
	}
	if _, err := os.Stat(filepath.Join(wsB, "out", "skip.log")); !os.IsNotExist(err) {
		t.Fatal("a file the glob did not match was not cached")
	}
	verify := run(engine, ctxB, jobB, model.PlanStep{ID: "verify", Shell: "bash",
		Run: `printf '%s|%s|%s' '${{ steps.deps.outputs.cache-hit }}' '${{ steps.deps.outputs.cache-primary-key }}' '${{ steps.deps.outputs.cache-matched-key }}'`})
	if verify != "true|deps-v1|deps-v1" {
		t.Fatalf("outputs of an exact hit: %q", verify)
	}
	if post, err := engine.FinalizeJob(ctxB, jobB); err != nil || post != "" {
		t.Fatalf("an exact hit saves nothing in post: %q %v", post, err)
	}

	// Job C misses its key, restores the prefix match and saves its own
	// key in the post step.
	wsC := filepath.Join(root, "ws-c")
	ctxC := ctxFor(wsC)
	jobC := model.PlanJob{ID: "c"}
	out = run(engine, ctxC, jobC, model.PlanStep{ID: "deps", Use: "actions/cache@v4", With: map[string]interface{}{
		"key":          "deps-v2",
		"path":         "node_modules",
		"restore-keys": "other-\ndeps-\n",
	}})
	if !strings.Contains(out, "Cache restored from key: deps-v1") {
		t.Fatalf("prefix restore: %q", out)
	}
	verify = run(engine, ctxC, jobC, model.PlanStep{ID: "verify", Shell: "bash",
		Run: `printf '%s|%s' '${{ steps.deps.outputs.cache-hit }}' '${{ steps.deps.outputs.cache-matched-key }}'; echo 2 > node_modules/left-pad/index.js`})
	if verify != "false|deps-v1" {
		t.Fatalf("outputs of a prefix hit: %q", verify)
	}
	post, err := engine.FinalizeJob(ctxC, jobC)
	if err != nil || !strings.Contains(post, "Cache saved with key: deps-v2") {
		t.Fatalf("the post step saves the primary key: %q %v", post, err)
	}

	// A new engine (another process) sees both entries; the newest wins
	// the prefix; lookup-only restores nothing; a miss with
	// fail-on-cache-miss fails.
	engine2 := newEngine()
	wsD := filepath.Join(root, "ws-d")
	ctxD := ctxFor(wsD)
	jobD := model.PlanJob{ID: "d"}
	out = run(engine2, ctxD, jobD, model.PlanStep{ID: "look", Use: "actions/cache/restore@v4", With: map[string]interface{}{
		"key":          "deps-v3",
		"path":         "node_modules",
		"restore-keys": "deps-",
		"lookup-only":  "true",
	}})
	if !strings.Contains(out, "can be restored from key: deps-v2") {
		t.Fatalf("the newest prefix match: %q", out)
	}
	if _, err := os.Stat(filepath.Join(wsD, "node_modules")); !os.IsNotExist(err) {
		t.Fatal("lookup-only restores nothing")
	}
	if _, err := engine2.RunStep(ctxD, jobD, model.PlanStep{ID: "miss", Use: "actions/cache/restore@v4", With: map[string]interface{}{
		"key": "nothing", "path": "node_modules", "fail-on-cache-miss": "true",
	}}); err == nil || !strings.Contains(err.Error(), "fail-on-cache-miss") {
		t.Fatalf("a miss with fail-on-cache-miss fails: %v", err)
	}
	out = run(engine2, ctxD, jobD, model.PlanStep{ID: "miss2", Use: "actions/cache/restore@v4", With: map[string]interface{}{
		"key": "nothing", "path": "node_modules",
	}})
	if !strings.Contains(out, "Cache not found for input keys: nothing") {
		t.Fatalf("a plain miss reports: %q", out)
	}
	write(filepath.Join(wsD, "node_modules", "left-pad", "index.js"), "module.exports = 3\n")
	out = run(engine2, ctxD, jobD, model.PlanStep{ID: "again", Use: "actions/cache/save@v4", With: map[string]interface{}{
		"key": "deps-v2", "path": "node_modules",
	}})
	if !strings.Contains(out, "Cache already exists for key: deps-v2") {
		t.Fatalf("an existing key is never replaced: %q", out)
	}
	if _, err := engine2.RunStep(ctxD, jobD, model.PlanStep{ID: "nokey", Use: "actions/cache@v4", With: map[string]interface{}{"path": "x"}}); err == nil {
		t.Fatal("key is required")
	}
	if entry, meta, exact := lookupCacheEntry(engine2.actionsCacheStore(), "deps-v2", nil); !exact || meta.Key != "deps-v2" || entry == "" {
		t.Fatalf("the entry deps-v2 is on disk: %q %+v %v", entry, meta, exact)
	}
	if _, err := engine2.FinalizeJob(ctxD, jobD); err != nil {
		t.Fatal(err)
	}
}

func TestNativeCacheKind(t *testing.T) {
	for raw, want := range map[string]string{
		"actions/cache@v4": "main", "actions/cache@v3.3.2": "main", "actions/cache/restore@v4": "restore",
		"actions/cache/save@v4": "save", "actions/cache/other@v4": "", "actions/setup-node@v4": "", "./.github/actions/cache": "",
	} {
		ref, err := ParseActionReference(raw)
		if err != nil {
			t.Fatal(err)
		}
		kind, ok := nativeCacheKind(ref)
		if kind != want || ok != (want != "") {
			t.Fatalf("%s: kind=%q ok=%v, want %q", raw, kind, ok, want)
		}
	}
}
