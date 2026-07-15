package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/objectstore"
)

const upgradeBPv1 = `apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: up
inputs:
  name: { type: string, required: true }
modules:
  - name: c
    mode: template
    files:
      "component.yaml": |
        apiVersion: sourceplane.io/v1
        kind: Component
        metadata:
          name: {{ .name }}
        spec:
          type: cloudflare-worker
          domain: d1
      "src/app.go": |
        package app // human-owned feature code
`

// v2 bumps the blueprint-owned component.yaml (domain d1 -> d2) but leaves the
// feature file's template unchanged.
const upgradeBPv2 = `apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: up
inputs:
  name: { type: string, required: true }
modules:
  - name: c
    mode: template
    files:
      "component.yaml": |
        apiVersion: sourceplane.io/v1
        kind: Component
        metadata:
          name: {{ .name }}
        spec:
          type: cloudflare-worker
          domain: d2
      "src/app.go": |
        package app // human-owned feature code
`

func TestUpgradeUpdatesBlueprintOwnedFile(t *testing.T) {
	ctx := context.Background()
	store := objectstore.NewMemStore(objectstore.AlgoSHA256)
	out := t.TempDir()

	if _, err := Run(ctx, Options{Blueprint: []byte(upgradeBPv1), Inputs: map[string]string{"name": "svc"}, OutDir: out, Store: store}); err != nil {
		t.Fatalf("run: %v", err)
	}

	res, err := Upgrade(ctx, UpgradeOptions{TargetDir: out, NewBlueprint: []byte(upgradeBPv2), Store: store, Apply: true})
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}

	statusOf := map[string]FileMergeStatus{}
	for _, m := range res.Merges {
		statusOf[m.Path] = m.Status
	}
	if statusOf["component.yaml"] != MergeUpdated {
		t.Fatalf("component.yaml status = %q, want updated", statusOf["component.yaml"])
	}
	if statusOf["src/app.go"] != MergeUnchanged {
		t.Fatalf("src/app.go status = %q, want unchanged", statusOf["src/app.go"])
	}
	got, _ := os.ReadFile(filepath.Join(out, "component.yaml"))
	if !strings.Contains(string(got), "domain: d2") {
		t.Fatalf("component.yaml not upgraded: %s", got)
	}
}

func TestUpgradeSurfacesConflictOnHumanEdit(t *testing.T) {
	ctx := context.Background()
	store := objectstore.NewMemStore(objectstore.AlgoSHA256)
	out := t.TempDir()

	if _, err := Run(ctx, Options{Blueprint: []byte(upgradeBPv1), Inputs: map[string]string{"name": "svc"}, OutDir: out, Store: store}); err != nil {
		t.Fatalf("run: %v", err)
	}
	// Human edits the blueprint-owned component.yaml.
	comp := filepath.Join(out, "component.yaml")
	orig, _ := os.ReadFile(comp)
	edited := strings.Replace(string(orig), "domain: d1", "domain: HUMAN-EDIT", 1)
	if err := os.WriteFile(comp, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Upgrade(ctx, UpgradeOptions{TargetDir: out, NewBlueprint: []byte(upgradeBPv2), Store: store, Apply: true})
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	status := ""
	for _, m := range res.Merges {
		if m.Path == "component.yaml" {
			status = string(m.Status)
		}
	}
	if status != string(MergeConflict) {
		t.Fatalf("expected conflict, got %q", status)
	}
	// Conflict must NOT overwrite the human's edit.
	after, _ := os.ReadFile(comp)
	if !strings.Contains(string(after), "HUMAN-EDIT") {
		t.Fatal("upgrade overwrote a human edit on conflict (design §11 violated)")
	}
}
