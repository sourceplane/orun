package scaffold

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/objectstore"
)

// The run's set-up in the background: the epic and the task open beside the
// first phase, and only the hooks that need them wait.
const backgroundBlueprint = `apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: bp
modules:
  - name: a
    mode: template
    files:
      a.md: "a"
phases:
  - name: 01-product
    modules: [a]
    hooks:
      post:
        - id: rebrand
          run: ["sh", "-c", "true"]
        - id: repo
          uses: orun.repo/ensure@v1
          with:
            owner: acme
            name: product
        - id: task
          uses: orun.task/ensure@v1
          with:
            kind: task
            name: build
        - id: land
          uses: orun.pr/land@v1
          with:
            task: "{{ .hooks.task.outputs.key }}"
hooks:
  preInstantiate:
    - id: epic
      uses: orun.task/ensure@v1
      background: true
      with:
        kind: epic
        slug: todos
        name: Todos
    - id: t01
      uses: orun.task/ensure@v1
      background: true
      with:
        kind: task
        name: build
`

// gatedRunner holds the background's epic until the phase has reached its
// repository, so a run that waited for the background before the phase would
// time out instead of passing.
type gatedRunner struct {
	mu       sync.Mutex
	log      []string
	repoSeen chan struct{}
	once     sync.Once
	epicErr  error
}

func (g *gatedRunner) note(s string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.log = append(g.log, s)
}

func (g *gatedRunner) Run(_ context.Context, id string, in ActionInput) (map[string]string, error) {
	kind, _ := in.Params["kind"].(string)
	name := strings.TrimSuffix(strings.TrimPrefix(id, "orun."), "@v1")
	if kind != "" {
		name += ":" + kind
	}
	g.note("start " + name)
	defer g.note("end " + name)
	switch {
	case id == "orun.repo/ensure@v1":
		g.once.Do(func() { close(g.repoSeen) })
	case kind == "epic":
		select {
		case <-g.repoSeen:
		case <-time.After(3 * time.Second):
			return nil, errors.New("the phase never reached its repository while the epic was opening")
		}
		if g.epicErr != nil {
			return nil, g.epicErr
		}
	case kind == "task":
		return map[string]string{"key": "BASE-1"}, nil
	case id == "orun.pr/land@v1":
		return map[string]string{"number": "1", "merged": "true"}, nil
	}
	return nil, nil
}

func (g *gatedRunner) index(s string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i, l := range g.log {
		if l == s {
			return i
		}
	}
	return -1
}

func runBackground(t *testing.T, bp string, g *gatedRunner) ([]Event, error) {
	t.Helper()
	sink := &CollectingSink{}
	_, err := Run(context.Background(), Options{
		Blueprint: []byte(bp), OutDir: t.TempDir(),
		Store: objectstore.NewMemStore(objectstore.AlgoSHA256), Actions: g,
		RunHooks: true, Events: sink, RunID: "run_test",
	})
	return snapshotOf(sink), err
}

func TestTheBackgroundRunsBesideThePhase(t *testing.T) {
	g := &gatedRunner{repoSeen: make(chan struct{})}
	evs, err := runBackground(t, backgroundBlueprint, g)
	if err != nil {
		t.Fatalf("run: %v\n%v", err, g.log)
	}
	// The phase reached its repository while the epic was still opening…
	if g.index("start repo/ensure") > g.index("end task/ensure:epic") {
		t.Fatalf("the phase waited for the background before its repository:\n%s", strings.Join(g.log, "\n"))
	}
	// …and its task hook — on the task plane — waited for all of it, so the
	// task it finds is the one the background opened, never a second one.
	tasks := 0
	for i, l := range g.log {
		if l == "start task/ensure:task" {
			tasks++
			if tasks == 2 && i < g.index("end task/ensure:task") {
				t.Fatalf("the phase's task hook ran before the background's had finished:\n%s", strings.Join(g.log, "\n"))
			}
		}
	}
	if tasks != 2 || g.index("start pr/land") < 0 {
		t.Fatalf("want the background's task, the phase's, then the landing:\n%s", strings.Join(g.log, "\n"))
	}
	// The background's steps are the run's, not the phase's.
	for _, e := range evs {
		if (e.Step == "epic" || e.Step == "t01") && e.Phase != "" {
			t.Fatalf("a background step was reported under phase %q", e.Phase)
		}
	}
}

