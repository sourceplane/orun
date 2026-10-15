package statebackend

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/remotestate"
)

// lostLeaseServer answers :claim for job a with lease epoch 3 (or rejects it
// as run_terminal when rejectClaim is set), :complete with 409, and the state
// read with job a as stateJob ("" → no state read, 404).
func lostLeaseServer(t *testing.T, stateJob string, rejectClaim bool) *CoordBackend {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, ":claim") && rejectClaim:
			_, _ = w.Write([]byte(`{"claimed":false,"reason":"run_terminal"}`))
		case strings.HasSuffix(r.URL.Path, ":claim"):
			_, _ = w.Write([]byte(`{"claimed":true,"leaseEpoch":3,"leaseExpiresAt":"2026-06-20T00:00:00Z","attempt":1,"leaseSeconds":60,"heartbeatIntervalSeconds":20}`))
		case strings.HasSuffix(r.URL.Path, ":complete"):
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"reason":"lease_lost"}`))
		case strings.HasSuffix(r.URL.Path, "/state") && stateJob != "":
			_, _ = w.Write([]byte(`{"runId":"R","phase":"running","jobs":{"a":` + stateJob + `},"frontier":[],"lastSeq":9}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	coord := &CoordClient{HTTP: srv.Client(), BaseURL: srv.URL + "/v1/organizations/o/projects/p/state"}
	inner := NewRemoteStateBackend(
		remotestate.NewClientWithScope(srv.URL, "test", stubToken{}, remotestate.Scope{OrgID: "o", ProjectID: "p"}), "r1")
	return NewCoordBackend(coord, inner, "r1")
}

func wireJob(phase, holder string) string {
	h := "null"
	if holder != "" {
		h = `"` + holder + `"`
	}
	return `{"jobId":"a","phase":"` + phase + `","holder":` + h + `,"leaseEpoch":null,"leaseExpiresAt":null,"attempt":2,"resultDigest":null,"errorText":null}`
}

// A :complete refused as lease_lost is superseded only when the coordinator
// holds a result for the job, or another runner holds it now. Otherwise the
// result is lost, and UpdateJob says so instead of returning nil (orun#748).
func TestCoordBackendUpdateJobLeaseLost(t *testing.T) {
	cases := []struct {
		name     string
		stateJob string
		wantErr  string // "" → superseded (nil)
	}{
		{"another runner recorded a result", wireJob("succeeded", ""), ""},
		{"another runner recorded a failure", wireJob("failed", ""), ""},
		{"another runner holds it now", wireJob("claimed", "r2"), ""},
		{"lease lapsed and the job went back to the queue", wireJob("queued", ""), "holds the job as queued"},
		{"held by this runner under another epoch", wireJob("claimed", "r1"), "holds the job as claimed"},
		{"no state read", "", "holds the job as unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := lostLeaseServer(t, tc.stateJob, false)
			ctx := context.Background()
			if _, err := b.ClaimJob(ctx, "run-1", model.PlanJob{ID: "a"}, "r1"); err != nil {
				t.Fatalf("claim: %v", err)
			}
			err := b.UpdateJob(ctx, "run-1", "a", "r1", JobStatusSuccess, "")
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("UpdateJob = %v, want nil (superseded)", err)
				}
				return
			}
			if !errors.Is(err, ErrResultNotRecorded) {
				t.Fatalf("UpdateJob = %v, want ErrResultNotRecorded", err)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("UpdateJob = %q, want it to say %q", err, tc.wantErr)
			}
		})
	}
}

// A claim refused because the job is terminal is classified from the
// coordinator's own record, not the read model: a read model behind the
// coordinator turned a job that passed into "already failed on another
// runner" (orun#748).
func TestCoordBackendClaimTerminalReadsTheCoordinator(t *testing.T) {
	cases := []struct {
		name       string
		stateJob   string
		wantStatus string
		wantPhase  string
	}{
		{"succeeded", wireJob("succeeded", ""), "success", "succeeded"},
		{"memoized", wireJob("memoized", ""), "success", "memoized"},
		{"timed out", wireJob("timed_out", ""), "failed", "timed_out"},
		{"canceled", wireJob("canceled", ""), "failed", "canceled"},
		{"no result", wireJob("queued", ""), "unrecorded", "queued"},
		// No state read: the read model decides (here it has no row → failed).
		{"no state read", "", "failed", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := lostLeaseServer(t, tc.stateJob, true)
			res, err := b.ClaimJob(context.Background(), "run-1", model.PlanJob{ID: "a"}, "r1")
			if err != nil {
				t.Fatalf("claim: %v", err)
			}
			if res.Claimed || res.CurrentStatus != tc.wantStatus || res.TerminalPhase != tc.wantPhase {
				t.Fatalf("claim = %+v, want CurrentStatus=%q TerminalPhase=%q", res, tc.wantStatus, tc.wantPhase)
			}
		})
	}
}
