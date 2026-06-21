package statebackend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/remotestate"
)

// CoordBackend drives the coordination cycle over the native v2 wire. This
// exercises it against a fake §3 server: claim outcome mapping, the lease-epoch
// threaded from :claim into :heartbeat/:complete, and the runnable frontier.
func TestCoordBackendDrivesNativeWire(t *testing.T) {
	var gotHeartbeatEpoch, gotCompleteEpoch int
	var gotCompleteOutcome string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, ":claim"):
			seg := path[strings.LastIndex(path, "/")+1:]
			job := strings.TrimSuffix(seg, ":claim")
			switch job {
			case "a":
				_, _ = w.Write([]byte(`{"claimed":true,"leaseEpoch":7,"leaseExpiresAt":"2026-06-20T00:00:00Z","attempt":1,"leaseSeconds":60,"heartbeatIntervalSeconds":20}`))
			case "b":
				_, _ = w.Write([]byte(`{"claimed":false,"reason":"deps_not_ready"}`))
			case "c":
				_, _ = w.Write([]byte(`{"claimed":false,"reason":"job_held"}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		case strings.HasSuffix(path, ":heartbeat"):
			var body struct {
				RunnerID   string `json:"runnerId"`
				LeaseEpoch int    `json:"leaseEpoch"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotHeartbeatEpoch = body.LeaseEpoch
			_, _ = w.Write([]byte(`{"leaseExpiresAt":"2026-06-20T00:01:00Z","leaseSeconds":60,"heartbeatIntervalSeconds":20}`))
		case strings.HasSuffix(path, ":complete"):
			var body struct {
				LeaseEpoch int    `json:"leaseEpoch"`
				Outcome    string `json:"outcome"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotCompleteEpoch = body.LeaseEpoch
			gotCompleteOutcome = body.Outcome
			_, _ = w.Write([]byte(`{"seq":3}`))
		case strings.HasSuffix(path, "/frontier"):
			_, _ = w.Write([]byte(`{"jobs":["a"]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	base := srv.URL + "/v1/organizations/o/projects/p/state"
	coord := &CoordClient{HTTP: srv.Client(), BaseURL: base}
	inner := NewRemoteStateBackend(
		remotestate.NewClientWithScope(srv.URL, "test", nil, remotestate.Scope{OrgID: "o", ProjectID: "p"}),
		"r1",
	)
	b := NewCoordBackend(coord, inner, "r1")
	ctx := context.Background()

	// Claim `a` — won, with the server-supplied lease tunables echoed through.
	res, err := b.ClaimJob(ctx, "run-1", model.PlanJob{ID: "a"}, "r1")
	if err != nil {
		t.Fatalf("claim a: %v", err)
	}
	if !res.Claimed || res.Takeover {
		t.Fatalf("claim a: got %+v, want claimed without takeover", res)
	}
	if res.LeaseSeconds != 60 || res.HeartbeatIntervalSeconds != 20 {
		t.Fatalf("claim a: lease tunables not threaded: %+v", res)
	}

	// Heartbeat `a` — the epoch stashed at claim (7) must be sent as the §3 key.
	hr, err := b.Heartbeat(ctx, "run-1", "a", "r1")
	if err != nil {
		t.Fatalf("heartbeat a: %v", err)
	}
	if !hr.OK || hr.LeaseLost {
		t.Fatalf("heartbeat a: got %+v, want OK", hr)
	}
	if gotHeartbeatEpoch != 7 {
		t.Fatalf("heartbeat sent leaseEpoch=%d, want 7 (threaded from claim)", gotHeartbeatEpoch)
	}

	// Complete `a` — success maps to outcome "succeeded" with the same epoch.
	if err := b.UpdateJob(ctx, "run-1", "a", "r1", JobStatusSuccess, ""); err != nil {
		t.Fatalf("update a: %v", err)
	}
	if gotCompleteEpoch != 7 || gotCompleteOutcome != "succeeded" {
		t.Fatalf("complete sent epoch=%d outcome=%q, want 7/succeeded", gotCompleteEpoch, gotCompleteOutcome)
	}

	// Rejection mappings the run loop branches on.
	if res, _ := b.ClaimJob(ctx, "run-1", model.PlanJob{ID: "b"}, "r1"); res.Claimed || !res.DepsWaiting {
		t.Fatalf("claim b: got %+v, want DepsWaiting", res)
	}
	if res, _ := b.ClaimJob(ctx, "run-1", model.PlanJob{ID: "c"}, "r1"); res.Claimed || res.CurrentStatus != "running" {
		t.Fatalf("claim c: got %+v, want CurrentStatus=running", res)
	}

	// Runnable frontier comes from the event-sourced fold.
	jobs, err := b.RunnableJobs(ctx, "run-1")
	if err != nil {
		t.Fatalf("runnable: %v", err)
	}
	if len(jobs) != 1 || jobs[0] != "a" {
		t.Fatalf("runnable: got %v, want [a]", jobs)
	}
}

// A failed terminal status maps to the §3 "failed" outcome.
func TestCoordBackendCompleteFailed(t *testing.T) {
	var gotOutcome, gotErrText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ":complete") {
			var body struct {
				Outcome   string `json:"outcome"`
				ErrorText string `json:"errorText"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotOutcome, gotErrText = body.Outcome, body.ErrorText
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"seq":1}`))
	}))
	defer srv.Close()

	coord := &CoordClient{HTTP: srv.Client(), BaseURL: srv.URL + "/v1/organizations/o/projects/p/state"}
	b := NewCoordBackend(coord, nil, "r1")
	if err := b.UpdateJob(context.Background(), "run-1", "j", "r1", JobStatusFailed, "boom"); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if gotOutcome != "failed" || gotErrText != "boom" {
		t.Fatalf("got outcome=%q errText=%q, want failed/boom", gotOutcome, gotErrText)
	}
}
