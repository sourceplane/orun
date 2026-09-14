package scaffold

import (
	"context"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/objectstore"
)

// Narration, actually rendered (orun-bootstrap-engine BE-O10).
//
// design.md §4 rule 1 is that narration is "a template over state, never prose
// about facts", and gives `"{{ .phase.title }} is live on {{ .envs | join }}"`
// as the example. BE-O6 shipped the type and passed nil at every call site, so
// the rule was true of the shape and false of the values: an authored line with
// an expression rendered nothing and degraded silently to the generated one.

func TestAnAuthoredLineRendersAgainstThePhaseAndItsInputs(t *testing.T) {
	scope := narrationScope("05-edge", "The API edge",
		map[string]any{"productName": "Acme Cloud"},
		map[string]string{"files": "59", "elapsed": "4m12s"},
		map[string]map[string]string{"land": {"number": "14"}})

	for _, tc := range []struct{ line, want string }{
		{"{{ .phase.title }} answers /health", "The API edge answers /health"},
		{"{{ .phase.name }}", "05-edge"},
		{"{{ .inputs.productName }} is ready", "Acme Cloud is ready"},
		{"{{ .meta.files }} files in {{ .meta.elapsed }}", "59 files in 4m12s"},
		{"PR {{ .hooks.land.outputs.number }} merged", "PR 14 merged"},
	} {
		got := renderNarration(tc.line, "05-edge", EventDone, scope)
		if got != tc.want {
			t.Errorf("%q rendered %q, want %q", tc.line, got, tc.want)
		}
	}
}

// The silent degrade was the worst part: an authored line that could not render
// looked exactly like a phase that authored nothing, so nobody would know the
// prose they wrote was never shown.
func TestALineThatCannotRenderSaysSoRatherThanVanishing(t *testing.T) {
	got := renderNarration("{{ .nope.here }}", "05-edge", EventDone, narrationScope("05-edge", "05-edge", nil, nil, nil))
	if !strings.Contains(got, "narration unavailable") {
		t.Errorf("a failed render should say so; got %q", got)
	}
}

// A template that cannot COMPILE is an authoring mistake, and finding out at
// run time means finding out in front of the operator it was written for.
func TestABrokenNarrationTemplateIsAParseTimeError(t *testing.T) {
	const body = `
apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: bp
modules:
  - name: only
    mode: template
    files:
      README.md: "hi"
phases:
  - name: 05-edge
    modules: [only]
    narrate:
      done: "{{ .phase.title "
`
	_, err := ParseBlueprint([]byte(body))
	if err == nil {
		t.Fatal("expected a parse error for an uncompilable narration template")
	}
	if !strings.Contains(err.Error(), "narrate.done") {
		t.Errorf("the error should name the line; got %v", err)
	}
}

// A hook's `narrate:` was the one place in the document where a caption could
// assert a state — it was emitted verbatim, never validated and never rendered.
func TestAHookNarrationIsHeldToTheSameRules(t *testing.T) {
	const body = `
apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: bp
modules:
  - name: only
    mode: template
    files:
      README.md: "hi"
phases:
  - name: 05-edge
    modules: [only]
    hooks:
      - id: land
        uses: orun.pr/land@v1
        narrate: "The edge PR is done."
        with:
          task: T-1
`
	_, err := ParseBlueprint([]byte(body))
	if err == nil {
		t.Fatal("a hook narration asserting a state should be refused")
	}
	if !strings.Contains(err.Error(), "may not assert") {
		t.Errorf("the error should explain the rule; got %v", err)
	}
}

// ── A hook-only phase is not done because it wrote nothing ────────────────

const hookOnlyBlueprint = `
apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: bp
modules:
  - name: only
    mode: template
    files:
      README.md: "hi"
phases:
  - name: 01-place
    modules: [only]
  - name: 02-record
    modules: []
    hooks:
      post:
        - id: land
          uses: orun.pr/land@v1
          with:
            task: T-1
`

func deriveHookOnly(t *testing.T, outDir string) *Status {
	t.Helper()
	st, err := Derive(context.Background(), Options{
		Blueprint: []byte(hookOnlyBlueprint),
		OutDir:    outDir,
		Store:     objectstore.NewMemStore(objectstore.AlgoSHA256),
	})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	return st
}

func TestAPhaseWithHooksAndNoFilesIsUnknownNotDone(t *testing.T) {
	st := deriveHookOnly(t, t.TempDir())
	byName := map[string]PhaseState{}
	for _, p := range st.Phases {
		byName[p.Name] = p.State
	}
	if byName["02-record"] != PhaseUnknown {
		t.Errorf("02-record = %q, want %q — it places nothing, so the tree cannot say it ran",
			byName["02-record"], PhaseUnknown)
	}
}

// The consume-only case still answers done: no files AND no hooks means there
// is genuinely nothing that could be missing.
func TestAPhaseWithNeitherFilesNorHooksIsStillDone(t *testing.T) {
	const body = `
apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: bp
modules:
  - name: only
    mode: template
    files:
      README.md: "hi"
phases:
  - name: 01-place
    modules: [only]
  - name: 02-nothing
    modules: []
`
	st, err := Derive(context.Background(), Options{
		Blueprint: []byte(body), OutDir: t.TempDir(),
		Store: objectstore.NewMemStore(objectstore.AlgoSHA256),
	})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	for _, p := range st.Phases {
		if p.Name == "02-nothing" && p.State != PhaseDone {
			t.Errorf("02-nothing = %q, want done", p.State)
		}
	}
}

// --resume must not skip a phase it cannot vouch for. The hooks are idempotent
// by construction (find-or-create, additive apply), so re-running one that had
// already run costs a few API calls; skipping one that had not leaves a
// bootstrap silently incomplete.
func TestResumeDoesNotSkipAnUnknownPhase(t *testing.T) {
	out := t.TempDir()
	opts := Options{
		Blueprint: []byte(hookOnlyBlueprint), OutDir: out,
		Store: objectstore.NewMemStore(objectstore.AlgoSHA256), Resume: true,
	}
	plan, err := buildPlan(context.Background(), opts)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	selected, narrowed, err := plan.selectPhases(opts)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if !narrowed {
		t.Fatal("--resume should narrow")
	}
	var names []string
	for _, p := range selected {
		names = append(names, p.Name)
	}
	if !contains(names, "02-record") {
		t.Errorf("resume selected %v — 02-record is unknown and must not be skipped", names)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
