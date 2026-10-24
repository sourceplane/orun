package main

import (
	"context"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/sourceplane/orun/internal/remotestate/logpipe"
	"github.com/sourceplane/orun/internal/runner"
	"github.com/sourceplane/orun/internal/statebackend"
)

// remoteStepLogs is how a remote run's steps reach the platform's log
// (saas-live-runs LR8, LR-D9). A step used to reach it once, whole, at its
// end: the console drew a running step as not started and its log as empty
// until it finished. Now:
//
//   - start: the step's banner goes out with its `start` event as the step
//     begins, so the step reads running within the pipeline's first upload;
//   - while it runs: its output streams through a StepStream (masked, every
//     2 s or 64 KiB) as `chunk` events;
//   - end: what the stream had not sent of the step's recorded output goes
//     out with the `end` event and the outcome — always, even for a step
//     that printed nothing, so no step is left reading running.
//
// Everything goes through the run's log pipeline, so one job's events keep
// their order and an outage spills and retries them as before.
type remoteStepLogs struct {
	ctx      context.Context
	pipe     *logpipe.Pipeline
	runID    string
	redactor logpipe.Redactor
	opts     logpipe.StepStreamOptions

	mu    sync.Mutex
	steps map[string]*remoteStep
}

type remoteStep struct {
	id     string
	index  int
	stream *logpipe.StepStream
	ended  bool
}

func newRemoteStepLogs(ctx context.Context, pipe *logpipe.Pipeline, runID string, redactor logpipe.Redactor) *remoteStepLogs {
	return &remoteStepLogs{ctx: ctx, pipe: pipe, runID: runID, redactor: redactor, steps: map[string]*remoteStep{}}
}

func stepKey(jobID, stepID string) string { return jobID + "\x00" + stepID }

// stepBanner is the line that opens a step's block in the job's log: what
// `orun logs` and a raw reader split the log on.
func stepBanner(stepID string) string { return "=== " + stepID + " ===\n" }

// start sends the step's `start` event (index is 0-based) and opens its
// record; a retried or re-entered step starts afresh.
func (l *remoteStepLogs) start(jobID, stepID string, index int) {
	l.mu.Lock()
	if prev := l.steps[stepKey(jobID, stepID)]; prev != nil && prev.stream != nil {
		prev.stream.Finish()
	}
	l.steps[stepKey(jobID, stepID)] = &remoteStep{id: stepID, index: index}
	l.mu.Unlock()
	l.pipe.Append(l.ctx, l.runID, jobID, stepBanner(stepID), &statebackend.LogStep{
		StepID: stepID,
		Index:  index,
		Event:  statebackend.LogStepStart,
	})
}

// writer is the step's live output sink: a stream whose pieces go out as
// `chunk` events. Nil for a step start never opened.
func (l *remoteStepLogs) writer(jobID string, rec runner.StepRecord) io.Writer {
	l.mu.Lock()
	defer l.mu.Unlock()
	st := l.steps[stepKey(jobID, rec.ID)]
	if st == nil || st.ended {
		return nil
	}
	if st.stream == nil {
		coord := statebackend.LogStep{StepID: rec.ID, Index: st.index, Event: statebackend.LogStepChunk}
		st.stream = logpipe.NewStepStream(func(text string) {
			c := coord
			l.pipe.Append(l.ctx, l.runID, jobID, text, &c)
		}, l.redactor, l.opts)
	}
	return st.stream
}

// take closes the step's record and returns what its stream sent and held.
func (l *remoteStepLogs) take(jobID, stepID string) (st *remoteStep, sent, held string) {
	l.mu.Lock()
	st = l.steps[stepKey(jobID, stepID)]
	if st == nil || st.ended {
		l.mu.Unlock()
		return nil, "", ""
	}
	st.ended = true
	delete(l.steps, stepKey(jobID, stepID))
	l.mu.Unlock()
	if st.stream != nil {
		sent, held = st.stream.FinishRest()
	}
	return st, sent, held
}

// end sends the step's `end` event with the part of its recorded (masked)
// output the stream did not send. A step with no start (an older hook
// order, a test) sends its whole block as before.
func (l *remoteStepLogs) end(jobID string, rec runner.StepRecord, output string) {
	coord := &statebackend.LogStep{StepID: rec.ID, Index: rec.Index, Event: statebackend.LogStepEnd, Status: rec.Status, ExitCode: rec.ExitCode}
	st, sent, _ := l.take(jobID, rec.ID)
	if st == nil {
		block := stepBanner(rec.ID) + output
		if !strings.HasSuffix(block, "\n") {
			block += "\n"
		}
		l.pipe.Append(l.ctx, l.runID, jobID, block, coord)
		return
	}
	coord.Index = st.index
	tail := logpipe.StepTail(sent, output)
	if full := sent + tail; full != "" && !strings.HasSuffix(full, "\n") {
		tail += "\n"
	}
	l.pipe.Append(l.ctx, l.runID, jobID, tail, coord)
}

// terminal ends a step that reached its end without AfterStepLog — it
// printed nothing, it was a lane step run by another job, a dry run: the
// `end` event carries what the stream held, and the outcome.
func (l *remoteStepLogs) terminal(jobID, stepID, status string) {
	st, sent, held := l.take(jobID, stepID)
	if st == nil {
		return
	}
	outcome := "succeeded"
	if status == "failed" {
		outcome = "failed"
	}
	if full := sent + held; full != "" && !strings.HasSuffix(full, "\n") {
		held += "\n"
	}
	l.pipe.Append(l.ctx, l.runID, jobID, held, &statebackend.LogStep{StepID: stepID, Index: st.index, Event: statebackend.LogStepEnd, Status: outcome})
}

// closeJob ends any step of the job still open (a job cut mid-step) as
// failed, so the job's log has no step left running when it completes.
func (l *remoteStepLogs) closeJob(jobID string) {
	l.mu.Lock()
	var open []string
	for key, st := range l.steps {
		if strings.HasPrefix(key, jobID+"\x00") {
			open = append(open, st.id)
		}
	}
	l.mu.Unlock()
	for _, id := range open {
		l.terminal(jobID, id, "failed")
	}
}

// jobLogDrainTimeout bounds how long a finished job waits for its log to
// land before it reports :complete.
const jobLogDrainTimeout = 15 * time.Second
