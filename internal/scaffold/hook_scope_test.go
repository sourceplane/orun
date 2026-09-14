package scaffold

import (
	"context"
	"strings"
	"testing"
)

// The `with:` scope (orun-bootstrap-engine BE-O9).
//
// Before this, a hook's parameters could see exactly one thing: the outputs of
// earlier hooks in the same phase. Every other expression a baseline would
// naturally write — the phase it is in, the answer the operator gave — failed
// at run time under missingkey=error, which meant a blueprint could not say
// "open the PR for THIS phase" without the phase name being typed again on
// every line that needed it.

const scopeBlueprint = `
apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: bp
inputs:
  epicslug:
    type: string
    default: infra-baselining
  token:
    type: string
    secret: true
    default: hunter2
modules:
  - name: only
    mode: template
    files:
      README.md: "hi"
phases:
  - name: 05-edge
    title: The API edge
    modules: [only]
    hooks:
      - id: task
        uses: orun.http/probe@v1
        with:
          urls: ["https://example.test/health"]
      - id: land
        uses: orun.http/probe@v1
        with:
          urls: ["https://example.test/health"]
          expectStatus: "{{ .phase.hooks.task.outputs.checked }}"
          timeoutSeconds: "{{ .hooks.task.outputs.checked }}"
`

