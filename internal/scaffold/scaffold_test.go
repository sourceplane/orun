package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/objectstore"
)

// a minimal single-component blueprint (inline, one template module).
const singleComponentBP = `apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: cloudflare-worker-svc
inputs:
  serviceName: { type: string, pattern: "^[a-z][a-z0-9-]*$", required: true }
  domain:      { type: string, required: true }
modules:
  - name: worker
    mode: template
    files:
      "apps/{{ .serviceName }}/component.yaml": |
        apiVersion: sourceplane.io/v1
        kind: Component
        metadata:
          name: {{ .serviceName }}
        spec:
          type: cloudflare-worker
          domain: {{ .domain }}
      "apps/{{ .serviceName }}/README.md": |
        # {{ .serviceName }}
`

func TestRunSingleComponentPassesGate(t *testing.T) {
	ctx := context.Background()
	out := t.TempDir()
	store := objectstore.NewMemStore(objectstore.AlgoSHA256)

	res, err := Run(ctx, Options{
		Blueprint: []byte(singleComponentBP),
		Inputs:    map[string]string{"serviceName": "billing-api", "domain": "platform-billing"},
		OutDir:    out,
		Store:     store,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// The generated component.yaml exists and passed both parsers (gate).
	comp := filepath.Join(out, "apps/billing-api/component.yaml")
	data, err := os.ReadFile(comp)
	if err != nil {
		t.Fatalf("read component.yaml: %v", err)
	}
	if err := gateComponentYAML("apps/billing-api/component.yaml", data); err != nil {
		t.Fatalf("gate failed on generated component.yaml: %v", err)
	}

	// Provenance lock written.
	prov, err := ReadProvenance(out)
	if err != nil {
		t.Fatalf("read provenance: %v", err)
	}
	if prov.Blueprint.Name != "cloudflare-worker-svc" {
		t.Errorf("prov blueprint name = %q", prov.Blueprint.Name)
	}
	if prov.InputsHash == "" {
		t.Error("inputs hash empty")
	}
	if len(res.Files) != 2 {
		t.Errorf("files = %v", res.Files)
	}
}

func TestRunFailsClosedOnBadInput(t *testing.T) {
	ctx := context.Background()
	_, err := Run(ctx, Options{
		Blueprint: []byte(singleComponentBP),
		Inputs:    map[string]string{"serviceName": "Bad Name", "domain": "d"},
		OutDir:    t.TempDir(),
		Store:     objectstore.NewMemStore(objectstore.AlgoSHA256),
	})
	assertExit(t, err, 1)
}

func TestRunUnknownBlueprintExit6(t *testing.T) {
	ctx := context.Background()
	_, err := Run(ctx, Options{
		Blueprint: []byte("not: a valid\nblueprint: true\n"),
		OutDir:    t.TempDir(),
		Store:     objectstore.NewMemStore(objectstore.AlgoSHA256),
	})
	assertExit(t, err, 6)
}

func TestRunGateRejectsInvalidComponent(t *testing.T) {
	// A blueprint that emits a component.yaml missing required kind.
	bad := `apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: bad
inputs:
  name: { type: string, required: true }
modules:
  - name: c
    mode: template
    files:
      "component.yaml": |
        apiVersion: sourceplane.io/v1
        metadata:
          name: {{ .name }}
`
	ctx := context.Background()
	_, err := Run(ctx, Options{
		Blueprint: []byte(bad),
		Inputs:    map[string]string{"name": "svc"},
		OutDir:    t.TempDir(),
		Store:     objectstore.NewMemStore(objectstore.AlgoSHA256),
	})
	assertExit(t, err, 1) // gate failure (fail closed)
}

func TestRunIdempotent(t *testing.T) {
	ctx := context.Background()
	store := objectstore.NewMemStore(objectstore.AlgoSHA256)
	inputs := map[string]string{"serviceName": "svc-x", "domain": "d"}

	out1 := t.TempDir()
	if _, err := Run(ctx, Options{Blueprint: []byte(singleComponentBP), Inputs: inputs, OutDir: out1, Store: store}); err != nil {
		t.Fatalf("run1: %v", err)
	}
	out2 := t.TempDir()
	if _, err := Run(ctx, Options{Blueprint: []byte(singleComponentBP), Inputs: inputs, OutDir: out2, Store: store}); err != nil {
		t.Fatalf("run2: %v", err)
	}
	// Same blueprint + inputs ⇒ byte-identical component.yaml (determinism).
	c1, _ := os.ReadFile(filepath.Join(out1, "apps/svc-x/component.yaml"))
	c2, _ := os.ReadFile(filepath.Join(out2, "apps/svc-x/component.yaml"))
	if string(c1) != string(c2) {
		t.Fatal("scaffold is not idempotent across runs")
	}
	p1, _ := ReadProvenance(out1)
	p2, _ := ReadProvenance(out2)
	if p1.InputsHash != p2.InputsHash {
		t.Fatal("inputs hash not stable")
	}
}
