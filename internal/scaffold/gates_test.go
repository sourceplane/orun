package scaffold

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sourceplane/orun/internal/objectstore"
)

const gatedBlueprint = `
apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: gated
inputs:
  domain:
    type: boolean
    default: false
modules:
  - name: core
    mode: template
    files:
      core.txt: "core"
  - name: extra
    mode: template
    files:
      extra.txt: "extra"
phases:
  - name: base
    modules: [core]
  - name: optional
    modules: [extra]
    when: "inputs.domain"
`

func gatedOpts(t *testing.T, out string, inputs map[string]string) Options {
	t.Helper()
	return Options{
		Blueprint: []byte(gatedBlueprint),
		Inputs:    inputs,
		OutDir:    out,
		Store:     objectstore.NewMemStore(objectstore.AlgoSHA256),
	}
}

func TestConditionFalseSkipsThePhase(t *testing.T) {
	out := t.TempDir()
	if _, err := Run(context.Background(), gatedOpts(t, out, nil)); err != nil {
		t.Fatalf("run: %v", err)
	}
	st, err := Derive(context.Background(), gatedOpts(t, out, nil))
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if got := stateOf(st, "optional"); got != PhaseSkipped {
		t.Errorf("an excluded phase should read as skipped, got %s", got)
	}
	// A skipped phase is not outstanding work — a product whose optional phase
	// is not wanted is finished.
	if !st.Done() {
		t.Errorf("a skipped phase must not keep a product forever unfinished; next=%q", st.Next)
	}
}

func TestConditionTruePlacesThePhase(t *testing.T) {
	out := t.TempDir()
	o := gatedOpts(t, out, map[string]string{"domain": "true"})
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatalf("run: %v", err)
	}
	st, err := Derive(context.Background(), o)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if got := stateOf(st, "optional"); got != PhaseDone {
		t.Errorf("the phase should be placed when its condition holds, got %s", got)
	}
}

func TestResumeDoesNotPlaceAnExcludedPhase(t *testing.T) {
	out := t.TempDir()
	o := gatedOpts(t, out, nil)
	o.Resume = true
	res, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	for _, ph := range res.Phases {
		if ph.Name == "optional" {
			t.Error("resume must not place a phase the condition excludes — that is work the operator asked not to have")
		}
	}
}

func TestAConditionThatDoesNotCompileIsAParseError(t *testing.T) {
	src := strings.Replace(gatedBlueprint, `when: "inputs.domain"`, `when: "inputs.domain &&"`, 1)
	if _, err := ParseBlueprint([]byte(src)); err == nil {
		t.Fatal("a broken condition must fail the parse, not silently never run the phase")
	}
}

func TestANonBooleanConditionIsRefused(t *testing.T) {
	src := strings.Replace(gatedBlueprint, `when: "inputs.domain"`, `when: "1 + 1"`, 1)
	_, err := ParseBlueprint([]byte(src))
	if err == nil {
		t.Fatal("a condition that is not a boolean must be refused")
	}
	if !strings.Contains(err.Error(), "true or false") {
		t.Errorf("the error should say what a condition must produce; got %v", err)
	}
}

const requiresBlueprint = `
apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: req
modules:
  - name: infra
    mode: template
    files:
      infra.txt: "infra"
  - name: workers
    mode: template
    files:
      workers.txt: "workers"
phases:
  - name: infrastructure
    modules: [infra]
  - name: workers
    modules: [workers]
    requires:
      phases: [infrastructure]
`

func TestRequiresRefusesWhenTheEarlierPhaseIsNotPlaced(t *testing.T) {
	out := t.TempDir()
	o := Options{Blueprint: []byte(requiresBlueprint), OutDir: out,
		Store: objectstore.NewMemStore(objectstore.AlgoSHA256), Only: "workers"}
	_, err := Run(context.Background(), o)
	if err == nil {
		t.Fatal("a phase whose requirement is unplaced must be refused")
	}
	if !strings.Contains(err.Error(), "infrastructure") {
		t.Errorf("the error should name what is missing; got %v", err)
	}
}

