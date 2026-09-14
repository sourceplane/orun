package runner

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"testing"

	"github.com/sourceplane/orun/internal/executor"
	"github.com/sourceplane/orun/internal/model"
)

// The step coordinate the runner hands its log sink (orun-cloud saas-step-logs
// SL-R1).
//
// The runner has always known which step it was running; what it handed the
// hook was a bare stepID, so the one sink that uploads logs dropped the rest on
// the way to the wire and the console could not say which step broke. These
// are the claims the widened record has to keep.

// scriptedExecutor returns a fixed output and error per step id.
type scriptedExecutor struct {
	out  string
	errs map[string]error
}

func (*scriptedExecutor) Name() string                       { return "scripted" }
func (*scriptedExecutor) Prepare(executor.ExecContext) error { return nil }
func (*scriptedExecutor) Cleanup(executor.ExecContext) error { return nil }
func (e *scriptedExecutor) RunStep(_ executor.ExecContext, _ model.PlanJob, step model.PlanStep) (string, error) {
	return e.out, e.errs[step.ID]
}

func threeStepPlan() *model.Plan {
	return &model.Plan{
		Jobs: []model.PlanJob{{
			ID:        "api@deploy",
			Name:      "deploy",
			Component: "api",
			Steps: []model.PlanStep{
				{ID: "checkout", Phase: "pre", Run: "git clone"},
				{ID: "build", Run: "make"},
				{ID: "ship", Phase: "post", Run: "deploy"},
			},
		}},
	}
}

func runWithHook(t *testing.T, errs map[string]error) []StepRecord {
	t.Helper()
	r := NewRunner("/tmp", true, io.Discard, io.Discard, false, "", false, false,
		&scriptedExecutor{out: "some output\n", errs: errs}, executor.RuntimeContext{}, "exec_steps", 1, nil, "")
	var got []StepRecord
	r.Hooks = &RunnerHooks{
		AfterStepLog: func(_ string, step StepRecord, _ string) { got = append(got, step) },
	}
	_ = r.Run(threeStepPlan())
	return got
}

func TestStepRecordCarriesIdentityAndPosition(t *testing.T) {
	got := runWithHook(t, nil)
	if len(got) != 3 {
		t.Fatalf("want a record per step, got %d", len(got))
	}
	for i, want := range []string{"checkout", "build", "ship"} {
		if got[i].ID != want {
			t.Errorf("record %d: ID = %q, want %q", i, got[i].ID, want)
		}
		// 0-BASED. OnStepStart reports 1-based because it renders "step 3 of 7";
		// this one is an address into the plan, and the two are different
		// numbers on purpose.
		if got[i].Index != i {
			t.Errorf("record %d: Index = %d, want %d", i, got[i].Index, i)
		}
		if got[i].Total != 3 {
			t.Errorf("record %d: Total = %d, want 3", i, got[i].Total)
		}
	}
}

func TestStepRecordReportsCleanExitAsZeroNotAbsent(t *testing.T) {
	got := runWithHook(t, nil)
	for _, rec := range got {
		if rec.Status != StepSucceeded {
			t.Errorf("%s: Status = %q, want %q", rec.ID, rec.Status, StepSucceeded)
		}
		// Zero is a real outcome. A nil here would read downstream as "no exit
		// code to report", which is what a timeout looks like.
		if rec.ExitCode == nil || *rec.ExitCode != 0 {
			t.Errorf("%s: ExitCode = %v, want 0", rec.ID, rec.ExitCode)
		}
	}
}

func TestStepRecordCarriesTheFailingExitCode(t *testing.T) {
	// The shape a real command failure arrives in.
	failing := exec.Command("sh", "-c", "exit 3")
	err := failing.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Skipf("no *exec.ExitError available on this host: %v", err)
	}

	got := runWithHook(t, map[string]error{"build": err})
	if len(got) < 2 {
		t.Fatalf("want the failing step's record, got %d", len(got))
	}
	build := got[1]
	if build.ID != "build" || build.Status != StepFailed {
		t.Fatalf("record = %+v, want build/failed", build)
	}
	if build.ExitCode == nil || *build.ExitCode != 3 {
		t.Errorf("ExitCode = %v, want 3", build.ExitCode)
	}
	// A failed step stops the job, so nothing after it reports.
	if len(got) != 2 {
		t.Errorf("steps after a failure must not report: got %d records", len(got))
	}
}

func TestStepExitCodeIsAbsentWhenThereIsNone(t *testing.T) {
	// A timeout or a cancellation is a real failure with NO exit code, and
	// reporting 0 for it would say "succeeded cleanly".
	for _, err := range []error{errors.New("substitution failed"), context.Canceled, context.DeadlineExceeded} {
		if code, ok := stepExitCode(err); ok {
			t.Errorf("stepExitCode(%v) = %d, true; want absent", err, code)
		}
	}
	if _, ok := stepExitCode(nil); ok {
		t.Error("stepExitCode(nil) must report absent")
	}
}
