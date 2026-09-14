package scaffold

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/actions"
	"github.com/sourceplane/orun/internal/objectstore"
)

// pendingRunner reports pending for a named hook and succeeds for the rest.
type pendingRunner struct {
	mu       sync.Mutex
	pendFor  string
	calls    []string
	settleAt int // after this many calls to pendFor, stop reporting pending
	seen     int
}

func (r *pendingRunner) Run(_ context.Context, id string, in ActionInput) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, id)
	// Key on the probed URL: the seam hands an action its parameters, not the
	// id of the hook that invoked it, so this is how a fake tells the phase's
	// three hooks apart.
	if !strings.Contains(fmt.Sprint(in.Params["urls"]), r.pendFor) {
		return map[string]string{}, nil
	}
	r.seen++
	if r.settleAt > 0 && r.seen >= r.settleAt {
		return map[string]string{}, nil
	}
	return nil, &actions.PendingError{
		ID:      id,
		Pending: actions.Pending{Reason: "convergence still running", RetryAfter: 30 * time.Second},
	}
}

const awaitBlueprint = `
apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: awaiting
modules:
  - name: only
    mode: template
    files:
      a.txt: "a"
phases:
  - name: edge
    modules: [only]
    hooks:
      pre:
        - id: task
          uses: orun.http/probe@v1
          with: { urls: ["https://example.test/t"] }
      post:
        - id: land
          uses: orun.http/probe@v1
          with: { urls: ["https://example.test/l"] }
      await:
        - id: converge
          uses: orun.http/probe@v1
          with: { urls: ["https://example.test/c"] }
`

func awaitOpts(t *testing.T, out string, runner ActionRunner) Options {
	t.Helper()
	return Options{
		Blueprint: []byte(awaitBlueprint),
		OutDir:    out,
		Store:     objectstore.NewMemStore(objectstore.AlgoSHA256),
		RunHooks:  true,
		Actions:   runner,
	}
}

func TestHookSlotsRunInOrder(t *testing.T) {
	bp, err := ParseBlueprint([]byte(awaitBlueprint))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	h := bp.Phases[0].Hooks
	if len(h.Pre) != 1 || len(h.Post) != 1 || len(h.Await) != 1 {
		t.Fatalf("expected one hook per slot, got pre=%d post=%d await=%d", len(h.Pre), len(h.Post), len(h.Await))
	}
	got := []string{}
	for _, hook := range h.All() {
		got = append(got, hook.ID)
	}
	if strings.Join(got, ",") != "task,land,converge" {
		t.Errorf("All() must be pre, post, await in order; got %v", got)
	}
}

// Every blueprint written before this milestone says `hooks:` as a bare list.
// That must keep parsing, and keep meaning what it always meant.
func TestABareHookListStillMeansPost(t *testing.T) {
	src := `
apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: legacy
modules:
  - name: only
    mode: template
    files:
      a.txt: "a"
phases:
  - name: p
    modules: [only]
    hooks:
      - id: rebrand
        run: ["true"]
`
	bp, err := ParseBlueprint([]byte(src))
	if err != nil {
		t.Fatalf("the legacy list form must still parse: %v", err)
	}
	h := bp.Phases[0].Hooks
	if len(h.Post) != 1 || h.Post[0].ID != "rebrand" {
		t.Errorf("a bare list should land in post, got %+v", h)
	}
	if len(h.Pre) != 0 || len(h.Await) != 0 {
		t.Error("a bare list must not populate the other slots")
	}
}

func TestAPendingAwaitParksThePhaseRatherThanFailing(t *testing.T) {
	out := t.TempDir()
	runner := &pendingRunner{pendFor: "/c"}
	_, err := Run(context.Background(), awaitOpts(t, out, runner))
	if err == nil {
		t.Fatal("a pending await should stop the run")
	}
	parked, ok := err.(*ParkedError)
	if !ok {
		t.Fatalf("expected a ParkedError, got %T: %v", err, err)
	}
	if parked.Phase != "edge" || parked.Hook != "converge" {
		t.Errorf("the park should name the phase and hook; got %+v", parked)
	}
	if !strings.Contains(parked.Error(), "convergence still running") {
		t.Errorf("the reason must reach the operator; got %v", parked)
	}
	if !strings.Contains(parked.Error(), "--resume") {
		t.Errorf("the message should say how to continue; got %v", parked)
	}
	// A park is not a validation failure; a caller that treats it as one would
	// tear down a product that is merely mid-deploy.
	if parked.ExitCode() == 1 {
		t.Error("a parked run must not share the validation exit code")
	}
}

