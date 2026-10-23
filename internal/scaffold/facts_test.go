package scaffold

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/sourceplane/orun/internal/actions"
	"github.com/sourceplane/orun/internal/objectstore"
)

// What a build's stream carries for a reader (orun-cloud saas-build-panel
// BV3): the run's own start and end, the run's hooks under no phase, a line
// for every step that matters, and the facts each hook made — sent as they
// are learned, only when they change, and never after a phase's last word.

const factBlueprint = `apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: notes
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
        - id: repo
          uses: orun.repo/ensure@v1
          with:
            owner: acme
            name: notes
        - id: land
          uses: orun.pr/land@v1
          label: Product
          with:
            task: NOTES-1
        - id: verify
          uses: orun.http/probe@v1
          with:
            urls:
              - "https://notes-api-edge-stage.acme.workers.dev/health"
              - "https://notes-console-stage.acme.workers.dev"
        - id: reprobe
          uses: orun.http/probe@v1
          quiet: true
          with:
            urls: ["https://notes-console-stage.acme.workers.dev"]
hooks:
  preInstantiate:
    - id: epic
      uses: orun.task/ensure@v1
      with:
        kind: epic
        name: Notes
        slug: notes
`

// building is a recording runner standing in for GitHub, the task plane and
// the network: it reports progress the way the real landing does, and fails
// or parks the hook it is told to.
type building struct {
	mu      sync.Mutex
	failOn  string // hook action to fail, with its pull request open
	parkOn  string
	reports int // how many times the landing reports "checks running"
}

func (b *building) Run(_ context.Context, id string, in ActionInput) (map[string]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch id {
	case "orun.task/ensure@v1":
		return map[string]string{"id": "epc_1", "key": "EP-3", "existed": "false"}, nil
	case "orun.repo/ensure@v1":
		return map[string]string{"fullName": "acme/notes", "created": "true"}, nil
	case "orun.pr/land@v1":
		report := func(out map[string]string) {
			if in.Progress != nil {
				in.Progress(out)
			}
		}
		report(map[string]string{"stage": "opening"})
		open := map[string]string{"number": "7", "url": "https://github.com/acme/notes/pull/7", "merged": "false", "landed": "true"}
		n := b.reports
		if n == 0 {
			n = 1
		}
		for i := 0; i < n; i++ {
			report(open)
		}
		if b.failOn == id {
			return open, errors.New("checks failed on #7")
		}
		if b.parkOn == id {
			return open, &actions.PendingError{ID: id, Pending: actions.Pending{Reason: "checks still running"}}
		}
		return map[string]string{"number": "7", "url": open["url"], "merged": "true", "landed": "true"}, nil
	case "orun.http/probe@v1":
		var urls []string
		for _, t := range actions.ProbeTargets(in.Params) {
			urls = append(urls, t.URL)
		}
		return map[string]string{"checked": fmt.Sprint(len(urls)), "failures": "0", "urls": strings.Join(urls, "\n"), "failedUrls": ""}, nil
	}
	return nil, nil
}

func build(t *testing.T, b *building) ([]Event, error) {
	t.Helper()
	sink := &CollectingSink{}
	_, err := Run(context.Background(), Options{
		Blueprint: []byte(factBlueprint), OutDir: t.TempDir(),
		Store: objectstore.NewMemStore(objectstore.AlgoSHA256), Actions: b,
		RunHooks: true, Events: sink, RunID: "run_test",
	})
	return snapshotOf(sink), err
}

func factsOf(evs []Event) []Event {
	var out []Event
	for _, e := range evs {
		if e.State == EventFact {
			out = append(out, e)
		}
	}
	return out
}

