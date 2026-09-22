package scaffold

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/objectstore"
)

// The tree cannot tell a phase that landed from one that landed and then
// failed to converge — its files are all there either way — so `--resume`
// alone leaves it as placed. A retry that knows better names it, and the
// phase is placed again whatever the tree says.
func TestAResumeRedoesTheNamedPhaseWhateverTheTreeSays(t *testing.T) {
	out := t.TempDir()
	if _, err := Run(context.Background(), Options{
		Blueprint: []byte(threePhaseBlueprint), OutDir: out,
		Store: objectstore.NewMemStore(objectstore.AlgoSHA256), Only: "01-scaffold",
	}); err != nil {
		t.Fatalf("first run: %v", err)
	}
	select_ := func(redo ...string) []string {
		opts := Options{
			Blueprint: []byte(threePhaseBlueprint), OutDir: out,
			Store: objectstore.NewMemStore(objectstore.AlgoSHA256), Resume: true, Redo: redo,
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
		return names
	}
	// Placed, so a plain resume leaves it: the tree says it is there.
	if got := strings.Join(select_(), ","); got != "02-foundation" {
		t.Fatalf("resume alone: %s", got)
	}
	// Named, so the resume places it again, and still places the rest.
	if got := strings.Join(select_("01-scaffold"), ","); got != "01-scaffold,02-foundation" {
		t.Fatalf("resume with redo: %s", got)
	}
	// The condition still rules: a redo cannot pull in a phase the inputs
	// exclude.
	if got := strings.Join(select_("07-domain"), ","); got != "02-foundation" {
		t.Fatalf("a redo of an excluded phase: %s", got)
	}
}

// A redo refines a resume; on its own there is nothing to refine, and a name
// the blueprint does not declare is a typo, not a no-op.
func TestARedoNeedsAResumeAndARealPhase(t *testing.T) {
	out := t.TempDir()
	base := Options{Blueprint: []byte(threePhaseBlueprint), OutDir: out,
		Store: objectstore.NewMemStore(objectstore.AlgoSHA256)}

	o := base
	o.Redo = []string{"01-scaffold"}
	if _, err := Run(context.Background(), o); err == nil || !strings.Contains(err.Error(), "--resume") {
		t.Fatalf("redo without resume must say so, got %v", err)
	}
	o = base
	o.Resume, o.Redo = true, []string{"99-nothing"}
	if _, err := Run(context.Background(), o); err == nil || !strings.Contains(err.Error(), "99-nothing") {
		t.Fatalf("an unknown phase must be refused by name, got %v", err)
	}
}

// A failed event carries the diagnosis as fields: which hook, what it used,
// and what the hook established before it failed — a convergence's run and
// the lanes that failed — beside the prose that always carried them.
func TestAFailedEventCarriesTheHooksOutputs(t *testing.T) {
	base := map[string]string{"files": "3", "expectedMinutes": "5"}
	err := &HookError{ID: "converge", Uses: "orun.run/watch@v1", Err: errors.New("convergence failure (run 7)"),
		Outputs: map[string]string{
			"runId": "7", "url": "https://github.com/acme/product/actions/runs/7", "conclusion": "failure",
			"resumes": "0", "failedCount": "1",
			"failedLanes": `[{"name":"api-edge · prod · Verify deploy","conclusion":"failure"}]`,
		}}
	meta, step := failureMeta(base, err)
	if step != "converge" {
		t.Fatalf("the step is the failing hook, got %q", step)
	}
	for k, want := range map[string]string{
		"files": "3", "hook": "converge", "uses": "orun.run/watch@v1", "runId": "7",
		"runUrl": "https://github.com/acme/product/actions/runs/7", "failedCount": "1",
	} {
		if meta[k] != want {
			t.Errorf("meta[%q] = %q, want %q", k, meta[k], want)
		}
	}
	if !strings.Contains(meta["failedLanes"], "api-edge") {
		t.Errorf("the lanes travel as data: %v", meta)
	}
	if base["hook"] != "" {
		t.Error("the phase's own meta must not be written to")
	}
	// The error still reads as it always did, and still unwraps.
	if err.Error() != `hook "converge" (orun.run/watch@v1): convergence failure (run 7)` {
		t.Errorf("wording changed: %s", err.Error())
	}
	plain, s := failureMeta(base, errors.New("not a hook's"))
	if s != "" || plain["hook"] != "" || len(plain) != len(base) {
		t.Errorf("a failure that is not a hook's carries only the phase's meta: %v %q", plain, s)
	}
}
