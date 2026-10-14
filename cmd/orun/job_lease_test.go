package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/statebackend"
)

// terminalServer answers every heartbeat lease_lost, which is what the
// coordinator does once a job has completed.
type terminalServer struct {
	statebackend.Backend
	beats atomic.Int32
}

func (s *terminalServer) Heartbeat(context.Context, string, string, string) (*statebackend.HeartbeatResult, error) {
	s.beats.Add(1)
	return &statebackend.HeartbeatResult{LeaseLost: true}, nil
}

func TestJobLeases_LossAfterTerminalIsNotATakeover(t *testing.T) {
	l := newJobLeases()
	l.setInterval("a", time.Second)
	beatCtx, interval := l.start("a", context.Background())
	if interval != time.Second {
		t.Fatalf("interval = %v", interval)
	}
	if l.terminal("a") {
		t.Fatal("a job that was never preempted reported preempted")
	}
	if beatCtx.Err() == nil {
		t.Fatal("terminal must stop the heartbeat")
	}
	if l.lost("a") {
		t.Fatal("a 409 after the job completed is not a lost lease")
	}
}

func TestJobLeases_LossWhileLiveIsATakeover(t *testing.T) {
	l := newJobLeases()
	l.start("a", context.Background())
	if !l.lost("a") {
		t.Fatal("a 409 while the job runs is a lost lease")
	}
	if !l.terminal("a") {
		t.Fatal("terminal must report the preemption so no completion is sent")
	}
}

// The race from cirrus-baseline run 38511459165: a job completes, keeps
// running post-steps, and its heartbeat ticks into a 409. That must neither
// print a takeover nor cancel the job.
func TestHeartbeat_StopsAtTerminal(t *testing.T) {
	l := newJobLeases()
	l.setInterval("a", 5*time.Millisecond)
	jobCtx, jobCancel := context.WithCancel(context.Background())
	defer jobCancel()
	srv := &terminalServer{}
	var takeovers atomic.Int32

	beatCtx, interval := l.start("a", jobCtx)
	done := make(chan struct{})
	go func() {
		runHeartbeat(beatCtx, srv, "run", "a", "runner", interval, func() {
			if l.lost("a") {
				takeovers.Add(1)
				jobCancel()
			}
		}, nil)
		close(done)
	}()
	l.terminal("a") // the job reported terminal before its first beat
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("heartbeat kept running after the job reported terminal")
	}
	time.Sleep(20 * time.Millisecond)
	if takeovers.Load() != 0 || jobCtx.Err() != nil {
		t.Fatalf("a completed job was treated as taken over (%d takeovers, ctx %v)", takeovers.Load(), jobCtx.Err())
	}
}
