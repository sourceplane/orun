package statebackend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeCoord is an in-memory coordinator double: it enforces deps-gated, single
// -winner claims and serves the event log in wire form, so RunLoop can drive a
// real DAG to completion over HTTP. It is the Go mirror of the DO's behavior,
// scoped to what the loop exercises.
type fakeCoord struct {
	mu         sync.Mutex
	deps       map[string][]string
	phase      map[string]string // queued|claimed|succeeded|memoized|failed
	holder     map[string]string
	memoizable map[string]string // jobID → digest: a claim is a memo hit
	events     []map[string]any
	seq        int
	epoch      int
}

func newFakeCoord(deps map[string][]string, memoizable map[string]string) *fakeCoord {
	fc := &fakeCoord{
		deps: deps, phase: map[string]string{}, holder: map[string]string{},
		memoizable: memoizable, seq: 1,
	}
	for j := range deps {
		fc.phase[j] = "queued"
	}
	fc.events = []map[string]any{{
		"seq": 1, "kind": EventRunCreated, "runId": "r", "actor": fakeActor(), "at": "t",
		"idempotencyKey": "r", "v": 1,
		"payload":        map[string]any{"planDigest": "sha256:p", "sourceHash": "sha256:s"},
	}}
	return fc
}

func fakeActor() map[string]any { return map[string]any{"id": "u1", "type": "user"} }

func (fc *fakeCoord) append(kind, job string, payload map[string]any) {
	fc.seq++
	fc.events = append(fc.events, map[string]any{
		"seq": fc.seq, "kind": kind, "runId": "r", "jobId": job, "actor": fakeActor(),
		"at": "t", "idempotencyKey": job, "v": 1, "payload": payload,
	})
}

func (fc *fakeCoord) success(job string) bool {
	return fc.phase[job] == "succeeded" || fc.phase[job] == "memoized"
}

func (fc *fakeCoord) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fc.mu.Lock()
		defer fc.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		if r.Method == http.MethodGet && strings.HasSuffix(path, "/log") {
			_ = json.NewEncoder(w).Encode(map[string]any{"events": fc.events})
			return
		}
		job, verb := jobFromPath(path)
		switch verb {
		case "claim":
			fc.handleClaim(w, job)
		case "complete":
			fc.handleComplete(w, r, job)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func (fc *fakeCoord) handleClaim(w http.ResponseWriter, job string) {
	if fc.phase[job] != "queued" {
		reason := "terminal"
		if fc.phase[job] == "claimed" {
			reason = "job_held"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"claimed": false, "reason": reason})
		return
	}
	for _, d := range fc.deps[job] {
		if !fc.success(d) {
			_ = json.NewEncoder(w).Encode(map[string]any{"claimed": false, "reason": "deps_not_ready"})
			return
		}
	}
	if dig, ok := fc.memoizable[job]; ok {
		fc.phase[job] = "memoized"
		fc.append(EventJobMemoized, job, map[string]any{"resultDigest": dig})
		_ = json.NewEncoder(w).Encode(map[string]any{"claimed": false, "cached": true, "result": map[string]any{"digest": dig}})
		return
	}
	fc.epoch++
	fc.phase[job] = "claimed"
	fc.holder[job] = "set"
	fc.append(EventJobClaimed, job, map[string]any{"runnerId": "runner-1", "leaseEpoch": fc.epoch, "leaseExpiresAt": "t2", "attempt": 1})
	_ = json.NewEncoder(w).Encode(map[string]any{"claimed": true, "leaseEpoch": fc.epoch, "leaseExpiresAt": "t2"})
}

