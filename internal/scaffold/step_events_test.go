package scaffold

import (
	"context"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/objectstore"
)

// A phase whose hooks are the slow part: it lands, then watches.
const stepBlueprint = `apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: bp
modules:
  - name: a
    mode: template
    files:
      a.md: "a"
phases:
  - name: 03-infrastructure
    modules: [a]
    hooks:
      post:
        - id: task
          uses: orun.task/ensure@v1
          with:
            kind: task
            name: infra
        - id: land
          uses: orun.pr/land@v1
          narrate: "PR {{ .hooks.land.outputs.number }} is merged."
          with:
            task: BASE-3
        - id: converge
          uses: orun.run/watch@v1
          with:
            repo: acme/product
hooks:
  postInstantiate:
    - id: tail
      uses: orun.run/watch@v1
      with:
        repo: acme/product
`

// seeing records, for each action it runs, what the build had already said.
type seeing struct {
	sink     *CollectingSink
	saidThen map[string][]Event
}

func (s *seeing) Run(_ context.Context, id string, _ ActionInput) (map[string]string, error) {
	s.saidThen[id] = snapshotOf(s.sink)
	if id == "orun.pr/land@v1" {
		return map[string]string{"number": "3"}, nil
	}
	return nil, nil
}

func snapshotOf(s *CollectingSink) []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Event(nil), s.Events...)
}

func runSteps(t *testing.T) (*seeing, []Event) {
	t.Helper()
	sink := &CollectingSink{}
	r := &seeing{sink: sink, saidThen: map[string][]Event{}}
	if _, err := Run(context.Background(), Options{
		Blueprint: []byte(stepBlueprint), OutDir: t.TempDir(),
		Store: objectstore.NewMemStore(objectstore.AlgoSHA256), Actions: r,
		RunHooks: true, Events: sink, RunID: "run_test",
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	return r, snapshotOf(sink)
}

// A landing waits on CI for up to an hour. The build says so as it starts,
// not when the whole phase is over — which is when it used to say anything.
func TestASlowActionIsAnnouncedAsItStarts(t *testing.T) {
	r, _ := runSteps(t)
	said := r.saidThen["orun.pr/land@v1"]
	if len(said) == 0 {
		t.Fatal("nothing had been said when the landing ran")
	}
	last := said[len(said)-1]
	if last.State != EventRunning || last.Step != "land" || !strings.Contains(last.Narration, "merging it once its CI passes") {
		t.Fatalf("when the landing ran, the last word was %+v", last)
	}
}

// A hook's own line lands the moment it completes: while the phase watches its
// convergence run, the feed already says the PR is merged.
func TestAHooksLineArrivesBeforeTheNextHookRuns(t *testing.T) {
	r, _ := runSteps(t)
	for _, e := range r.saidThen["orun.run/watch@v1"] {
		if e.Step == "land" && e.State == EventDone && e.Narration == "PR 3 is merged." {
			return
		}
	}
	t.Fatalf("the landing's line had not been said when the watch began: %+v", r.saidThen["orun.run/watch@v1"])
}

// Bookkeeping says nothing: finding a task is not something to watch.
func TestAQuickActionSaysNothingAsItStarts(t *testing.T) {
	_, evs := runSteps(t)
	for _, e := range evs {
		if e.Step == "task" {
			t.Fatalf("a quick action was announced: %+v", e)
		}
	}
}

// Every authored line once, and global hooks under no phase.
func TestEachLineOnceAndGlobalHooksUnderNoPhase(t *testing.T) {
	_, evs := runSteps(t)
	merged := 0
	for _, e := range evs {
		if e.Narration == "PR 3 is merged." {
			merged++
		}
		if e.Step == "tail" {
			t.Fatalf("a postInstantiate hook was reported under phase %q", e.Phase)
		}
	}
	if merged != 1 {
		t.Fatalf("the landing's line was said %d times", merged)
	}
}
