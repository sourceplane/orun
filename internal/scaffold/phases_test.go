package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/objectstore"
)

func phasedBP(phases string) string {
	return `apiVersion: orun.dev/v1
kind: Blueprint
metadata: { name: phased }
inputs:
  name: { type: string, required: true }
modules:
  - name: contracts
    mode: template
    files: { "packages/contracts/{{ .name }}.txt": "c" }
  - name: worker
    mode: template
    dependsOn: [contracts]
    files: { "apps/{{ .name }}/main.txt": "w" }
  - name: infra
    mode: template
    files: { "infra/{{ .name }}.tf": "i" }
` + phases
}

func TestPhasesOrderingBarrier(t *testing.T) {
	doc := phasedBP(`phases:
  - name: foundation
    modules: [contracts]
  - name: services
    modules: [worker, infra]
`)
	bp, err := ParseBlueprint([]byte(doc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	phases, err := planPhases(bp)
	if err != nil {
		t.Fatalf("planPhases: %v", err)
	}
	if len(phases) != 2 || phases[0].Name != "foundation" || phases[1].Name != "services" {
		t.Fatalf("phases = %+v", phases)
	}
	// foundation places contracts; services places infra + worker.
	if len(phases[0].Batches) != 1 || phases[0].Batches[0][0] != "contracts" {
		t.Fatalf("foundation batches = %v", phases[0].Batches)
	}
	got := map[string]bool{}
	for _, b := range phases[1].Batches {
		for _, n := range b {
			got[n] = true
		}
	}
	if !got["worker"] || !got["infra"] {
		t.Fatalf("services phase missing modules: %v", phases[1].Batches)
	}
}

func TestPhasesRejectUncoveredModule(t *testing.T) {
	doc := phasedBP(`phases:
  - name: only
    modules: [contracts]
`)
	_, err := ParseBlueprint([]byte(doc))
	if err == nil || !strings.Contains(err.Error(), "in no phase") {
		t.Fatalf("expected uncovered-module error, got %v", err)
	}
}

func TestPhasesRejectForwardDependency(t *testing.T) {
	// worker (phase 1) depends on contracts (phase 2) — a backward barrier cross.
	doc := phasedBP(`phases:
  - name: first
    modules: [worker, infra]
  - name: second
    modules: [contracts]
`)
	_, err := ParseBlueprint([]byte(doc))
	if err == nil || !strings.Contains(err.Error(), "phase barrier") {
		t.Fatalf("expected barrier-law error, got %v", err)
	}
}

func TestPhasesRejectDuplicateModule(t *testing.T) {
	doc := phasedBP(`phases:
  - name: a
    modules: [contracts, worker]
  - name: b
    modules: [worker, infra]
`)
	_, err := ParseBlueprint([]byte(doc))
	if err == nil || !strings.Contains(err.Error(), "two phases") {
		t.Fatalf("expected duplicate-module error, got %v", err)
	}
}

func TestPhaseHooksRunInOrder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh-free touch via shell-less argv")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "order.txt")
	// Two phases, each with a hook that appends its name to a marker file via a
	// no-shell argv. Use `sh -c`? No — hooks are shell-free. Use `touch` markers
	// per phase and assert both ran; order asserted by distinct files + the
	// global hook last.
	doc := `apiVersion: orun.dev/v1
kind: Blueprint
metadata: { name: h }
inputs:
  name: { type: string, required: true }
modules:
  - name: a
    mode: template
    files: { "a-{{ .name }}.txt": "a" }
  - name: b
    mode: template
    dependsOn: [a]
    files: { "b-{{ .name }}.txt": "b" }
phases:
  - name: first
    modules: [a]
    hooks:
      - id: phase1
        run: ["touch", "` + filepath.Join(dir, "phase1.done") + `"]
  - name: second
    modules: [b]
    hooks:
      - id: phase2
        run: ["touch", "` + filepath.Join(dir, "phase2.done") + `"]
hooks:
  postInstantiate:
    - id: final
      run: ["touch", "` + filepath.Join(dir, "final.done") + `"]
`
	out := t.TempDir()
	res, err := Run(context.Background(), Options{
		Blueprint: []byte(doc),
		Inputs:    map[string]string{"name": "x"},
		OutDir:    out,
		Store:     objectstore.NewMemStore(objectstore.AlgoSHA256),
		RunHooks:  true,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	wantHooks := []string{"phase1", "phase2", "final"}
	if strings.Join(res.HooksRun, ",") != strings.Join(wantHooks, ",") {
		t.Fatalf("hooks ran %v, want %v", res.HooksRun, wantHooks)
	}
	for _, f := range []string{"phase1.done", "phase2.done", "final.done"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("hook marker %s not created: %v", f, err)
		}
	}
	_ = marker
}

func TestNoPhasesIsSingleImplicitPhase(t *testing.T) {
	bp, err := ParseBlueprint([]byte(singleComponentBP))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	phases, err := planPhases(bp)
	if err != nil {
		t.Fatalf("planPhases: %v", err)
	}
	if len(phases) != 1 || phases[0].Name != "" {
		t.Fatalf("expected one implicit phase, got %+v", phases)
	}
}
