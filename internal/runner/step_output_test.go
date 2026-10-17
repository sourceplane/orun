package runner

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/sourceplane/orun/internal/executor"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/outtee"
)

// saas-live-runs LR8: the runner asks for a step's live sink as the step
// begins and the executor writes the step's output to it as it arrives,
// before the step ends.

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// teeingExecutor writes to the context's sink mid-step and records what the
// sink held when the step returned.
type teeingExecutor struct{ seenMidStep []string }

func (*teeingExecutor) Name() string                       { return "teeing" }
func (*teeingExecutor) Prepare(executor.ExecContext) error { return nil }
func (*teeingExecutor) Cleanup(executor.ExecContext) error { return nil }
func (e *teeingExecutor) RunStep(ctx executor.ExecContext, _ model.PlanJob, step model.PlanStep) (string, error) {
	var rec bytes.Buffer
	w := outtee.Capture(ctx.Context, &rec)
	_, _ = io.WriteString(w, step.ID+" line\n")
	if sink, ok := outtee.From(ctx.Context).(*syncBuf); ok {
		e.seenMidStep = append(e.seenMidStep, sink.String())
	}
	return strings.TrimRight(rec.String(), "\n"), nil
}

func TestTheStepsSinkReadsItsOutputWhileItRuns(t *testing.T) {
	ex := &teeingExecutor{}
	r := NewRunner("/tmp", true, io.Discard, io.Discard, false, "", false, false,
		ex, executor.RuntimeContext{}, "exec_live", 1, nil, "")
	sinks := map[string]*syncBuf{}
	var asked []StepRecord
	var logged []string
	r.Hooks = &RunnerHooks{
		StepOutput: func(jobID string, step StepRecord) io.Writer {
			asked = append(asked, step)
			sinks[step.ID] = &syncBuf{}
			return sinks[step.ID]
		},
		AfterStepLog: func(_ string, step StepRecord, output string) { logged = append(logged, output) },
	}
	if err := r.Run(threeStepPlan()); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 3 || asked[1].ID != "build" || asked[1].Index != 1 || asked[1].Total != 3 {
		t.Fatalf("StepOutput asked %+v", asked)
	}
	for i, id := range []string{"checkout", "build", "ship"} {
		if ex.seenMidStep[i] != id+" line\n" {
			t.Errorf("step %s: sink held %q before the step ended", id, ex.seenMidStep[i])
		}
		if logged[i] != id+" line" {
			t.Errorf("step %s: recorded output %q changed", id, logged[i])
		}
	}
}

func TestNoStepOutputHookNoSink(t *testing.T) {
	ex := &teeingExecutor{}
	r := NewRunner("/tmp", true, io.Discard, io.Discard, false, "", false, false,
		ex, executor.RuntimeContext{}, "exec_nolive", 1, nil, "")
	r.Hooks = &RunnerHooks{}
	if err := r.Run(threeStepPlan()); err != nil {
		t.Fatal(err)
	}
	if len(ex.seenMidStep) != 0 {
		t.Fatalf("a sink appeared without the hook: %q", ex.seenMidStep)
	}
}
