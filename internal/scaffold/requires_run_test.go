package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/objectstore"
)

// A whole-blueprint run, and what a probe is for (orun-bootstrap-engine BE-O11).
//
// Preconditions are checked before the first byte is written. Until BE-O11 that
// meant a run placing EVERY phase had `workers` ask whether `infrastructure`
// was on disk during the very run about to write it — and always hear no. So a
// blueprint declaring `requires.phases` could only ever be run one phase at a
// time, and a dry instantiation, which is a check every baseline needs, could
// not run at all.
//
// The neighbouring cases in gates_test.go are the ones this must NOT soften: a
// phase run alone still refuses when its predecessor is genuinely absent, and
// still passes once it is on disk.

func TestAWholeBlueprintRunSatisfiesItsOwnPhaseRequirements(t *testing.T) {
	out := t.TempDir()
	res, err := Run(context.Background(), Options{
		Blueprint: []byte(requiresBlueprint), OutDir: out,
		Store: objectstore.NewMemStore(objectstore.AlgoSHA256),
	})
	if err != nil {
		t.Fatalf("a run placing every phase should satisfy its own requires: %v", err)
	}
	if len(res.Files) != 2 {
		t.Fatalf("placed %v, want both files", res.Files)
	}
	for _, name := range []string{"infra.txt", "workers.txt"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Errorf("%s was not written: %v", name, err)
		}
	}
}

// `--until` is a whole-run too, just a shorter one: everything it places is
// ordered, so a requirement inside the selection is satisfied by the selection.
func TestUntilSatisfiesRequirementsInsideTheSelection(t *testing.T) {
	if _, err := Run(context.Background(), Options{
		Blueprint: []byte(requiresBlueprint), OutDir: t.TempDir(),
		Store: objectstore.NewMemStore(objectstore.AlgoSHA256), Until: "workers",
	}); err != nil {
		t.Errorf("--until workers places infrastructure first: %v", err)
	}
}

// ── a probe gates the work, not the bytes ─────────────────────────────────

const probeBlueprint = `
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
  - name: 01-deploy
    modules: [only]
    requires:
      probe:
        - id: wiring
          uses: orun.http/probe@v1
          with:
            urls: ["https://example.invalid/health"]
`

type countingRunner struct{ calls int }

func (c *countingRunner) Run(_ context.Context, _ string, _ ActionInput) (map[string]string, error) {
	c.calls++
	return nil, nil
}

// Placement writes files. It does not deploy and does not read a secret, so
// with hooks off there is nothing for a probe to protect — and probing anyway
// is what made a baseline's own dry instantiation impossible: it needed a
// workspace and a live provider to write a temp directory.
func TestAPlacementWithNoHooksDoesNotProbe(t *testing.T) {
	rec := &countingRunner{}
	if _, err := Run(context.Background(), Options{
		Blueprint: []byte(probeBlueprint), OutDir: t.TempDir(),
		Store: objectstore.NewMemStore(objectstore.AlgoSHA256), Actions: rec,
	}); err != nil {
		t.Fatalf("a placement should not need a live provider: %v", err)
	}
	if rec.calls != 0 {
		t.Errorf("probed %d time(s) for a run that executes no hooks", rec.calls)
	}
}

// With hooks on, the probe is exactly what it was written for.
func TestARunThatExecutesHooksStillProbes(t *testing.T) {
	rec := &countingRunner{}
	if _, err := Run(context.Background(), Options{
		Blueprint: []byte(probeBlueprint), OutDir: t.TempDir(),
		Store: objectstore.NewMemStore(objectstore.AlgoSHA256), Actions: rec,
		RunHooks: true,
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.calls == 0 {
		t.Error("a run that executes hooks must still check what they depend on")
	}
}
