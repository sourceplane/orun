package scaffold

import (
	"context"
	"testing"

	"github.com/sourceplane/orun/internal/objectstore"
)

const threePhaseBlueprint = `apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: bp
inputs:
  domain:
    type: boolean
    default: false
modules:
  - name: a
    mode: template
    files:
      a.md: "a"
  - name: b
    mode: template
    files:
      b.md: "b"
  - name: zone
    mode: template
    files:
      zone.md: "z"
phases:
  - name: 01-scaffold
    title: The repo is born
    modules: [a]
  - name: 02-foundation
    modules: [b]
  - name: 07-domain
    title: The custom domain
    modules: [zone]
    when: "inputs.domain"
`

func skipLines(t *testing.T, opts Options) map[string]string {
	t.Helper()
	sink := &CollectingSink{}
	opts.Blueprint = []byte(threePhaseBlueprint)
	opts.Store = objectstore.NewMemStore(objectstore.AlgoSHA256)
	opts.Events = sink
	if _, err := Run(context.Background(), opts); err != nil {
		t.Fatalf("run: %v", err)
	}
	out := map[string]string{}
	for _, e := range snapshotEvents(sink) {
		if e.State == EventSkipped {
			out[e.Phase] = e.Narration
		}
	}
	return out
}

func snapshotEvents(s *CollectingSink) []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Event(nil), s.Events...)
}

// A RESUMED build leaves what an earlier run placed, and says so. It used to
// report those phases in the words meant for a phase whose condition excludes
// it — a resumed console build told its operator "The repo is born is not
// needed for this build" about the repository's first phase.
func TestAResumedBuildSaysWhatAnEarlierRunPlaced(t *testing.T) {
	out := t.TempDir()
	if _, err := Run(context.Background(), Options{
		Blueprint: []byte(threePhaseBlueprint), OutDir: out,
		Store: objectstore.NewMemStore(objectstore.AlgoSHA256), Only: "01-scaffold",
	}); err != nil {
		t.Fatalf("first run: %v", err)
	}
	lines := skipLines(t, Options{OutDir: out, Resume: true})
	if got := lines["01-scaffold"]; got != "01-scaffold is already in place from an earlier run." {
		t.Fatalf("resumed phase narrated %q", got)
	}
	if got := lines["07-domain"]; got != "The custom domain is not needed for this build." {
		t.Fatalf("a phase its condition excludes narrated %q", got)
	}
	if _, placed := lines["02-foundation"]; placed {
		t.Fatal("the phase this run places was reported as skipped")
	}
}

// A run narrowed with --phase does not place the others, and that is neither
// "already in place" nor "not needed".
func TestANarrowedRunSaysTheRestAreNotPartOfIt(t *testing.T) {
	lines := skipLines(t, Options{OutDir: t.TempDir(), Only: "02-foundation"})
	if got := lines["01-scaffold"]; got != "01-scaffold is not part of this run." {
		t.Fatalf("a phase outside the selection narrated %q", got)
	}
	if got := lines["07-domain"]; got != "The custom domain is not needed for this build." {
		t.Fatalf("a phase its condition excludes narrated %q", got)
	}
}