// runScopeHooks drives one phase's hooks with a recording runner and hands
// back what the second hook was actually called with.
func runScopeHooks(t *testing.T, body string, with map[string]any) map[string]any {
	t.Helper()
	bp, err := ParseBlueprint([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rec := &recordingRunner{outputs: map[string]map[string]string{
		"orun.http/probe@v1": {"checked": "7"},
	}}
	hr := &hookRunner{outDir: t.TempDir(), actions: rec, inputs: with, phase: bp.Phases[0]}
	hr.resetOutputs()
	if _, err := hr.run(context.Background(), bp.Phases[0].Hooks.All()); err != nil {
		t.Fatalf("run hooks: %v", err)
	}
	return rec.calls[len(rec.calls)-1].params
}

func TestAPhaseHookSeesItsPhaseAndTheInputs(t *testing.T) {
	const body = `
apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: bp
inputs:
  epicslug:
    type: string
    default: infra-baselining
modules:
  - name: only
    mode: template
    files:
      README.md: "hi"
phases:
  - name: 05-edge
    title: The API edge
    modules: [only]
    hooks:
      - id: land
        uses: orun.pr/land@v1
        with:
          task: "T-1"
          branchSlug: "{{ .phase.name }}"
          title: "{{ .phase.title }}: api-edge"
          epic: "{{ .inputs.epicslug }}"
`
	params := runScopeHooks(t, body, map[string]any{"epicslug": "infra-baselining"})

	for name, want := range map[string]string{
		"branchSlug": "05-edge",
		"title":      "The API edge: api-edge",
		"epic":       "infra-baselining",
	} {
		if got := params[name]; got != want {
			t.Errorf("%s = %v, want %q", name, got, want)
		}
	}
}

// A phase with no title still answers .phase.title — with its name, which is
// what a person calls a phase that never named itself anything else. An empty
// string here would put a bare colon in a PR title.
func TestPhaseTitleFallsBackToTheName(t *testing.T) {
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
  - name: 08-docs
    modules: [only]
    hooks:
      - id: land
        uses: orun.pr/land@v1
        with:
          task: "T-1"
          title: "{{ .phase.title }}"
`
	params := runScopeHooks(t, body, nil)
	if got := params["title"]; got != "08-docs" {
		t.Errorf("title = %v, want %q", got, "08-docs")
	}
}

// Hook outputs are reachable both ways. `.hooks.<id>` is the short spelling a
// hook already used; `.phase.hooks.<id>` is the one that reads correctly
// beside `.phase.name` in the same block.
func TestHookOutputsAreReachableUnderPhaseToo(t *testing.T) {
	params := runScopeHooks(t, scopeBlueprint, map[string]any{"epicslug": "x", "token": "<secret>"})
	if got := params["expectStatus"]; got != "7" {
		t.Errorf(".phase.hooks.task.outputs.checked = %v, want \"7\"", got)
	}
	if got := params["timeoutSeconds"]; got != "7" {
		t.Errorf(".hooks.task.outputs.checked = %v, want \"7\"", got)
	}
}

// The scope carries the SECRET-FREE inputs. A blueprint that reaches for a
// secret gets the redaction, not the value — a hook that needs a credential
// gets one brokered at resolve time, and an input rendered into a parameter
// would travel through an argv, a PR body or a task brief on the way.
func TestASecretInputDoesNotReachAHookParameter(t *testing.T) {
	const body = `
apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: bp
inputs:
  token:
    type: string
    secret: true
    default: hunter2
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
        with:
          task: "T-1"
          body: "{{ .inputs.token }}"
`
	bp, err := ParseBlueprint([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	values, err := CollectInputs(bp.Inputs, nil)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	params := runScopeHooks(t, body, values.nonSecretFields())
	if got := params["body"]; got == "hunter2" {
		t.Fatal("a secret input reached a hook parameter")
	} else if got != "<secret>" {
		t.Errorf("body = %v, want the redaction %q", got, "<secret>")
	}
}

// Reaching for something the scope does not carry fails AT THE LINE, rather
// than rendering empty and travelling on as a parameter that means something
// else. `--phase 05-edge` against a lock that named no branch is worse than a
// refusal.
func TestAnUnknownScopeKeyIsAnErrorNotAnEmptyString(t *testing.T) {
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
        with:
          task: "{{ .provenance.blueprint.digest }}"
`
	bp, err := ParseBlueprint([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	hr := &hookRunner{outDir: t.TempDir(), actions: &recordingRunner{}, phase: bp.Phases[0]}
	hr.resetOutputs()
	_, err = hr.run(context.Background(), bp.Phases[0].Hooks.All())
	if err == nil {
		t.Fatal("expected an error for an unknown scope key")
	}
	if !strings.Contains(err.Error(), "provenance") {
		t.Errorf("the error should name what was reached for; got %v", err)
	}
}

// The blueprint's own directory travels with the action, distinct from the
// product tree. A task contract, a policy document — the baseline's machinery
// is authored beside the blueprint and deliberately not copied into the
// product, so an action that takes a file path needs both to resolve one.
func TestAnActionReceivesTheBaselineDirectory(t *testing.T) {
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
      - id: probe
        uses: orun.http/probe@v1
        with:
          urls: ["https://example.test/health"]
`
	bp, err := ParseBlueprint([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	seen := &baseDirRunner{}
	hr := &hookRunner{outDir: "/tmp/product", baseDir: "/tmp/baseline", actions: seen, phase: bp.Phases[0]}
	hr.resetOutputs()
	if _, err := hr.run(context.Background(), bp.Phases[0].Hooks.All()); err != nil {
		t.Fatalf("run hooks: %v", err)
	}
	if seen.dir != "/tmp/product" || seen.baseDir != "/tmp/baseline" {
		t.Errorf("action saw dir=%q baseDir=%q, want /tmp/product and /tmp/baseline", seen.dir, seen.baseDir)
	}
}

type baseDirRunner struct {
	dir     string
	baseDir string
}

func (b *baseDirRunner) Run(_ context.Context, _ string, in ActionInput) (map[string]string, error) {
	b.dir, b.baseDir = in.Dir, in.BaseDir
	return nil, nil
}

// Every parameter a baseline actually needs to template is a LIST: the URLs a
// phase probes, the secret keys it requires. A stringList that passed through
// unrendered would put the literal text `{{ .inputs.repoName }}` into an HTTP
// request and then report it as a failed probe — a wrong answer dressed as a
// real one.
func TestExpressionsInsideAListParameterAreRendered(t *testing.T) {
	const body = `
apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: bp
inputs:
  repoName:
    type: string
    default: acme-cloud
  workersDevSubdomain:
    type: string
    default: acme
modules:
  - name: only
    mode: template
    files:
      README.md: "hi"
phases:
  - name: 05-edge
    modules: [only]
    hooks:
      - id: verify
        uses: orun.http/probe@v1
        with:
          urls:
            - "https://{{ .inputs.repoName }}-api-edge-stage.{{ .inputs.workersDevSubdomain }}.workers.dev/health"
            - "https://example.test/static"
`
	params := runScopeHooks(t, body, map[string]any{"repoName": "acme-cloud", "workersDevSubdomain": "acme"})
	urls, ok := params["urls"].([]any)
	if !ok {
		t.Fatalf("urls = %#v, want a list", params["urls"])
	}
	want := "https://acme-cloud-api-edge-stage.acme.workers.dev/health"
	if urls[0] != want {
		t.Errorf("urls[0] = %v, want %q", urls[0], want)
	}
	if urls[1] != "https://example.test/static" {
		t.Errorf("a list element with no expression should pass through; got %v", urls[1])
	}
}

// The failure inside a list names the element, not just the parameter. "urls
// is wrong" sends a reader to a block of six; "urls[3]" sends them to a line.
func TestAFailureInsideAListNamesTheElement(t *testing.T) {
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
      - id: verify
        uses: orun.http/probe@v1
        with:
          urls:
            - "https://example.test/ok"
            - "https://{{ .nope.here }}/bad"
`
	bp, err := ParseBlueprint([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	hr := &hookRunner{outDir: t.TempDir(), actions: &recordingRunner{}, phase: bp.Phases[0]}
	hr.resetOutputs()
	_, err = hr.run(context.Background(), bp.Phases[0].Hooks.All())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "urls[1]") {
		t.Errorf("the error should name the offending element; got %v", err)
	}
}

// An argv hook runs in the PRODUCT tree, and a baseline's machinery — the
// rebrand tool, a bootstrap helper — is deliberately not copied there. Without
// `{{ .baseline.dir }}` such a hook can only name files the product carries,
// which is exactly the set that does not include the tools the bootstrap runs.
func TestAnArgvHookCanNameTheBaseline(t *testing.T) {
	const body = `
apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: bp
inputs:
  repoName:
    type: string
    default: acme-cloud
modules:
  - name: only
    mode: template
    files:
      README.md: "hi"
phases:
  - name: 01-scaffold
    modules: [only]
    hooks:
      - id: rebrand
        run: ["/bin/echo", "{{ .baseline.dir }}/tooling/rebrand/rebrand.mjs", "--repo", "{{ .inputs.repoName }}"]
`
	bp, err := ParseBlueprint([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	hr := &hookRunner{
		outDir:  t.TempDir(),
		baseDir: "/baselines/cirrus",
		inputs:  map[string]any{"repoName": "acme-cloud"},
		phase:   bp.Phases[0],
	}
	argv, err := hr.renderArgv(bp.Phases[0].Hooks.All()[0])
	if err != nil {
		t.Fatalf("render argv: %v", err)
	}
	want := []string{"/bin/echo", "/baselines/cirrus/tooling/rebrand/rebrand.mjs", "--repo", "acme-cloud"}
	for i := range want {
		if argv[i] != want[i] {
			t.Errorf("argv[%d] = %q, want %q", i, argv[i], want[i])
		}
	}
}

// A rendered element is exactly ONE argument however it renders — there is no
// shell between the elements, so a value carrying spaces or a semicolon stays
// a single argv entry rather than becoming two commands.
func TestARenderedArgvElementStaysOneArgument(t *testing.T) {
	const body = `
apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: bp
inputs:
  productName:
    type: string
    default: "Acme Cloud; rm -rf /"
modules:
  - name: only
    mode: template
    files:
      README.md: "hi"
phases:
  - name: 01-scaffold
    modules: [only]
    hooks:
      - id: brand
        run: ["/bin/echo", "{{ .inputs.productName }}"]
`
	bp, err := ParseBlueprint([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	hr := &hookRunner{outDir: t.TempDir(), inputs: map[string]any{"productName": "Acme Cloud; rm -rf /"}, phase: bp.Phases[0]}
	argv, err := hr.renderArgv(bp.Phases[0].Hooks.All()[0])
	if err != nil {
		t.Fatalf("render argv: %v", err)
	}
	if len(argv) != 2 {
		t.Fatalf("argv = %#v, want exactly 2 elements", argv)
	}
	if argv[1] != "Acme Cloud; rm -rf /" {
		t.Errorf("argv[1] = %q, want the value verbatim as one argument", argv[1])
	}
}
