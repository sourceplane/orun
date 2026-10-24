package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/statebackend"
)

// scriptedBackend answers UpdateJob from a script (the last entry repeats)
// and Heartbeat with heartbeatErr, counting both.
type scriptedBackend struct {
	*fakeBackend
	mu           sync.Mutex
	updates      []error
	updateCalls  int
	heartbeats   int
	heartbeatErr error
}

func (s *scriptedBackend) UpdateJob(context.Context, string, string, string, statebackend.JobStatus, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.updateCalls
	if i >= len(s.updates) {
		i = len(s.updates) - 1
	}
	s.updateCalls++
	return s.updates[i]
}

func (s *scriptedBackend) Heartbeat(context.Context, string, string, string) (*statebackend.HeartbeatResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.heartbeats++
	if s.heartbeatErr != nil {
		return nil, s.heartbeatErr
	}
	return &statebackend.HeartbeatResult{OK: true}, nil
}

func TestUpdateJobWithRetry_TransientThenRecorded(t *testing.T) {
	transient := errors.New("complete a: unexpected status 503")
	b := &scriptedBackend{fakeBackend: &fakeBackend{}, updates: []error{transient, transient, nil}}
	err := updateJobWithRetryWindow(context.Background(), b, "run", "a", "r1", statebackend.JobStatusSuccess, "", time.Minute, time.Millisecond)
	if err != nil {
		t.Fatalf("updateJobWithRetryWindow = %v, want nil once :complete lands", err)
	}
	if b.updateCalls != 3 {
		t.Fatalf(":complete sent %d times, want 3", b.updateCalls)
	}
	// The job's own heartbeat is stopped by now: each retry renews the lease
	// itself, so a slow :complete does not outlive it (orun#748).
	if b.heartbeats != 2 {
		t.Fatalf("lease renewed %d times between retries, want 2", b.heartbeats)
	}
}

func TestUpdateJobWithRetry_StopsAtOnceWhenTheResultIsRefused(t *testing.T) {
	refused := fmt.Errorf("%w: a: the lease was lost", statebackend.ErrResultNotRecorded)
	b := &scriptedBackend{fakeBackend: &fakeBackend{}, updates: []error{refused}}
	err := updateJobWithRetryWindow(context.Background(), b, "run", "a", "r1", statebackend.JobStatusSuccess, "", time.Minute, time.Millisecond)
	if !errors.Is(err, statebackend.ErrResultNotRecorded) {
		t.Fatalf("updateJobWithRetryWindow = %v, want ErrResultNotRecorded", err)
	}
	if b.updateCalls != 1 {
		t.Fatalf(":complete sent %d times, want 1 (a lost lease is not retried)", b.updateCalls)
	}
}

func TestUpdateJobWithRetry_ReturnsTheLastErrorAfterTheWindow(t *testing.T) {
	transient := errors.New("complete a: unexpected status 429")
	b := &scriptedBackend{fakeBackend: &fakeBackend{}, updates: []error{transient}}
	err := updateJobWithRetryWindow(context.Background(), b, "run", "a", "r1", statebackend.JobStatusSuccess, "", 20*time.Millisecond, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("updateJobWithRetryWindow = %v, want the last error (429) — it used to return nothing", err)
	}
}

func TestUpdateJobWithRetry_ReportsACanceledContext(t *testing.T) {
	transient := errors.New("complete a: unexpected status 502")
	b := &scriptedBackend{fakeBackend: &fakeBackend{}, updates: []error{transient}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := updateJobWithRetryWindow(ctx, b, "run", "a", "r1", statebackend.JobStatusSuccess, "", time.Minute, time.Second)
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "502") {
		t.Fatalf("updateJobWithRetryWindow = %v, want context.Canceled carrying the last error", err)
	}
}

func TestUnrecordedResults_SortedIDs(t *testing.T) {
	u := &unrecordedResults{}
	u.add("b.verify")
	u.add("a.verify")
	if u.count() != 2 || strings.Join(u.ids(), ",") != "a.verify,b.verify" {
		t.Fatalf("unrecorded = %d %v, want 2 [a.verify b.verify]", u.count(), u.ids())
	}
}

func TestUnrecordedClaimError_NamesTheServersRecord(t *testing.T) {
	err := unrecordedClaimError("sdk.dev.verify", "queued")
	for _, want := range []string{"sdk.dev.verify", "queued", "never recorded"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("unrecordedClaimError = %q, want it to say %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "failed on another runner") {
		t.Fatalf("unrecordedClaimError = %q: a job with no result did not fail on another runner", err)
	}
}

// A failing heartbeat is said once per job, and beating carries on.
func TestRunHeartbeatReporting_ReportsTheFirstFailureOnce(t *testing.T) {
	b := &scriptedBackend{fakeBackend: &fakeBackend{}, heartbeatErr: errors.New("heartbeat a: unexpected status 429")}
	jobCtx, cancel := context.WithCancel(context.Background())
	var reports int32
	var first atomic.Value
	done := make(chan struct{})
	go func() {
		runHeartbeatReporting(jobCtx, b, "run", "a", "r1", 2*time.Millisecond, func(err error) {
			atomic.AddInt32(&reports, 1)
			first.Store(err.Error())
		}, func() { t.Error("onLeaseLost must not fire on a transient error") }, nil)
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		b.mu.Lock()
		n := b.heartbeats
		b.mu.Unlock()
		if n >= 5 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d heartbeats sent; a failing beat must not stop the heartbeat", n)
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	if got := atomic.LoadInt32(&reports); got != 1 {
		t.Fatalf("onFailing called %d times, want 1", got)
	}
	if s, _ := first.Load().(string); !strings.Contains(s, "429") {
		t.Fatalf("onFailing got %q, want the status", s)
	}
}
