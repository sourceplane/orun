package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/objectstore"
)

const phasedBlueprint = `
apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: phased
inputs:
  productName:
    type: string
    required: true
modules:
  - name: one
    mode: template
    files:
      a.txt: "one for {{ .productName }}"
  - name: two
    mode: template
    files:
      b.txt: "two"
  - name: three
    mode: template
    files:
      c.txt: "three"
phases:
  - name: first
    modules: [one]
  - name: second
    modules: [two]
  - name: third
    modules: [three]
`

func opts(t *testing.T, out string) Options {
	t.Helper()
	return Options{
		Blueprint: []byte(phasedBlueprint),
		Inputs:    map[string]string{"productName": "acme"},
		OutDir:    out,
		Store:     objectstore.NewMemStore(objectstore.AlgoSHA256),
	}
}

func statusOf(t *testing.T, o Options) *Status {
	t.Helper()
	st, err := Derive(context.Background(), o)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	return st
}

func stateOf(st *Status, name string) PhaseState {
	for _, p := range st.Phases {
		if p.Name == name {
			return p.State
		}
	}
	return ""
}

// The milestone's own acceptance: an untouched directory derives every phase
// as pending, with nothing stored anywhere.
func TestDeriveOnAnEmptyTreeReportsEveryPhasePending(t *testing.T) {
	o := opts(t, t.TempDir())
	st := statusOf(t, o)
	if len(st.Phases) != 3 {
		t.Fatalf("expected 3 phases, got %d", len(st.Phases))
	}
	for _, p := range st.Phases {
		if p.State != PhasePending {
			t.Errorf("phase %s should be pending, got %s", p.Name, p.State)
		}
	}
	if st.Next != "first" {
		t.Errorf("next should be the first phase, got %q", st.Next)
	}
	if st.Done() {
		t.Error("an empty tree is not done")
	}
}

func TestDeriveAfterAFullRunReportsEveryPhaseDone(t *testing.T) {
	out := t.TempDir()
	o := opts(t, out)
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatalf("run: %v", err)
	}
	st := statusOf(t, opts(t, out))
	for _, p := range st.Phases {
		if p.State != PhaseDone {
			t.Errorf("phase %s should be done, got %s (missing %v, drifted %v)", p.Name, p.State, p.Missing, p.Drifted)
		}
	}
	if !st.Done() {
		t.Errorf("everything is placed; Done() should be true, next=%q", st.Next)
	}
}

// The property the whole rule exists for: derivation reads the TREE, so
// deleting every local artifact changes nothing about the answer.
func TestDeriveSurvivesDeletingTheLocalArtifacts(t *testing.T) {
	out := t.TempDir()
	if _, err := Run(context.Background(), opts(t, out)); err != nil {
		t.Fatalf("run: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(out, ".orun")); err != nil {
		t.Fatalf("remove .orun: %v", err)
	}
	st := statusOf(t, opts(t, out))
	for _, p := range st.Phases {
		if p.State != PhaseDone {
			t.Errorf("phase %s should still derive as done with no .orun present, got %s", p.Name, p.State)
		}
	}
}

func TestOnlyPlacesOnePhase(t *testing.T) {
	out := t.TempDir()
	o := opts(t, out)
	o.Only = "second"
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatalf("run: %v", err)
	}
	st := statusOf(t, opts(t, out))
	if got := stateOf(st, "second"); got != PhaseDone {
		t.Errorf("the selected phase should be done, got %s", got)
	}
	if got := stateOf(st, "first"); got != PhasePending {
		t.Errorf("an unselected phase must not be placed, got %s", got)
	}
	if got := stateOf(st, "third"); got != PhasePending {
		t.Errorf("an unselected phase must not be placed, got %s", got)
	}
}

func TestUntilPlacesThroughTheNamedPhase(t *testing.T) {
	out := t.TempDir()
	o := opts(t, out)
	o.Until = "second"
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatalf("run: %v", err)
	}
	st := statusOf(t, opts(t, out))
	if stateOf(st, "first") != PhaseDone || stateOf(st, "second") != PhaseDone {
		t.Error("phases through the named one should be placed")
	}
	if stateOf(st, "third") != PhasePending {
		t.Error("phases after the named one must not be placed")
	}
	if st.Next != "third" {
		t.Errorf("next should be the first undone phase, got %q", st.Next)
	}
}

