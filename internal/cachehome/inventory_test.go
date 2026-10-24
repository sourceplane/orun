package cachehome

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/fslock"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInventory_ListsEveryCacheWithSizeAndAge(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORUN_TOOL_CACHE", "")
	t.Setenv("RUNNER_TOOL_CACHE", "")
	root := filepath.Join(home, ".orun")

	write(t, filepath.Join(root, "actions", "actions", "setup-node", "abc123", ".ready"), "")
	write(t, filepath.Join(root, "actions", "actions", "setup-node", "abc123", "action.yml"), "name: n\n")
	write(t, filepath.Join(root, "actions", "actions", "setup-node", "refs", "v4"), "abc123 1\n")
	write(t, filepath.Join(root, "actions", "actions", "setup-node", "def456", "action.yml"), "half-written, no .ready\n")
	write(t, filepath.Join(root, "tool-cache", "node", "22.1.0", "x64", "bin", "node"), "binary")
	write(t, filepath.Join(root, "actions", "shared", "pnpm-store", "v10", "files", "a"), "aaaa")
	write(t, filepath.Join(root, "actions", "shared", "caches", "next-web", "cache", "x"), "xx")
	write(t, filepath.Join(root, "cache", "compositions", "0123abcd", "stack.yaml"), "x")
	write(t, filepath.Join(root, "cache", "compositions", "0123abcd-tmp-1", "stack.yaml"), "x")
	write(t, filepath.Join(root, "actions", "shared", "actions-cache", "e1", "meta.json"), `{"key":"deps-v1","paths":["ws/node_modules"]}`)
	write(t, filepath.Join(root, "actions", "shared", "actions-cache", "e1", "tree", "ws", "node_modules", "x"), "xxx")
	write(t, filepath.Join(root, "actions", "shared", "actions-cache", "e2-tmp-1", "meta.json"), `{"key":"half"}`)

	checkout := t.TempDir()
	write(t, filepath.Join(checkout, ".orun", "runs", "exec_1", "a", "work", "f"), "run")
	write(t, filepath.Join(checkout, ".orun", "objectmodel", "cache", "resolve", "k1"), "{}")
	write(t, filepath.Join(checkout, ".orun", "objectmodel", "cache", "resolve", "k2.tmp"), "{}")

	entries, err := Inventory(checkout)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Entry{}
	for _, e := range entries {
		got[string(e.Kind)+":"+e.Name] = e
	}
	for _, want := range []string{
		"actions:actions/setup-node/abc123", "tool-cache:node/22.1.0", "shared:pnpm-store",
		"declared:next-web", "compositions:0123abcd", "runs:exec_1", "resolve-memo:k1", "actions-cache:deps-v1",
	} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing %s in %v", want, keys(got))
		}
	}
	for _, unwanted := range []string{"actions:actions/setup-node/def456", "compositions:0123abcd-tmp-1", "resolve-memo:k2.tmp", "shared:caches", "shared:actions-cache", "actions-cache:e2-tmp-1"} {
		if _, ok := got[unwanted]; ok {
			t.Errorf("%s is not an entry", unwanted)
		}
	}
	if len(got) != 8 {
		t.Fatalf("8 entries, got %d: %v", len(got), keys(got))
	}
	if e := got["actions-cache:deps-v1"]; e.Size != int64(3+len(`{"key":"deps-v1","paths":["ws/node_modules"]}`)) {
		t.Fatalf("an actions/cache entry is named by its key and sized by its tree and record: %+v", e)
	}
	if e := got["shared:pnpm-store"]; e.Size != 4 || e.LastUsed.IsZero() || e.InUse {
		t.Fatalf("a store's size is its files', aged by mtime, not in use: %+v", e)
	}
	if e := got["actions:actions/setup-node/abc123"]; e.Size != int64(len("name: n\n")) {
		t.Fatalf("the action's size counts its files, not the ref: %+v", e)
	}

	// Touch moves an entry's last use forward and the stamp wins over mtime.
	stamped := got["compositions:0123abcd"]
	past := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(stamped.Path, past, past); err != nil {
		t.Fatal(err)
	}
	if LastUsed(stamped.Path).After(past.Add(time.Second)) {
		t.Fatal("an untouched entry is aged by its mtime")
	}
	Touch(stamped.Path)
	if time.Since(LastUsed(stamped.Path)) > time.Minute {
		t.Fatal("a touched entry was used now")
	}
}

func keys(m map[string]Entry) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestInventory_ALiveLockMarksTheEntryInUseAndRemoveLeavesIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORUN_TOOL_CACHE", "")
	t.Setenv("RUNNER_TOOL_CACHE", "")
	root := filepath.Join(home, ".orun")
	write(t, filepath.Join(root, "actions", "shared", "caches", "held", "f"), "1")
	write(t, filepath.Join(root, "actions", "shared", "caches", "free", "f"), "1")
	write(t, filepath.Join(root, "actions", "o", "r", "sha1", ".ready"), "")
	write(t, filepath.Join(root, "actions", "o", "r", "sha1.lock"), "123 1\n") // a fetch in progress, just touched

	lock, ok, err := fslock.TryAcquire(filepath.Join(root, "actions", "shared", "caches", "held.lock"))
	if err != nil || !ok {
		t.Fatal(err)
	}
	defer lock.Release()

	entries, err := Inventory("")
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Entry{}
	for _, e := range entries {
		byName[e.Name] = e
	}
	if !byName["held"].InUse || byName["free"].InUse || !byName["o/r/sha1"].InUse {
		t.Fatalf("held and the action being fetched are in use, free is not: %+v", entries)
	}
	if err := Remove(byName["held"]); err == nil {
		t.Fatal("an entry in use is not removed")
	}
	if err := Remove(byName["free"]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(byName["free"].Path); !os.IsNotExist(err) {
		t.Fatal("free was removed")
	}
	if _, err := os.Stat(byName["held"].Path); err != nil {
		t.Fatal("held stays")
	}
}

func TestOlderThanAndOverBudget(t *testing.T) {
	now := time.Now()
	entries := []Entry{
		{Kind: KindActions, Name: "old", Size: 10, LastUsed: now.Add(-40 * 24 * time.Hour)},
		{Kind: KindToolCache, Name: "older-in-use", Size: 50, LastUsed: now.Add(-60 * 24 * time.Hour), InUse: true},
		{Kind: KindCompositions, Name: "recent", Size: 30, LastUsed: now.Add(-time.Hour)},
		{Kind: KindRuns, Name: "run-old", Size: 100, LastUsed: now.Add(-40 * 24 * time.Hour)},
	}
	old := OlderThan(entries, 30*24*time.Hour, now)
	if len(old) != 3 || old[0].Name != "old" || old[1].Name != "older-in-use" || old[2].Name != "run-old" {
		t.Fatalf("older than 30d: %+v", old)
	}
	// Machine-wide total is 90; a 50-byte budget evicts the oldest free entry
	// (old, 10) and then recent (30), never the one in use, and never a run.
	over := OverBudget(entries, 50)
	if len(over) != 2 || over[0].Name != "old" || over[1].Name != "recent" {
		t.Fatalf("over a 50-byte budget: %+v", over)
	}
	if over := OverBudget(entries, 90); len(over) != 0 {
		t.Fatalf("under budget removes nothing: %+v", over)
	}
}