func (fc *fakeCoord) handleComplete(w http.ResponseWriter, r *http.Request, job string) {
	var req CompleteRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if fc.phase[job] != "claimed" {
		w.WriteHeader(http.StatusConflict)
		return
	}
	fc.phase[job] = req.Outcome // succeeded | failed
	if req.Outcome == "failed" {
		fc.append(EventJobFailed, job, map[string]any{"runnerId": req.RunnerID, "leaseEpoch": req.LeaseEpoch, "reason": "job_failed", "errorText": req.ErrorText})
	} else {
		fc.append(EventJobSucceeded, job, map[string]any{"runnerId": req.RunnerID, "leaseEpoch": req.LeaseEpoch, "resultDigest": req.ResultDigest})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func diamondDeps() map[string][]string {
	return map[string][]string{"a": {}, "b": {"a"}, "c": {"a"}, "d": {"b", "c"}}
}

// diamondPlan() is defined in fold_test.go (same diamond DAG).

// recordingExec records execution order and returns a per-job result digest.
func recordingExec(order *[]string, mu *sync.Mutex, fail map[string]bool) JobExecutor {
	return func(_ context.Context, jobID string) (string, error) {
		mu.Lock()
		*order = append(*order, jobID)
		mu.Unlock()
		if fail[jobID] {
			return "", &execError{jobID}
		}
		return "sha256:result-" + jobID, nil
	}
}

type execError struct{ job string }

func (e *execError) Error() string { return "exec failed: " + e.job }

func TestRunLoopDrivesDiamondToCompletion(t *testing.T) {
	fc := newFakeCoord(diamondDeps(), nil)
	srv := httptest.NewServer(fc.handler())
	defer srv.Close()

	var order []string
	var mu sync.Mutex
	c := &CoordClient{BaseURL: srv.URL}
	err := c.RunLoop(context.Background(), RunLoopOptions{
		RunID: "r", RunnerID: "runner-1", Plan: diamondPlan(),
		Execute: recordingExec(&order, &mu, nil), MaxTicks: 20,
	})
	if err != nil {
		t.Fatalf("run loop: %v", err)
	}

	// every job ran exactly once, in dependency order
	if len(order) != 4 {
		t.Fatalf("executed %v, want 4 jobs once each", order)
	}
	pos := map[string]int{}
	for i, j := range order {
		pos[j] = i
	}
	if !(pos["a"] < pos["b"] && pos["a"] < pos["c"] && pos["b"] < pos["d"] && pos["c"] < pos["d"]) {
		t.Fatalf("execution order violates deps: %v", order)
	}
	// the run folds to succeeded
	events, _ := c.ReadLog(context.Background(), "r", 0)
	if st := Fold(events, diamondPlan()); st.Phase != "succeeded" {
		t.Fatalf("final phase = %s, want succeeded", st.Phase)
	}
}

func TestRunLoopAdoptsMemoizedHitWithoutExecuting(t *testing.T) {
	// 'a' is a memo hit: the loop must adopt it (never execute), and downstream
	// jobs still run because the fold sees 'a' as satisfied.
	fc := newFakeCoord(diamondDeps(), map[string]string{"a": "sha256:memo-a"})
	srv := httptest.NewServer(fc.handler())
	defer srv.Close()

	var order []string
	var mu sync.Mutex
	c := &CoordClient{BaseURL: srv.URL}
	if err := c.RunLoop(context.Background(), RunLoopOptions{
		RunID: "r", RunnerID: "runner-1", Plan: diamondPlan(),
		Execute: recordingExec(&order, &mu, nil), MaxTicks: 20,
	}); err != nil {
		t.Fatalf("run loop: %v", err)
	}
	for _, j := range order {
		if j == "a" {
			t.Fatalf("memoized job 'a' must not execute; order=%v", order)
		}
	}
	if len(order) != 3 {
		t.Fatalf("executed %v, want b,c,d", order)
	}
}

func TestRunLoopTerminatesOnJobFailure(t *testing.T) {
	fc := newFakeCoord(diamondDeps(), nil)
	srv := httptest.NewServer(fc.handler())
	defer srv.Close()

	var order []string
	var mu sync.Mutex
	c := &CoordClient{BaseURL: srv.URL}
	if err := c.RunLoop(context.Background(), RunLoopOptions{
		RunID: "r", RunnerID: "runner-1", Plan: diamondPlan(),
		Execute: recordingExec(&order, &mu, map[string]bool{"b": true}), MaxTicks: 20,
	}); err != nil {
		t.Fatalf("run loop: %v", err)
	}
	events, _ := c.ReadLog(context.Background(), "r", 0)
	if st := Fold(events, diamondPlan()); st.Phase != "failed" {
		t.Fatalf("final phase = %s, want failed", st.Phase)
	}
	// 'd' must never run — its dep 'b' failed
	for _, j := range order {
		if j == "d" {
			t.Fatalf("'d' ran despite failed dep 'b'; order=%v", order)
		}
	}
}
