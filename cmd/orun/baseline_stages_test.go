package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/scaffold"
)

// Two-stage builds (orun-cloud saas-orun-work WA5): scaffold, then live. An
// account with no Cloudflare connection gets a code-only app by stopping after
// scaffold (`--until`), and a later build runs the live stage (`--phase`).

// ── Readiness: a build that stops does not demand what it will not use ──────

func blueprintView(t *testing.T, raw string) *remotestate.BlueprintView {
	t.Helper()
	var v remotestate.BlueprintView
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return &v
}

// What the registry reports for an orun.work account with no Cloudflare: a
// FULL build's readiness. GitHub is not in it — the door drops it, because the
// repository is the proof — so the only provider is the one the live stage
// needs.
const notReady = `{"blueprint":{"id":"tanstack-start","tag":"baseline-v2",
  "readiness":{"integrationsReady":false,"integrations":[{"provider":"cloudflare","connected":false}]}}}`

const ready = `{"blueprint":{"id":"tanstack-start","tag":"baseline-v2",
  "readiness":{"integrationsReady":true,"integrations":[{"provider":"cloudflare","connected":true}]}}}`

// Without --until the gate is exactly what it was: a missing provider refuses
// the build by name, exit 1, before anything is written.
func TestTheReadinessGateStillRefusesAFullBuild(t *testing.T) {
	var note bytes.Buffer
	for _, until := range []string{"", "   "} {
		err := localReadinessGate(&note, blueprintView(t, notReady), until)
		if err == nil {
			t.Fatalf("until=%q: a full build started without cloudflare", until)
		}
		if code := exitCodeOf(t, err); code != 1 {
			t.Fatalf("until=%q: exit %d, want 1", until, code)
		}
		for _, want := range []string{"tanstack-start", "cloudflare", "is not connected", "Nothing has been written"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("until=%q: the refusal does not say %q: %v", until, want, err)
			}
		}
	}
	if err := localReadinessGate(&note, blueprintView(t, ready), ""); err != nil {
		t.Fatalf("a ready workspace was refused: %v", err)
	}
	if note.Len() != 0 {
		t.Fatalf("the gate printed a note for a full build: %q", note.String())
	}
}

// A code-only app: the scaffold stage needs GitHub alone, which the registry
// view cannot see, so --until does not ask it — and says what is missing so a
// hand-run stop is not a surprise.
func TestABuildThatStopsSkipsTheReadinessGateAndSaysWhatIsMissing(t *testing.T) {
	var note bytes.Buffer
	if err := localReadinessGate(&note, blueprintView(t, notReady), "01-scaffold"); err != nil {
		t.Fatalf("a scaffold-only build was refused for a provider it does not use: %v", err)
	}
	for _, want := range []string{"cloudflare", "not connected", "01-scaffold"} {
		if !strings.Contains(note.String(), want) {
			t.Fatalf("the note does not say %q: %q", want, note.String())
		}
	}
	note.Reset()
	if err := localReadinessGate(&note, blueprintView(t, ready), "01-scaffold"); err != nil {
		t.Fatalf("ready and stopping: %v", err)
	}
	if note.Len() != 0 {
		t.Fatalf("a note with nothing missing: %q", note.String())
	}
}

// ── The two stages through `orun new` ───────────────────────────────────────

const twoStageBlueprint = `apiVersion: orun.dev/v1
kind: Blueprint
metadata: { name: app }
inputs:
  reponame: { type: string, required: true }
  subdomain:
    type: string
    default: your-workers-subdomain
    lateBound: true
modules:
  - name: identity
    mode: template
    files: { ".rebrand/values.json": "{{ .reponame }} {{ .subdomain }}\n" }
  - name: deploy
    mode: template
    files: { "deploy.txt": "https://{{ .reponame }}.{{ .subdomain }}.workers.dev\n" }
phases:
  - name: 01-scaffold
    modules: [identity]
  - name: 02-live
    modules: [deploy]
`

