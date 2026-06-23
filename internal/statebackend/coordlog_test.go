package statebackend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
)

// wire log for: created(1) → claim a(2) → succeed a(3)
var wireLog = []map[string]any{
	{"seq": 1, "kind": EventRunCreated, "runId": "r", "actor": map[string]any{"id": "u1", "type": "user"}, "at": "t", "idempotencyKey": "r", "v": 1,
		"payload": map[string]any{"planDigest": "sha256:p", "sourceHash": "sha256:s", "environment": nil}},
	{"seq": 2, "kind": EventJobClaimed, "runId": "r", "jobId": "a", "actor": map[string]any{"id": "u1", "type": "user"}, "at": "t", "idempotencyKey": "a:c:1", "v": 1,
		"payload": map[string]any{"runnerId": "r1", "leaseEpoch": 1, "leaseExpiresAt": "t2", "attempt": 1}},
	{"seq": 3, "kind": EventJobSucceeded, "runId": "r", "jobId": "a", "actor": map[string]any{"id": "u1", "type": "user"}, "at": "t", "idempotencyKey": "a:s:1", "v": 1,
		"payload": map[string]any{"runnerId": "r1", "leaseEpoch": 1, "resultDigest": "sha256:ra"}},
}

func logServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		from, _ := strconv.Atoi(r.URL.Query().Get("from"))
		events := []map[string]any{}
		for _, e := range wireLog {
			if e["seq"].(int) > from {
				events = append(events, e)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"events": events})
	}))
}

func TestReadLogFoldsToState(t *testing.T) {
	srv := logServer()
	defer srv.Close()
	c := &CoordClient{BaseURL: srv.URL}

	events, err := c.ReadLog(context.Background(), "r", 0, 0)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}

	state := Fold(events, linearPlan())
	if state.Jobs["a"].Phase != "succeeded" || state.Jobs["a"].ResultDigest != "sha256:ra" {
		t.Fatalf("a not folded from wire: %+v", state.Jobs["a"])
	}
	if !reflect.DeepEqual(state.Frontier, []string{"b"}) {
		t.Fatalf("frontier = %v, want [b]", state.Frontier)
	}
	if state.Phase != "running" || state.PlanDigest != "sha256:p" {
		t.Fatalf("run state wrong: phase=%s planDigest=%s", state.Phase, state.PlanDigest)
	}
}

func TestReadLogRespectsFromSeq(t *testing.T) {
	srv := logServer()
	defer srv.Close()
	c := &CoordClient{BaseURL: srv.URL}

	events, err := c.ReadLog(context.Background(), "r", 2, 0)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if len(events) != 1 || events[0].Seq != 3 || events[0].Kind != EventJobSucceeded {
		t.Fatalf("fromSeq filtering wrong: %+v", events)
	}
	// the decoded payload fields survive the wire round-trip
	if events[0].ResultDigest != "sha256:ra" {
		t.Fatalf("payload not decoded from wire: %+v", events[0])
	}
}