func TestRequiresPassesOnceTheEarlierPhaseIsPlaced(t *testing.T) {
	out := t.TempDir()
	base := Options{Blueprint: []byte(requiresBlueprint), OutDir: out,
		Store: objectstore.NewMemStore(objectstore.AlgoSHA256), Only: "infrastructure"}
	if _, err := Run(context.Background(), base); err != nil {
		t.Fatalf("infrastructure: %v", err)
	}
	next := Options{Blueprint: []byte(requiresBlueprint), OutDir: out,
		Store: objectstore.NewMemStore(objectstore.AlgoSHA256), Only: "workers"}
	if _, err := Run(context.Background(), next); err != nil {
		t.Fatalf("workers should be allowed once its requirement is placed: %v", err)
	}
}

// THE BOOTSTRAP THIS BROKE (BE-O12). A baseline that brands what it places
// rewrites the files it just wrote, so its first phase reads `drifted` from
// then on — and every later phase requiring it was refused at step two of the
// bootstrap the blueprint exists to perform. Every file IS there, which is the
// question a requirement asks.
func TestARequirementIsSatisfiedByADriftedPredecessor(t *testing.T) {
	out := t.TempDir()
	base := Options{Blueprint: []byte(requiresBlueprint), OutDir: out,
		Store: objectstore.NewMemStore(objectstore.AlgoSHA256), Only: "infrastructure"}
	if _, err := Run(context.Background(), base); err != nil {
		t.Fatalf("infrastructure: %v", err)
	}
	// What rebrand does: the placed file, with the baseline's identity gone.
	if err := os.WriteFile(filepath.Join(out, "infra.txt"), []byte("infra, branded"), 0o644); err != nil {
		t.Fatalf("brand: %v", err)
	}
	st, err := Derive(context.Background(), Options{Blueprint: []byte(requiresBlueprint),
		OutDir: out, Store: objectstore.NewMemStore(objectstore.AlgoSHA256)})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if got := stateOf(st, "infrastructure"); got != PhaseDrifted {
		t.Fatalf("this test is only meaningful if the predecessor reads drifted, got %s", got)
	}
	next := Options{Blueprint: []byte(requiresBlueprint), OutDir: out,
		Store: objectstore.NewMemStore(objectstore.AlgoSHA256), Only: "workers"}
	if _, err := Run(context.Background(), next); err != nil {
		t.Fatalf("a drifted predecessor has placed every one of its files, so the requirement holds: %v", err)
	}
}

