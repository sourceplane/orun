package main

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/redact"
	"github.com/sourceplane/orun/internal/remotestate/logpipe"
	"github.com/sourceplane/orun/internal/runner"
	"github.com/sourceplane/orun/internal/statebackend"
)

// saas-live-runs LR8: a remote run's step reaches the log as it starts,
// while it runs, and when it ends — and its job completes after the last
// chunk landed.

type wireRec struct {
	mu   sync.Mutex
	evts []string
	text strings.Builder
}

func (w *wireRec) up(_ context.Context, _, _, content string, step *statebackend.LogStep) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	ev := "none"
	if step != nil {
		ev = step.Event
		if step.Event == statebackend.LogStepEnd {
			ev += "(" + step.Status + ")"
		}
	}
	w.evts = append(w.evts, ev)
	w.text.WriteString(content)
	return nil
}

func (w *wireRec) snapshot() ([]string, string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string{}, w.evts...), w.text.String()
}

func newTestStepLogs(w *wireRec, r logpipe.Redactor) (*remoteStepLogs, *logpipe.Pipeline) {
	pipe := logpipe.New(w.up, logpipe.Options{})
	l := newRemoteStepLogs(context.Background(), pipe, "run", r)
	l.opts = logpipe.StepStreamOptions{Interval: 5 * time.Millisecond}
	return l, pipe
}

func TestAStepStartsStreamsAndEndsInOrder(t *testing.T) {
	w := &wireRec{}
	l, pipe := newTestStepLogs(w, redact.New())
	l.start("job", "build", 1)
	sink := l.writer("job", runner.StepRecord{ID: "build", Index: 1})
	_, _ = io.WriteString(sink, "compiling\n")
	deadline := time.Now().Add(2 * time.Second)
	for {
		evts, _ := w.snapshot()
		if len(evts) >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the running step's output never streamed")
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, _ = io.WriteString(sink, "linking")
	zero := 0
	l.end("job", runner.StepRecord{ID: "build", Index: 1, Status: "succeeded", ExitCode: &zero}, "compiling\nlinking")
	l.terminal("job", "build", "completed") // after AfterStepLog: nothing more
	if !pipe.Drain(context.Background(), "run", "job", 2*time.Second) {
		t.Fatal("drain")
	}
	evts, text := w.snapshot()
	if strings.Join(evts, ",") != "start,chunk,end(succeeded)" {
		t.Fatalf("events %v", evts)
	}
	if text != "=== build ===\ncompiling\nlinking\n" {
		t.Fatalf("log %q", text)
	}
}

func TestASilentStepStillEnds(t *testing.T) {
	w := &wireRec{}
	l, pipe := newTestStepLogs(w, nil)
	l.start("job", "noop", 0)
	_ = l.writer("job", runner.StepRecord{ID: "noop"})
	l.terminal("job", "noop", "failed")
	pipe.Drain(context.Background(), "run", "job", time.Second)
	evts, text := w.snapshot()
	if strings.Join(evts, ",") != "start,end(failed)" || text != "=== noop ===\n" {
		t.Fatalf("events %v log %q", evts, text)
	}
}

func TestAStepWithNoStartSendsItsBlockAsBefore(t *testing.T) {
	w := &wireRec{}
	l, pipe := newTestStepLogs(w, nil)
	l.end("job", runner.StepRecord{ID: "x", Status: "succeeded"}, "out")
	pipe.Drain(context.Background(), "run", "job", time.Second)
	evts, text := w.snapshot()
	if strings.Join(evts, ",") != "end(succeeded)" || text != "=== x ===\nout\n" {
		t.Fatalf("events %v log %q", evts, text)
	}
}

func TestAJobCutMidStepClosesItsOpenStep(t *testing.T) {
	w := &wireRec{}
	l, pipe := newTestStepLogs(w, nil)
	l.start("job", "long", 2)
	_, _ = io.WriteString(l.writer("job", runner.StepRecord{ID: "long", Index: 2}), "half")
	l.closeJob("job")
	pipe.Drain(context.Background(), "run", "job", time.Second)
	evts, text := w.snapshot()
	if evts[len(evts)-1] != "end(failed)" || !strings.HasSuffix(text, "half\n") {
		t.Fatalf("events %v log %q", evts, text)
	}
}

func TestTheStreamMasksWithTheRunsRedactor(t *testing.T) {
	w := &wireRec{}
	r := redact.New()
	r.Add("tok-abcdef-123")
	l, pipe := newTestStepLogs(w, r)
	l.start("job", "s", 0)
	sink := l.writer("job", runner.StepRecord{ID: "s"})
	_, _ = io.WriteString(sink, "using tok-abcdef-123 now\n")
	time.Sleep(30 * time.Millisecond)
	l.end("job", runner.StepRecord{ID: "s", Status: "succeeded"}, r.Filter("using tok-abcdef-123 now"))
	pipe.Drain(context.Background(), "run", "job", time.Second)
	_, text := w.snapshot()
	if strings.Contains(text, "tok-abcdef") || !strings.Contains(text, "using *** now") {
		t.Fatalf("log %q", text)
	}
}