func TestResumePlacesOnlyWhatIsNotDone(t *testing.T) {
	out := t.TempDir()
	first := opts(t, out)
	first.Until = "first"
	if _, err := Run(context.Background(), first); err != nil {
		t.Fatalf("first run: %v", err)
	}
	rest := opts(t, out)
	rest.Resume = true
	res, err := Run(context.Background(), rest)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	// The already-done phase is not re-placed, so it is not in this run's plan.
	for _, ph := range res.Phases {
		if ph.Name == "first" {
			t.Error("resume should skip a phase already derived as done")
		}
	}
	st := statusOf(t, opts(t, out))
	if !st.Done() {
		t.Errorf("resume should finish the product, next=%q", st.Next)
	}
}

// Re-running a completed product is a no-op that re-verifies, which is what
// makes "run a phase again" always safe.
func TestResumeOnACompleteProductPlacesNothing(t *testing.T) {
	out := t.TempDir()
	if _, err := Run(context.Background(), opts(t, out)); err != nil {
		t.Fatalf("run: %v", err)
	}
	again := opts(t, out)
	again.Resume = true
	res, err := Run(context.Background(), again)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if len(res.Phases) != 0 {
		t.Errorf("nothing should be re-placed, got %d phase(s)", len(res.Phases))
	}
}