// The states that still fail, each for a reason the tree can back.
func TestAPartialPredecessorStillFailsTheRequirement(t *testing.T) {
	out := t.TempDir()
	base := Options{Blueprint: []byte(requiresBlueprint), OutDir: out,
		Store: objectstore.NewMemStore(objectstore.AlgoSHA256), Only: "infrastructure"}
	if _, err := Run(context.Background(), base); err != nil {
		t.Fatalf("infrastructure: %v", err)
	}
	// Not edited — REMOVED. The placement is unfinished, not branded.
	if err := os.Remove(filepath.Join(out, "infra.txt")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	next := Options{Blueprint: []byte(requiresBlueprint), OutDir: out,
		Store: objectstore.NewMemStore(objectstore.AlgoSHA256), Only: "workers"}
	_, err := Run(context.Background(), next)
	if err == nil {
		t.Fatal("a predecessor missing its files must still refuse — accepting drift must not become accepting anything")
	}
	if !strings.Contains(err.Error(), "infrastructure") {
		t.Errorf("the error should name what is missing; got %v", err)
	}
}

// A requirement pointing forward is unsatisfiable by construction.
func TestRequiringALaterPhaseIsAParseError(t *testing.T) {
	src := strings.Replace(requiresBlueprint, "      phases: [infrastructure]", "      phases: [workers]", 1)
	_, err := ParseBlueprint([]byte(src))
	if err == nil {
		t.Fatal("a phase requiring itself or a later phase must be refused")
	}
	if !strings.Contains(err.Error(), "backwards") {
		t.Errorf("the error should explain the direction rule; got %v", err)
	}
}

func TestRequiresUnknownPhaseIsAParseError(t *testing.T) {
	src := strings.Replace(requiresBlueprint, "      phases: [infrastructure]", "      phases: [nonesuch]", 1)
	if _, err := ParseBlueprint([]byte(src)); err == nil {
		t.Fatal("requiring a phase that does not exist must be refused")
	}
}

func TestProbeMustBeAnAction(t *testing.T) {
	src := strings.Replace(requiresBlueprint, `    requires:
      phases: [infrastructure]`, `    requires:
      probe:
        - id: shell
          run: ["true"]`, 1)
	_, err := ParseBlueprint([]byte(src))
	if err == nil {
		t.Fatal("a probe that shells out must be refused — a probe answers a question")
	}
	if !strings.Contains(err.Error(), "uses:") {
		t.Errorf("the error should point at the action form; got %v", err)
	}
}

// flakyRunner fails a given number of times before succeeding.
type flakyRunner struct {
	mu       sync.Mutex
	failures int
	calls    int
}

func (f *flakyRunner) Run(context.Context, string, ActionInput) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls <= f.failures {
		return nil, fmt.Errorf("transient failure %d", f.calls)
	}
	return map[string]string{"ok": "yes"}, nil
}

func TestPhaseRetryRerunsTheHooks(t *testing.T) {
	hr := &hookRunner{outDir: t.TempDir(), actions: &flakyRunner{failures: 2}}
	decl := &Phase{Name: "p", Retry: &RetrySpec{Attempts: 3}}
	hooks := []Hook{{ID: "h", Uses: "orun.http/probe@v1"}}
	if _, err := runPhaseHooks(context.Background(), hr, decl, hooks); err != nil {
		t.Fatalf("two transient failures inside a budget of three should succeed: %v", err)
	}
}

func TestPhaseRetryGivesUpAndSaysHowManyTimesItTried(t *testing.T) {
	hr := &hookRunner{outDir: t.TempDir(), actions: &flakyRunner{failures: 99}}
	decl := &Phase{Name: "p", Retry: &RetrySpec{Attempts: 2}}
	hooks := []Hook{{ID: "h", Uses: "orun.http/probe@v1"}}
	_, err := runPhaseHooks(context.Background(), hr, decl, hooks)
	if err == nil {
		t.Fatal("a persistent failure must surface")
	}
	if !strings.Contains(err.Error(), "2 attempt") {
		t.Errorf("the error should say how many attempts were made; got %v", err)
	}
}

// Without a declared policy a hook runs once — a silent default retry would
// hide a real failure behind a delay.
func TestWithoutARetryPolicyAHookRunsOnce(t *testing.T) {
	flaky := &flakyRunner{failures: 1}
	hr := &hookRunner{outDir: t.TempDir(), actions: flaky}
	hooks := []Hook{{ID: "h", Uses: "orun.http/probe@v1"}}
	if _, err := runPhaseHooks(context.Background(), hr, nil, hooks); err == nil {
		t.Fatal("with no policy the single failure should surface")
	}
	if flaky.calls != 1 {
		t.Errorf("expected exactly one attempt, got %d", flaky.calls)
	}
}

func TestRetryAttemptsMustBeAtLeastOne(t *testing.T) {
	src := strings.Replace(gatedBlueprint, `    when: "inputs.domain"`,
		"    retry:\n      attempts: 0", 1)
	if _, err := ParseBlueprint([]byte(src)); err == nil {
		t.Fatal("attempts: 0 means the phase never runs; it must be refused")
	}
}