// The tree a parked phase placed stays valid — that is what makes resuming
// from here meaningful.
func TestAParkedPhaseStillLeavesItsTreeOnDisk(t *testing.T) {
	out := t.TempDir()
	runner := &pendingRunner{pendFor: "/c"}
	if _, err := Run(context.Background(), awaitOpts(t, out, runner)); err == nil {
		t.Fatal("expected a park")
	}
	if _, err := os.Stat(filepath.Join(out, "a.txt")); err != nil {
		t.Errorf("the placed tree must survive a park: %v", err)
	}
}

func TestRunStateRecordsWhatIsBeingWaitedOn(t *testing.T) {
	out := t.TempDir()
	runner := &pendingRunner{pendFor: "/c"}
	if _, err := Run(context.Background(), awaitOpts(t, out, runner)); err == nil {
		t.Fatal("expected a park")
	}
	st, ok := ReadRunState(out)
	if !ok {
		t.Fatal("a parked run should record what it is waiting on")
	}
	if st.Phase != "edge" || st.Hook != "converge" {
		t.Errorf("run state should name the phase and hook, got %+v", st)
	}
}

// The cache is a convenience, never the truth: deleting it must change nothing
// about what a resumed run does.
func TestDeletingRunStateChangesNothing(t *testing.T) {
	out := t.TempDir()
	parking := &pendingRunner{pendFor: "/c"}
	if _, err := Run(context.Background(), awaitOpts(t, out, parking)); err == nil {
		t.Fatal("expected a park")
	}
	if err := os.Remove(filepath.Join(out, filepath.FromSlash(RunStateRelPath))); err != nil {
		t.Fatalf("remove run state: %v", err)
	}
	if _, ok := ReadRunState(out); ok {
		t.Fatal("run state should be gone")
	}
	// Resuming re-runs the await and learns the same answer from the world.
	settled := &pendingRunner{pendFor: "/c", settleAt: 1}
	o := awaitOpts(t, out, settled)
	o.Resume = true
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatalf("a resume must not depend on the deleted cache: %v", err)
	}
}

func TestRunStateIsClearedOnceNothingIsWaiting(t *testing.T) {
	out := t.TempDir()
	parking := &pendingRunner{pendFor: "/c"}
	if _, err := Run(context.Background(), awaitOpts(t, out, parking)); err == nil {
		t.Fatal("expected a park")
	}
	if _, ok := ReadRunState(out); !ok {
		t.Fatal("expected a parked record")
	}
	settled := &pendingRunner{pendFor: "/c", settleAt: 1}
	if _, err := Run(context.Background(), awaitOpts(t, out, settled)); err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, ok := ReadRunState(out); ok {
		t.Error("the parked record must be cleared once the wait is over")
	}
}

// Retrying a wait would turn "still running" into an error after N attempts,
// when the honest answer is that it is still running.
func TestAwaitIsNotSubjectToTheRetryPolicy(t *testing.T) {
	src := strings.Replace(awaitBlueprint, "    hooks:", "    retry:\n      attempts: 3\n    hooks:", 1)
	runner := &pendingRunner{pendFor: "/c"}
	o := awaitOpts(t, t.TempDir(), runner)
	o.Blueprint = []byte(src)
	if _, err := Run(context.Background(), o); err == nil {
		t.Fatal("expected a park")
	}
	// pre and post ran once each, and await ran once — not three times.
	if runner.seen != 1 {
		t.Errorf("await should be attempted once, not retried; attempted %d times", runner.seen)
	}
}

func TestAFailingAwaitIsStillAFailure(t *testing.T) {
	out := t.TempDir()
	runner := &failingRunner{}
	_, err := Run(context.Background(), awaitOpts(t, out, runner))
	if err == nil {
		t.Fatal("a real error in an await must not be mistaken for a wait")
	}
	if _, ok := err.(*ParkedError); ok {
		t.Error("a failure is not a park")
	}
}

type failingRunner struct{}

func (failingRunner) Run(context.Context, string, ActionInput) (map[string]string, error) {
	return nil, fmt.Errorf("the deploy is broken")
}
