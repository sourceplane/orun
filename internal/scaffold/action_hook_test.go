package scaffold

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// recordingRunner is the substitute a phase-simulation test uses: it asserts
// the sequence of actions a phase performs, with their resolved parameters,
// and needs no network.
type recordingRunner struct {
	mu      sync.Mutex
	calls   []recordedCall
	outputs map[string]map[string]string
	err     error
}

type recordedCall struct {
	id     string
	params map[string]any
}

func (r *recordingRunner) Run(_ context.Context, id string, in ActionInput) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, recordedCall{id: id, params: in.Params})
	if r.err != nil {
		return nil, r.err
	}
	return r.outputs[id], nil
}

const actionBlueprint = `
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
  - name: edge
    modules: [only]
    hooks:
      - id: land
        uses: orun.http/probe@v1
        with:
          urls: ["https://example.test/health"]
      - id: converge
        uses: orun.http/probe@v1
        with:
          urls: ["https://example.test/health"]
          expectStatus: "{{ .hooks.land.outputs.checked }}"
`

func TestActionHookOutputsAreReadableByALaterHookInThePhase(t *testing.T) {
	bp, err := ParseBlueprint([]byte(actionBlueprint))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rec := &recordingRunner{outputs: map[string]map[string]string{
		"orun.http/probe@v1": {"checked": "7", "failures": "0"},
	}}
	hr := &hookRunner{outDir: t.TempDir(), actions: rec}
	hr.resetOutputs()

	if _, err := hr.run(context.Background(), bp.Phases[0].Hooks); err != nil {
		t.Fatalf("run hooks: %v", err)
	}
	if len(rec.calls) != 2 {
		t.Fatalf("expected 2 action calls, got %d", len(rec.calls))
	}
	got := rec.calls[1].params["expectStatus"]
	if got != "7" {
		t.Errorf("the second hook should read the first hook's output; expectStatus = %v, want \"7\"", got)
	}
}

// Outputs are per-phase. A resumed run executes one phase in a container that
// never saw the others, so a cross-phase reference would be a promise the
// engine cannot keep.
func TestActionOutputsDoNotLeakAcrossPhases(t *testing.T) {
	hr := &hookRunner{outDir: t.TempDir(), actions: &recordingRunner{}}
	hr.resetOutputs()
	hr.outputs["land"] = map[string]string{"checked": "7"}
	hr.resetOutputs()
	if len(hr.outputs) != 0 {
		t.Errorf("resetOutputs must clear the scope, got %v", hr.outputs)
	}
}

func TestParseRejectsMisspelledParameterWithTheLine(t *testing.T) {
	src := `
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
  - name: edge
    modules: [only]
    hooks:
      - id: verify
        uses: orun.http/probe@v1
        with:
          urlz: ["https://example.test/health"]
`
	_, err := ParseBlueprint([]byte(src))
	if err == nil {
		t.Fatal("a misspelled parameter must fail the parse")
	}
	msg := err.Error()
	if !strings.Contains(msg, "line 18") {
		t.Errorf("the error must name the offending line; got %v", err)
	}
	if !strings.Contains(msg, `"urlz"`) {
		t.Errorf("the error must quote the offending parameter; got %v", err)
	}
	if !strings.Contains(msg, "verify") {
		t.Errorf("the error must name the hook; got %v", err)
	}
}

func TestParseRejectsUnknownAction(t *testing.T) {
	src := `
apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: bp
modules:
  - name: only
    mode: template
    files:
      README.md: "hi"
hooks:
  postInstantiate:
    - id: nope
      uses: orun.pr/land@v99
`
	_, err := ParseBlueprint([]byte(src))
	if err == nil {
		t.Fatal("an unregistered action must fail the parse")
	}
	if !strings.Contains(err.Error(), "known actions:") {
		t.Errorf("the error should list the registry; got %v", err)
	}
}

func TestParseRejectsWithWithoutUses(t *testing.T) {
	src := `
apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: bp
modules:
  - name: only
    mode: template
    files:
      README.md: "hi"
hooks:
  postInstantiate:
    - id: strayparams
      run: ["true"]
      with:
        urls: ["https://example.test"]
`
	_, err := ParseBlueprint([]byte(src))
	if err == nil {
		t.Fatal("a `with:` block with no `uses:` is silently ignored today — it must be an error")
	}
	if !strings.Contains(err.Error(), "strayparams") {
		t.Errorf("the error must name the hook; got %v", err)
	}
}

func TestHookValidateIsExclusiveAcrossThreeKinds(t *testing.T) {
	cases := []struct {
		name string
		hook Hook
		ok   bool
	}{
		{"run only", Hook{ID: "a", Run: []string{"true"}}, true},
		{"uses only", Hook{ID: "b", Uses: "orun.http/probe@v1"}, true},
		{"workflow only", Hook{ID: "c", Workflow: "wf.yaml"}, true},
		{"run and uses", Hook{ID: "d", Run: []string{"true"}, Uses: "orun.http/probe@v1"}, false},
		{"uses and workflow", Hook{ID: "e", Uses: "orun.http/probe@v1", Workflow: "wf.yaml"}, false},
		{"none", Hook{ID: "f"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.hook.validate()
			if tc.ok && err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("expected invalid")
			}
		})
	}
}

// An action hook with no runner configured must say so plainly rather than
// behaving like an argv hook with an empty command.
func TestActionHookWithoutRunnerIsAClearError(t *testing.T) {
	hr := &hookRunner{outDir: t.TempDir()}
	_, err := hr.run(context.Background(), []Hook{{ID: "x", Uses: "orun.http/probe@v1"}})
	if err == nil || !strings.Contains(err.Error(), "no action runner") {
		t.Fatalf("expected a clear missing-runner error, got %v", err)
	}
}