func TestTheRunSaysWhenItStartsAndWhenItIsOver(t *testing.T) {
	evs, err := build(t, &building{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	first, last := evs[0], evs[len(evs)-1]
	if first.Phase != "" || first.Step != "" || first.State != EventStarted {
		t.Errorf("the first word is the run's own start: %+v", first)
	}
	if last.Phase != "" || last.Step != "" || last.State != EventDone {
		t.Errorf("the last word is the run's own end: %+v", last)
	}
}

func TestAFailedRunSaysSoLastAndAParkedOneSaysNothing(t *testing.T) {
	evs, err := build(t, &building{failOn: "orun.pr/land@v1"})
	if err == nil {
		t.Fatal("the landing failed")
	}
	last := evs[len(evs)-1]
	if last.Phase != "" || last.Step != "" || last.State != EventFailed || !strings.Contains(last.Detail, "checks failed") {
		t.Errorf("a failed run ends on its own failed, with the error: %+v", last)
	}

	evs, err = build(t, &building{parkOn: "orun.pr/land@v1"})
	var parked *ParkedError
	if !errors.As(err, &parked) {
		t.Fatalf("expected a park, got %v", err)
	}
	for _, e := range evs {
		if e.Phase == "" && e.Step == "" && (e.State == EventDone || e.State == EventFailed) {
			t.Errorf("a parked run is waiting, not over, and must not say it ended: %+v", e)
		}
	}
	// It waits on the HOOK, which a reader finds in the blueprint.
	if parked.Hook != "land" {
		t.Errorf("the park names the hook, not the action: %+v", parked)
	}
	last = evs[len(evs)-1]
	if last.State != EventWaiting || last.Step != "land" {
		t.Errorf("the wait names the hook: %+v", last)
	}
}

func TestTheEpicIsTheFirstThingTheBuildMakes(t *testing.T) {
	evs, _ := build(t, &building{})
	var epicFact, epicLine *Event
	for i := range evs {
		e := &evs[i]
		if e.Step != "epic" {
			continue
		}
		if e.Phase != "" {
			t.Fatalf("a run-level hook is under no phase: %+v", e)
		}
		switch e.State {
		case EventFact:
			epicFact = e
		case EventDone:
			epicLine = e
		case EventRunning:
			t.Errorf("finding an epic is quick and is not announced: %+v", e)
		}
	}
	if epicFact == nil || epicFact.Meta["kind"] != "epic" || epicFact.Meta["value"] != "EP-3" || epicFact.Meta["label"] != "Notes" || epicFact.Meta["key"] != "epic" {
		t.Errorf("the epic fact: %+v", epicFact)
	}
	if epicLine == nil || epicLine.Narration != "Opened the epic EP-3." {
		t.Errorf("the epic's line: %+v", epicLine)
	}
	if epicFact != nil && epicLine != nil && epicFact.Seq > epicLine.Seq {
		t.Error("a fact is pinned before the line that names it")
	}
}

func TestAPullRequestIsPinnedFromTheMomentItIsOpened(t *testing.T) {
	evs, err := build(t, &building{reports: 3})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var statuses []string
	for _, e := range factsOf(evs) {
		if e.Meta["kind"] != "pr" {
			continue
		}
		if e.Phase != "01-product" || e.Step != "land" || e.Meta["key"] != "pr:01-product/land" || e.Meta["label"] != "Product" || e.Meta["fact"] != "1" {
			t.Errorf("a pull request fact is the hook's, labelled as the hook says: %+v", e)
		}
		statuses = append(statuses, e.Meta["status"])
	}
	// Reported three times while its checks ran, and sent once: a fact is
	// sent only when it changed.
	if strings.Join(statuses, ",") != "opening,checks,merged" {
		t.Errorf("the pull request's states: %v", statuses)
	}
}

func TestEveryStepThatMattersLeavesALine(t *testing.T) {
	evs, err := build(t, &building{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	lines := map[string]string{}
	for _, e := range evs {
		if e.Phase == "01-product" && e.State == EventDone && e.Step != "" {
			lines[e.Step] = e.Narration
		}
	}
	for step, want := range map[string]string{
		"repo":   "Created the repository acme/notes.",
		"land":   "Merged #7 into main.",
		"verify": "All 2 addresses answered.",
	} {
		if lines[step] != want {
			t.Errorf("%s left %q, want %q", step, lines[step], want)
		}
	}
	if _, said := lines["reprobe"]; said {
		t.Error("a quiet hook leaves no generated line")
	}
}

func TestAddressesArePinnedAndAQuietReprobeAddsNothing(t *testing.T) {
	evs, _ := build(t, &building{})
	urls := map[string]Event{}
	for _, e := range factsOf(evs) {
		if e.Meta["kind"] == "url" {
			if e.Step == "reprobe" {
				t.Errorf("a quiet hook files no facts: %+v", e)
			}
			urls[e.Meta["value"]] = e
		}
	}
	health := urls["https://notes-api-edge-stage.acme.workers.dev/health"]
	console := urls["https://notes-console-stage.acme.workers.dev"]
	if health.Meta["pin"] != "false" || health.Meta["env"] != "stage" {
		t.Errorf("a health check is recorded, not pinned: %+v", health.Meta)
	}
	if console.Meta["pin"] != "" || console.Meta["env"] != "stage" || console.Meta["status"] != "answered" {
		t.Errorf("the console is pinned on stage: %+v", console.Meta)
	}
	for _, e := range evs {
		if e.Step == "reprobe" && e.State == EventRunning {
			t.Errorf("a quiet hook is not announced: %+v", e)
		}
	}
}

func TestAFailingHooksFactsArriveBeforeItsFailure(t *testing.T) {
	evs, _ := build(t, &building{failOn: "orun.pr/land@v1"})
	failedAt, failing := -1, -1
	for i, e := range evs {
		switch {
		case e.Phase == "01-product" && e.State == EventFailed:
			failedAt = i
			if e.Meta["prUrl"] != "https://github.com/acme/notes/pull/7" || e.Meta["prNumber"] != "7" || e.Meta["runUrl"] != "" {
				t.Errorf("a landing's failure names its pull request, never as a run: %+v", e.Meta)
			}
		case e.State == EventFact && e.Meta["kind"] == "pr" && e.Meta["status"] == "failing":
			failing = i
		}
		if failedAt >= 0 && i > failedAt && e.State == EventFact {
			t.Errorf("a fact after the phase's failure: %+v", e)
		}
	}
	if failing < 0 || failedAt < 0 || failing > failedAt {
		t.Errorf("the failing pull request (%d) is pinned before the failure (%d)", failing, failedAt)
	}
}

func TestAHooksPinOverridesTheActions(t *testing.T) {
	no, yes := false, true
	h := Hook{ID: "verify", Uses: "orun.http/probe@v1", Pin: &no, Label: "Console"}
	facts := hookFacts("08-docs", h, map[string]any{"urls": []any{"https://a.dev"}}, map[string]string{"urls": "https://a.dev"}, false)
	if len(facts) != 1 || !facts[0].Unpinned || facts[0].Label != "Console" {
		t.Errorf("pin: false keeps it off the panel: %+v", facts)
	}
	h.Pin = &yes
	facts = hookFacts("08-docs", h, nil, map[string]string{"urls": "https://a.dev/health"}, false)
	if len(facts) != 1 || facts[0].Unpinned {
		t.Errorf("pin: true keeps even a health check in view: %+v", facts)
	}
}

// An action reports progress from wherever it is running; the stream is in
// seq order however the emits interleave.
func TestTheStreamIsDeliveredInSeqOrder(t *testing.T) {
	sink := &CollectingSink{}
	em := newEmitter(sink, "run_test")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				em.emit(context.Background(), Event{State: EventRunning})
			}
		}()
	}
	wg.Wait()
	for i, e := range sink.Events {
		if e.Seq != i+1 {
			t.Fatalf("event %d delivered with seq %d", i, e.Seq)
		}
	}
}

// A misspelled parameter in a `pre:` or `await:` hook gets its line, like
// one in a bare list.
func TestAMappingFormHookErrorNamesItsLine(t *testing.T) {
	src := `apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: bp
modules:
  - name: a
    mode: template
    files:
      a.md: "a"
phases:
  - name: 01
    modules: [a]
    hooks:
      pre:
        - id: ok
          run: ["true"]
      await:
        - id: watch
          uses: orun.run/watch@v1
          with:
            repo: acme/notes
            branchh: main
`
	_, err := ParseBlueprint([]byte(src))
	if err == nil || !strings.Contains(err.Error(), "line 22") {
		t.Fatalf("expected the misspelling's line, got %v", err)
	}
}
