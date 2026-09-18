package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/sourceplane/orun/internal/actions"
	"github.com/sourceplane/orun/internal/objectstore"
)

// A phase is on disk only once its own turn comes.
//
// Found by running cirrus's bootstrap for real: with hooks on, the engine wrote
// the WHOLE tree before the first phase's hooks ran, so 01-scaffold's landing
// staged 1075 files — every worker, the console, the terraform — before
// 03-infrastructure had minted a single credential they deploy with.

// sightRunner records, for every hook it runs, which of the watched files were
// on disk at that moment.
type sightRunner struct {
	mu    sync.Mutex
	watch []string
	seen  map[string]string // hook id -> comma-joined files present
	park  string            // a hook id that reports pending
}

func (r *sightRunner) Run(_ context.Context, id string, in ActionInput) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	hook, _ := in.Params["urls"].([]any)
	name := ""
	if len(hook) > 0 {
		name, _ = hook[0].(string)
		name = strings.TrimPrefix(name, "https://example.test/")
	}
	var present []string
	for _, f := range r.watch {
		if _, err := os.Stat(filepath.Join(in.Dir, f)); err == nil {
			present = append(present, f)
		}
	}
	sort.Strings(present)
	if r.seen == nil {
		r.seen = map[string]string{}
	}
	r.seen[name] = strings.Join(present, ",")
	if name == r.park {
		return nil, &actions.PendingError{ID: id, Pending: actions.Pending{Reason: "convergence still running"}}
	}
	return map[string]string{}, nil
}

const interleavedBlueprint = `
apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: interleaved
modules:
  - name: one
    mode: template
    files:
      a.txt: "a"
  - name: two
    mode: template
    files:
      b.txt: "b"
  - name: three
    mode: template
    files:
      c.txt: "c"
phases:
  - name: first
    modules: [one]
    hooks:
      pre:
        - { id: pre, uses: orun.http/probe@v1, with: { urls: ["https://example.test/first-pre"] } }
      post:
        - { id: post, uses: orun.http/probe@v1, with: { urls: ["https://example.test/first-post"] } }
  - name: second
    modules: [two]
    hooks:
      pre:
        - { id: pre, uses: orun.http/probe@v1, with: { urls: ["https://example.test/second-pre"] } }
      post:
        - { id: post, uses: orun.http/probe@v1, with: { urls: ["https://example.test/second-post"] } }
      await:
        - { id: converge, uses: orun.http/probe@v1, with: { urls: ["https://example.test/second-await"] } }
  - name: third
    modules: [three]
    hooks:
      pre:
        - { id: pre, uses: orun.http/probe@v1, with: { urls: ["https://example.test/third-pre"] } }
      post:
        - { id: post, uses: orun.http/probe@v1, with: { urls: ["https://example.test/third-post"] } }
`

func interleavedOpts(out string, runner ActionRunner) Options {
	return Options{
		Blueprint: []byte(interleavedBlueprint),
		OutDir:    out,
		Store:     objectstore.NewMemStore(objectstore.AlgoSHA256),
		RunHooks:  true,
		Actions:   runner,
	}
}

func TestEachPhaseIsPlacedBetweenItsOwnPreAndPostHooks(t *testing.T) {
	r := &sightRunner{watch: []string{"a.txt", "b.txt", "c.txt"}}
	if _, err := Run(context.Background(), interleavedOpts(t.TempDir(), r)); err != nil {
		t.Fatalf("run: %v", err)
	}
	want := map[string]string{
		// pre is BEFORE placement: a phase's own files are not there yet.
		"first-pre": "",
		// post is AFTER placement — of this phase, and of no later one.
		"first-post":   "a.txt",
		"second-pre":   "a.txt",
		"second-post":  "a.txt,b.txt",
		"second-await": "a.txt,b.txt",
		"third-pre":    "a.txt,b.txt",
		"third-post":   "a.txt,b.txt,c.txt",
	}
	for hook, files := range want {
		if got := r.seen[hook]; got != files {
			t.Errorf("%s saw [%s] on disk, want [%s]", hook, got, files)
		}
	}
}

// A run that stops part-way leaves the later phases unwritten, and its lock
// names only what is actually there.
func TestAParkedRunHasNotWrittenThePhasesAfterIt(t *testing.T) {
	out := t.TempDir()
	r := &sightRunner{watch: []string{"a.txt", "b.txt", "c.txt"}, park: "second-await"}
	_, err := Run(context.Background(), interleavedOpts(out, r))
	if _, parked := err.(*ParkedError); !parked {
		t.Fatalf("expected the run to park in second's await, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "c.txt")); err == nil {
		t.Error("third's file is on disk, but the run parked before third began")
	}
	prov, err := ReadProvenance(out)
	if err != nil {
		t.Fatalf("read provenance: %v", err)
	}
	for _, m := range prov.Modules {
		if m.Name == "three" && len(m.Targets) != 0 {
			t.Errorf("the lock records %v for module three, which was never written", m.Targets)
		}
		if m.Name == "two" && len(m.Targets) != 1 {
			t.Errorf("the lock should record module two's file, got %v", m.Targets)
		}
	}
}

// Without hooks there is nothing between the phases, and the whole tree is
// written as before.
func TestWithoutHooksTheWholeTreeIsWritten(t *testing.T) {
	out := t.TempDir()
	o := interleavedOpts(out, nil)
	o.RunHooks = false
	res, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, f := range []string{"a.txt", "b.txt", "c.txt"} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Errorf("%s was not written: %v", f, err)
		}
	}
	if len(res.Files) != 3 {
		t.Errorf("Result.Files = %v, want all three", res.Files)
	}
}