// runNew drives the real `orun new` entry point with the flags the bootstrap
// driver passes (`--resume` always) plus the ones given here.
func runNew(t *testing.T, bp, out string, sets []string, until, phase string) error {
	t.Helper()
	type globals struct {
		blueprint, out, values, until, phase, progress string
		sets, redo                                     []string
		resume, hooks, status                          bool
		bootstrap                                      map[string]string
		sink                                           *scaffold.PlatformSink
	}
	saved := globals{scaffoldBlueprint, scaffoldOut, scaffoldValuesFile, scaffoldUntil, scaffoldPhase, scaffoldProgress,
		scaffoldSet, scaffoldRedo, scaffoldResume, scaffoldRunHooks, scaffoldStatus, scaffoldBootstrapInputs, scaffoldPlatformSink}
	t.Cleanup(func() {
		scaffoldBlueprint, scaffoldOut, scaffoldValuesFile = saved.blueprint, saved.out, saved.values
		scaffoldUntil, scaffoldPhase, scaffoldProgress = saved.until, saved.phase, saved.progress
		scaffoldSet, scaffoldRedo = saved.sets, saved.redo
		scaffoldResume, scaffoldRunHooks, scaffoldStatus = saved.resume, saved.hooks, saved.status
		scaffoldBootstrapInputs, scaffoldPlatformSink = saved.bootstrap, saved.sink
	})
	scaffoldBlueprint, scaffoldOut, scaffoldValuesFile, scaffoldSet = bp, out, "", sets
	scaffoldResume, scaffoldUntil, scaffoldPhase, scaffoldRedo = true, until, phase, nil
	scaffoldProgress, scaffoldRunHooks, scaffoldStatus = "plain", false, false
	scaffoldBootstrapInputs, scaffoldPlatformSink = nil, nil
	return runScaffoldNew(context.Background())
}

func TestACodeOnlyAppGoesLiveLater(t *testing.T) {
	dir := t.TempDir()
	bp := filepath.Join(dir, "repo-blueprint.yaml")
	if err := os.WriteFile(bp, []byte(twoStageBlueprint), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "acme")

	// 1. Scaffold only, with no Cloudflare and so no subdomain: the
	//    placeholder is placed and the live stage is not.
	if err := runNew(t, bp, out, []string{"reponame=acme"}, "01-scaffold", ""); err != nil {
		t.Fatalf("scaffold-only: %v", err)
	}
	if got := readString(t, filepath.Join(out, ".rebrand/values.json")); got != "acme your-workers-subdomain\n" {
		t.Fatalf("scaffold placed %q", got)
	}
	if _, err := os.Stat(filepath.Join(out, "deploy.txt")); err == nil {
		t.Fatal("a scaffold-only build placed the live stage")
	}

	// 2. A resume that goes live without the value is refused by name —
	//    the recorded placeholder is not recovered as though it were one.
	err := runNew(t, bp, out, nil, "", "02-live")
	if err == nil || !strings.Contains(err.Error(), "subdomain") || !strings.Contains(err.Error(), "your-workers-subdomain") {
		t.Fatalf("the live stage kept the placeholder; err = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(out, "deploy.txt")); statErr == nil {
		t.Fatal("the refused live stage placed a file")
	}

	// 3. The live stage with the value: no conflict with the recorded
	//    placeholder, the rest of the inputs recovered from the tree, and
	//    the record now carries the value.
	if err := runNew(t, bp, out, []string{"subdomain=acme-team"}, "", "02-live"); err != nil {
		t.Fatalf("live: %v", err)
	}
	if got := readString(t, filepath.Join(out, "deploy.txt")); got != "https://acme.acme-team.workers.dev\n" {
		t.Fatalf("live placed %q", got)
	}
	prov, err := scaffold.ReadProvenance(out)
	if err != nil {
		t.Fatalf("provenance: %v", err)
	}
	if prov.Inputs["subdomain"] != "acme-team" || prov.Inputs["reponame"] != "acme" {
		t.Fatalf("the record holds %v", prov.Inputs)
	}
	// What the scaffold placed is left as placed. A resume never re-places a
	// phase that is already in place — on a branded product that would undo
	// the branding — so replacing the placeholder in the scaffold's own files
	// is the baseline's live phase's work, not the engine's.
	if got := readString(t, filepath.Join(out, ".rebrand/values.json")); got != "acme your-workers-subdomain\n" {
		t.Fatalf("the live stage re-placed the scaffold's files: %q", got)
	}

	// 4. An ordinary input that disagrees with the record is still refused.
	err = runNew(t, bp, out, []string{"reponame=other"}, "", "")
	if err == nil || !strings.Contains(err.Error(), "input conflict") {
		t.Fatalf("an ordinary conflict was accepted: %v", err)
	}
}

func readString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
