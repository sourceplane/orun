package scaffold

import (
	"context"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/objectstore"
)

const narratedBlueprint = `
apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: narrated
inputs:
  domain:
    type: boolean
    default: false
modules:
  - name: edge
    mode: template
    files:
      edge.txt: "edge"
  - name: zone
    mode: template
    files:
      zone.txt: "zone"
phases:
  - name: 05-edge
    title: The API edge
    expectedMinutes: 5
    modules: [edge]
    narrate:
      start: "Now building the API edge — the single front door to the services."
      done: "The edge answers /health on stage and prod."
      failed: "The edge did not come up. The log above says why."
  - name: 07-domain
    modules: [zone]
    when: "inputs.domain"
`

func narratedOpts(t *testing.T, sink EventSink) Options {
	t.Helper()
	return Options{
		Blueprint: []byte(narratedBlueprint),
		OutDir:    t.TempDir(),
		Store:     objectstore.NewMemStore(objectstore.AlgoSHA256),
		RunHooks:  true,
		Events:    sink,
		RunID:     "run_test",
	}
}

func TestAuthoredNarrationIsWhatIsEmitted(t *testing.T) {
	sink := &CollectingSink{}
	if _, err := Run(context.Background(), narratedOpts(t, sink)); err != nil {
		t.Fatalf("run: %v", err)
	}
	joined := strings.Join(sink.Narrations(), "\n")
	if !strings.Contains(joined, "single front door") {
		t.Errorf("the baseline's own start line should be emitted; got:\n%s", joined)
	}
	if !strings.Contains(joined, "answers /health") {
		t.Errorf("the baseline's own done line should be emitted; got:\n%s", joined)
	}
}

// A missing line renders a generated one. Silence reads as a stalled build.
func TestAPhaseWithNoNarrationStillSpeaks(t *testing.T) {
	sink := &CollectingSink{}
	o := narratedOpts(t, sink)
	o.Inputs = map[string]string{"domain": "true"}
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatalf("run: %v", err)
	}
	joined := strings.Join(sink.Narrations(), "\n")
	if !strings.Contains(joined, "07-domain") {
		t.Errorf("an unnarrated phase must still produce a line; got:\n%s", joined)
	}
}

// A phase the condition excludes is reported once, so a feed shows the whole
// shape rather than only the part that moved.
func TestAnExcludedPhaseIsReportedAsSkipped(t *testing.T) {
	sink := &CollectingSink{}
	if _, err := Run(context.Background(), narratedOpts(t, sink)); err != nil {
		t.Fatalf("run: %v", err)
	}
	var found bool
	for _, e := range sink.Events {
		if e.Phase == "07-domain" && e.State == EventSkipped {
			found = true
		}
	}
	if !found {
		t.Error("the excluded phase should be reported as skipped")
	}
}

func TestEventsCarryTheEnvelopeAndAreOrdered(t *testing.T) {
	sink := &CollectingSink{}
	if _, err := Run(context.Background(), narratedOpts(t, sink)); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(sink.Events) == 0 {
		t.Fatal("expected events")
	}
	for i, e := range sink.Events {
		if e.Schema != EventSchema {
			t.Errorf("event %d has no schema version — the stream is an interface", i)
		}
		if e.RunID != "run_test" {
			t.Errorf("event %d lost the run id, got %q", i, e.RunID)
		}
		if e.Seq != i+1 {
			t.Errorf("seq must be monotonic from 1; event %d has seq %d", i, e.Seq)
		}
		if e.At == "" {
			t.Errorf("event %d has no timestamp", i)
		}
	}
}

// The engine supplies the numbers; the YAML supplies the words.
func TestDoneEventsCarryTheEnginesFacts(t *testing.T) {
	sink := &CollectingSink{}
	if _, err := Run(context.Background(), narratedOpts(t, sink)); err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, e := range sink.Events {
		if e.Phase == "05-edge" && e.State == EventDone {
			if e.Meta["files"] != "1" {
				t.Errorf("expected the placed file count, got %v", e.Meta)
			}
			if e.Meta["expectedMinutes"] != "5" {
				t.Errorf("expected the declared budget, got %v", e.Meta)
			}
			if e.Meta["elapsed"] == "" {
				t.Error("expected an elapsed fact")
			}
			return
		}
	}
	t.Error("no done event for 05-edge")
}

// state is the truth and narration is the caption.
func TestNarrationMayNotAssertAState(t *testing.T) {
	for _, bad := range []string{
		`      done: "05-edge: done"`,
		`      done: "the phase completed"`,
		`      done: "nothing failed"`,
	} {
		src := strings.Replace(narratedBlueprint,
			`      done: "The edge answers /health on stage and prod."`, bad, 1)
		_, err := ParseBlueprint([]byte(src))
		if err == nil {
			t.Errorf("narration %q asserts a state and must be refused", bad)
			continue
		}
		if !strings.Contains(err.Error(), "caption") {
			t.Errorf("the error should explain the rule; got %v", err)
		}
	}
}

// A state word inside an expression is a field reference, not an assertion.
func TestNarrationMayReferenceAStateWordInsideAnExpression(t *testing.T) {
	src := strings.Replace(narratedBlueprint,
		`      done: "The edge answers /health on stage and prod."`,
		`      done: "The edge answers on {{ .done }} environments."`, 1)
	if _, err := ParseBlueprint([]byte(src)); err != nil {
		t.Errorf("an expression is not an assertion: %v", err)
	}
}

// "incomplete" must not fire the "complete" rule.
func TestNarrationWordMatchingIsWholeWord(t *testing.T) {
	src := strings.Replace(narratedBlueprint,
		`      done: "The edge answers /health on stage and prod."`,
		`      done: "The edge is reachable, with incompleteness nowhere in sight."`, 1)
	if _, err := ParseBlueprint([]byte(src)); err != nil {
		t.Errorf("a substring match would be a false positive: %v", err)
	}
}

// A broken caption must never fail a build that otherwise succeeded.
func TestABrokenNarrationTemplateFallsBackRatherThanFailing(t *testing.T) {
	got := renderNarration("{{ .nope | nosuchfunc }}", "05-edge", EventDone, nil)
	if got == "" {
		t.Fatal("a failed render must fall back, never to silence")
	}
	if strings.Contains(got, "{{") {
		t.Errorf("the fallback should be prose, got %q", got)
	}
}

func TestTransitionLineComposesProseAndFacts(t *testing.T) {
	line := transitionLine("The edge answers /health.", []string{"1 file(s)", "took 4s"},
		"Now building the console.")
	for _, want := range []string{"The edge answers /health.", "1 file(s) · took 4s", "→ Now building the console."} {
		if !strings.Contains(line, want) {
			t.Errorf("transition line missing %q; got:\n%s", want, line)
		}
	}
}