func TestABackgroundFailureStopsTheRunAtTheNextHook(t *testing.T) {
	g := &gatedRunner{repoSeen: make(chan struct{}), epicErr: errors.New("epic: 503 from the task plane")}
	evs, err := runBackground(t, backgroundBlueprint, g)
	if err == nil || !strings.Contains(err.Error(), "503 from the task plane") {
		t.Fatalf("want the background's failure as the run's, got %v", err)
	}
	if g.index("start pr/land") >= 0 || g.index("start task/ensure:task") >= 0 {
		t.Fatalf("hooks ran past the background's failure:\n%s", strings.Join(g.log, "\n"))
	}
	reported := false
	for _, e := range evs {
		if e.State == EventFailed && e.Step == "epic" && e.Phase == "" {
			reported = true
		}
	}
	if !reported {
		t.Fatalf("the background's failure was not reported as its own step: %+v", evs)
	}
}

// A phase list that never touches the task plane never waited for the
// background — and the run still does not end before it, or over its failure.
func TestTheRunDoesNotEndBeforeTheBackground(t *testing.T) {
	bp := strings.Replace(backgroundBlueprint, `        - id: task
          uses: orun.task/ensure@v1
          with:
            kind: task
            name: build
        - id: land
          uses: orun.pr/land@v1
          with:
            task: "{{ .hooks.task.outputs.key }}"
`, "", 1)
	g := &gatedRunner{repoSeen: make(chan struct{}), epicErr: errors.New("epic: refused")}
	if _, err := runBackground(t, bp, g); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("want the background's failure at the end of the run, got %v", err)
	}
	if g.index("end task/ensure:epic") < 0 {
		t.Fatal("the run ended with the background still running")
	}
}

func TestBackgroundIsOnlyForTheRunsSetUp(t *testing.T) {
	cases := map[string]struct{ from, to, want string }{
		"an argv hook": {"      uses: orun.task/ensure@v1\n      background: true\n      with:\n        kind: epic",
			"      run: [\"true\"]\n      background: true\n      with:\n        kind: epic", "background is for `uses:` hooks"},
		"a foreground hook after it": {"    - id: t01\n      uses: orun.task/ensure@v1\n      background: true\n",
			"    - id: t01\n      uses: orun.task/ensure@v1\n", "must be `background: true` too"},
		"a phase's hook": {"        - id: repo\n          uses: orun.repo/ensure@v1\n",
			"        - id: repo\n          uses: orun.repo/ensure@v1\n          background: true\n", "only for hooks.preInstantiate"},
	}
	for name, c := range cases {
		bp := strings.Replace(backgroundBlueprint, c.from, c.to, 1)
		if bp == backgroundBlueprint {
			t.Fatalf("%s: the case did not change the blueprint", name)
		}
		if c.want == "background is for `uses:` hooks" {
			// An argv hook takes no `with:`.
			bp = strings.Replace(bp, "      run: [\"true\"]\n      background: true\n      with:\n        kind: epic\n        slug: todos\n        name: Todos\n", "      run: [\"true\"]\n      background: true\n", 1)
		}
		_, err := Run(context.Background(), Options{
			Blueprint: []byte(bp), OutDir: t.TempDir(),
			Store: objectstore.NewMemStore(objectstore.AlgoSHA256), Actions: &gatedRunner{repoSeen: make(chan struct{})},
			RunHooks: true,
		})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want a refusal saying %q, got %v", name, c.want, err)
		}
	}
}

func TestSplitBackgroundKeepsTheOrder(t *testing.T) {
	hooks := []Hook{{ID: "a"}, {ID: "b"}, {ID: "c", Background: true}, {ID: "d", Background: true}}
	fg, bg := splitBackground(hooks)
	if fmt.Sprint(ids(fg)) != "[a b]" || fmt.Sprint(ids(bg)) != "[c d]" {
		t.Fatalf("foreground %v, background %v", ids(fg), ids(bg))
	}
	if fg, bg := splitBackground(hooks[:2]); len(fg) != 2 || bg != nil {
		t.Fatalf("no background: %v %v", ids(fg), ids(bg))
	}
}

func ids(hs []Hook) []string {
	out := make([]string, len(hs))
	for i, h := range hs {
		out[i] = h.ID
	}
	return out
}