// RESUME MUST NOT UN-BRAND A PRODUCT (BE-O12).
//
// This used to re-place a drifted phase, on the reasoning that "re-placing
// restores the phase to what the blueprint says, which is what a resume is
// for". For a baseline that brands what it places, what the blueprint says is
// the UNBRANDED baseline — so a resume reverted the product's identity.
// Observed on cirrus's real blueprint: package.json's `name` went from
// "acme-cloud" back to "cirrus".
func TestResumeLeavesADriftedPhaseAlone(t *testing.T) {
	out := t.TempDir()
	if _, err := Run(context.Background(), opts(t, out)); err != nil {
		t.Fatalf("run: %v", err)
	}
	branded := []byte("two, branded for the product")
	if err := os.WriteFile(filepath.Join(out, "b.txt"), branded, 0o644); err != nil {
		t.Fatalf("brand: %v", err)
	}
	again := opts(t, out)
	again.Resume = true
	res, err := Run(context.Background(), again)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	for _, ph := range res.Phases {
		if ph.Name == "second" {
			t.Error("resume re-placed a drifted phase — on a branded product that reverts the branding")
		}
	}
	got, err := os.ReadFile(filepath.Join(out, "b.txt"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != string(branded) {
		t.Errorf("the product's own content must survive a resume; got %q", got)
	}
}

// Skipping drift must not become skipping an unfinished placement: `partial`
// means some files are missing, so re-running is what completes it.
func TestResumeStillPlacesAPartialPhase(t *testing.T) {
	out := t.TempDir()
	if _, err := Run(context.Background(), opts(t, out)); err != nil {
		t.Fatalf("run: %v", err)
	}
	if err := os.Remove(filepath.Join(out, "b.txt")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	again := opts(t, out)
	again.Resume = true
	res, err := Run(context.Background(), again)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	var placed bool
	for _, ph := range res.Phases {
		if ph.Name == "second" {
			placed = true
		}
	}
	if !placed {
		t.Error("a phase missing one of its files is unfinished, and resume must finish it")
	}
	if !statusOf(t, opts(t, out)).Done() {
		t.Error("the product should be complete again after the resume")
	}
}

func TestDriftIsReportedRatherThanIgnored(t *testing.T) {
	out := t.TempDir()
	if _, err := Run(context.Background(), opts(t, out)); err != nil {
		t.Fatalf("run: %v", err)
	}
	if err := os.WriteFile(filepath.Join(out, "b.txt"), []byte("edited by hand"), 0o644); err != nil {
		t.Fatalf("edit: %v", err)
	}
	st := statusOf(t, opts(t, out))
	if got := stateOf(st, "second"); got != PhaseDrifted {
		t.Errorf("an edited file should read as drifted, got %s", got)
	}
	if st.Next != "second" {
		t.Errorf("a drifted phase is not done, so it is next; got %q", st.Next)
	}
}

func TestPartialTreeReadsAsPartialNotPending(t *testing.T) {
	out := t.TempDir()
	o := opts(t, out)
	o.Blueprint = []byte(strings.Replace(phasedBlueprint,
		`  - name: first
    modules: [one]`,
		`  - name: first
    modules: [one, two]`, 1))
	o.Blueprint = []byte(strings.Replace(string(o.Blueprint),
		`  - name: second
    modules: [two]
`, "", 1))
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatalf("run: %v", err)
	}
	if err := os.Remove(filepath.Join(out, "b.txt")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	st, err := Derive(context.Background(), o)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if got := stateOf(st, "first"); got != PhasePartial {
		t.Errorf("one of two files missing is partial, got %s", got)
	}
}

// `--phase` and `--until` are two answers to "which phases?"; given both, the
// engine would have to pick one, and either pick is a build nobody asked for.
func TestSelectingTwoWaysAtOnceIsRefused(t *testing.T) {
	o := opts(t, t.TempDir())
	o.Only = "first"
	o.Until = "second"
	_, err := Run(context.Background(), o)
	if err == nil {
		t.Fatal("--phase and --until select phases two different ways; that must be refused")
	}
	if !strings.Contains(err.Error(), "--phase") || !strings.Contains(err.Error(), "--until") {
		t.Errorf("the refusal must name both selectors; got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(o.OutDir, "a.txt")); statErr == nil {
		t.Error("a refused selection placed a file")
	}
}

// ── --resume COMPOSES WITH A WINDOW (orun-cloud saas-orun-work WA5) ─────────
//
// The bootstrap driver always passes --resume, so a runner restarted on the
// same tree does not re-place what is there. A build that stops after its
// scaffold stage (--until) or runs only its live one (--phase) is restarted
// the same way, so --resume narrows whichever window it is given instead of
// being refused beside it.

func selected(res *Result) string {
	names := make([]string, 0, len(res.Phases))
	for _, ph := range res.Phases {
		names = append(names, ph.Name)
	}
	return strings.Join(names, ",")
}

func TestResumeNarrowsAnUntilWindow(t *testing.T) {
	dir := t.TempDir()
	first := opts(t, dir)
	first.Until = "first"
	if _, err := Run(context.Background(), first); err != nil {
		t.Fatalf("first run: %v", err)
	}
	again := opts(t, dir)
	again.Until, again.Resume = "second", true
	res, err := Run(context.Background(), again)
	if err != nil {
		t.Fatalf("--until second --resume: %v", err)
	}
	if got := selected(res); got != "second" {
		t.Fatalf("placed %q; want only the window's phase not yet in place", got)
	}
	st := statusOf(t, opts(t, dir))
	if stateOf(st, "third") != PhasePending {
		t.Error("a resume placed past its --until stop")
	}

	// Restarted on the same tree — the sandbox reclaimed after the stop — the
	// same command places nothing and still stops where it was told.
	res, err = Run(context.Background(), again)
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	if got := selected(res); got != "" {
		t.Fatalf("a restart re-placed %q", got)
	}
	if stateOf(statusOf(t, opts(t, dir)), "third") != PhasePending {
		t.Error("a restarted stopped build placed past its stop")
	}
}

func TestResumeNarrowsAPhaseWindow(t *testing.T) {
	dir := t.TempDir()
	first := opts(t, dir)
	first.Until = "second"
	if _, err := Run(context.Background(), first); err != nil {
		t.Fatalf("first run: %v", err)
	}
	// The live stage on its own: the phase the stop left.
	live := opts(t, dir)
	live.Only, live.Resume = "third", true
	res, err := Run(context.Background(), live)
	if err != nil {
		t.Fatalf("--phase third --resume: %v", err)
	}
	if got := selected(res); got != "third" {
		t.Fatalf("placed %q; want the one phase", got)
	}
	// A phase already in place is not placed again by a resume, even named.
	done := opts(t, dir)
	done.Only, done.Resume = "second", true
	res, err = Run(context.Background(), done)
	if err != nil {
		t.Fatalf("--phase second --resume: %v", err)
	}
	if got := selected(res); got != "" {
		t.Fatalf("a resume re-placed %q, which the tree has in place", got)
	}
	if !statusOf(t, opts(t, dir)).Done() {
		t.Error("the product should be complete")
	}
}

// A redo is placed whatever the tree says — but never outside the window. A
// stopped build that redid a phase past its stop would deploy a code-only app.
func TestARedoOutsideTheWindowIsRefused(t *testing.T) {
	o := opts(t, t.TempDir())
	o.Until, o.Resume, o.Redo = "first", true, []string{"third"}
	_, err := Run(context.Background(), o)
	if err == nil {
		t.Fatal("a redo past the --until stop was accepted")
	}
	if !strings.Contains(err.Error(), "third") || !strings.Contains(err.Error(), "--until first") {
		t.Errorf("the refusal must name the phase and the window; got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(o.OutDir, "a.txt")); statErr == nil {
		t.Error("a refused selection placed a file")
	}
}

// A STOPPED BUILD STILL SAYS IT ENDED. The platform gives a repository back on
// the run-level `done`, and the bootstrap driver reports `completed` when the
// child exits 0 — so a build that stops where it was told to must end exactly
// as a full one does: phase "" and no step, last, with nothing after it.
func TestAStoppedRunEndsWithTheRunLevelDone(t *testing.T) {
	sink := &CollectingSink{}
	o := opts(t, t.TempDir())
	// What the bootstrap driver runs: hooks on, resume on, a stop.
	o.Until, o.Resume, o.RunHooks, o.Events = "second", true, true, sink
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatalf("run: %v", err)
	}
	events := snapshotEvents(sink)
	phaseDone := map[string]bool{}
	for _, e := range events {
		if e.Step == "" && e.State == EventDone {
			phaseDone[e.Phase] = true
		}
	}
	if !phaseDone["first"] || !phaseDone["second"] {
		t.Fatalf("the phases through the stop did not report done: %v", phaseDone)
	}
	if len(events) == 0 {
		t.Fatal("no events")
	}
	end := events[len(events)-1]
	if end.Phase != "" || end.Step != "" || end.State != EventDone {
		t.Fatalf("the last event is %+v; want the run-level done", end)
	}
	var thirdSkipped bool
	for _, e := range events {
		if e.Phase == "third" && e.State != EventSkipped {
			t.Fatalf("the phase past the stop reported %s", e.State)
		}
		if e.Phase == "third" && e.State == EventSkipped {
			thirdSkipped = true
		}
	}
	if !thirdSkipped {
		t.Error("the phase past the stop was not reported at all")
	}
}

func TestUnknownPhaseNamesTheOnesThatExist(t *testing.T) {
	o := opts(t, t.TempDir())
	o.Only = "fourth"
	_, err := Run(context.Background(), o)
	if err == nil {
		t.Fatal("expected an error for an unknown phase")
	}
	if !strings.Contains(err.Error(), "first, second, third") {
		t.Errorf("the error should list the phases that do exist; got %v", err)
	}
}

// A partial run must not erase the record of phases it did not touch.
func TestPartialRunKeepsTheEarlierModuleRecords(t *testing.T) {
	out := t.TempDir()
	firstOnly := opts(t, out)
	firstOnly.Only = "first"
	if _, err := Run(context.Background(), firstOnly); err != nil {
		t.Fatalf("first: %v", err)
	}
	secondOnly := opts(t, out)
	secondOnly.Only = "second"
	if _, err := Run(context.Background(), secondOnly); err != nil {
		t.Fatalf("second: %v", err)
	}
	prov, err := ReadProvenance(out)
	if err != nil {
		t.Fatalf("read provenance: %v", err)
	}
	names := map[string]bool{}
	for _, m := range prov.Modules {
		names[m.Name] = true
	}
	if !names["one"] {
		t.Error("the lock lost the record of the module the first run placed")
	}
	if !names["two"] {
		t.Error("the lock is missing the module the second run placed")
	}
}

func TestRecoverInputsReusesWhatTheProductWasBuiltWith(t *testing.T) {
	out := t.TempDir()
	if _, err := Run(context.Background(), opts(t, out)); err != nil {
		t.Fatalf("run: %v", err)
	}
	// A fresh container has no values file at all.
	got, err := RecoverInputs(out, nil, nil)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if got["productName"] != "acme" {
		t.Errorf("inputs should come back from the tree, got %v", got)
	}
}

func TestRecoverInputsRefusesAConflictAndNamesBoth(t *testing.T) {
	out := t.TempDir()
	if _, err := Run(context.Background(), opts(t, out)); err != nil {
		t.Fatalf("run: %v", err)
	}
	_, err := RecoverInputs(out, map[string]string{"productName": "different"}, nil)
	if err == nil {
		t.Fatal("a disagreeing input must be refused, not applied")
	}
	msg := err.Error()
	if !strings.Contains(msg, "acme") || !strings.Contains(msg, "different") {
		t.Errorf("the error must name both values; got %v", err)
	}
}

func TestRecoverInputsOnAFirstRunIsANoOp(t *testing.T) {
	got, err := RecoverInputs(t.TempDir(), map[string]string{"productName": "acme"}, nil)
	if err != nil {
		t.Fatalf("no lock is not an error: %v", err)
	}
	if got["productName"] != "acme" {
		t.Errorf("given inputs should pass through, got %v", got)
	}
}
