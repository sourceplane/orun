package statebackend

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
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

type stubToken struct{}

func (stubToken) Token(context.Context) (string, error) { return "test-token", nil }

// A hermetic job claims with its input-hash key and, on success, pushes a
// job-result and reports the memo key + digest so the server can index it.
func TestCoordBackendMemoizationProducer(t *testing.T) {
	var claimBody, completeBody map[string]any
	var putKind string
	var putResult JobResult

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, ":claim"):
			_ = json.NewDecoder(r.Body).Decode(&claimBody)
			_, _ = w.Write([]byte(`{"claimed":true,"leaseEpoch":1,"leaseExpiresAt":"t","attempt":1,"leaseSeconds":60,"heartbeatIntervalSeconds":20}`))
		case strings.HasSuffix(path, "/objects/missing"):
			var req struct {
				Digests []string `json:"digests"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			_ = json.NewEncoder(w).Encode(map[string][]string{"missing": req.Digests}) // all missing → force the PUT
		case r.Method == http.MethodPut && strings.Contains(path, "/objects/"):
			putKind = r.Header.Get("Orun-Object-Kind")
			_ = json.NewDecoder(r.Body).Decode(&putResult)
			w.WriteHeader(http.StatusCreated)
		case strings.HasSuffix(path, ":complete"):
			_ = json.NewDecoder(r.Body).Decode(&completeBody)
			_, _ = w.Write([]byte(`{"seq":2}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	coord := &CoordClient{HTTP: srv.Client(), BaseURL: srv.URL + "/v1/organizations/o/projects/p/state"}
	inner := NewRemoteStateBackend(
		remotestate.NewClientWithScope(srv.URL, "test", stubToken{}, remotestate.Scope{OrgID: "o", ProjectID: "p"}), "r1")
	b := NewCoordBackend(coord, inner, "r1")
	b.SetSourceDigest(func(job model.PlanJob) (string, error) { return "sha256:src-" + job.Path, nil })
	ctx := context.Background()

	job := model.PlanJob{
		ID:     "h",
		Path:   "apps/web",
		Labels: map[string]string{HermeticLabel: "true"},
		Steps:  []model.PlanStep{{ID: "s1", Run: "echo hi"}},
		Env:    map[string]any{"FOO": "bar"},
	}

	res, err := b.ClaimJob(ctx, "run-1", job, "r1")
	if err != nil || !res.Claimed {
		t.Fatalf("claim hermetic: res=%+v err=%v", res, err)
	}
	if claimBody["hermetic"] != true {
		t.Fatalf("claim did not carry hermetic: %+v", claimBody)
	}
	hash, _ := claimBody["jobInputHash"].(string)
	if want, _ := b.jobInputHashFor(job); hash != want || hash == "" {
		t.Fatalf("claim jobInputHash %q != recomputed %q", hash, want)
	}

	if err := b.UpdateJob(ctx, "run-1", "h", "r1", JobStatusSuccess, ""); err != nil {
		t.Fatalf("update: %v", err)
	}
	if putKind != objectKindJobResult {
		t.Fatalf("job-result pushed with kind %q, want %q", putKind, objectKindJobResult)
	}
	if putResult.JobInputHash != hash {
		t.Fatalf("pushed job-result hash %q != claim hash %q", putResult.JobInputHash, hash)
	}
	if completeBody["jobInputHash"] != hash {
		t.Fatalf("complete jobInputHash %v != claim hash %q", completeBody["jobInputHash"], hash)
	}
	if d, _ := completeBody["resultDigest"].(string); !strings.HasPrefix(d, "sha256:") {
		t.Fatalf("complete resultDigest missing/malformed: %v", completeBody["resultDigest"])
	}
}

// A cached claim (server-resolved memo hit) surfaces Cached + ResultDigest and is
// reported as already-complete, so the run loop adopts it without executing.
func TestCoordBackendCachedClaimAdopts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, ":claim") {
			_, _ = w.Write([]byte(`{"cached":true,"result":{"digest":"sha256:deadbeefcafe0000"}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	coord := &CoordClient{HTTP: srv.Client(), BaseURL: srv.URL + "/v1/organizations/o/projects/p/state"}
	inner := NewRemoteStateBackend(
		remotestate.NewClientWithScope(srv.URL, "test", stubToken{}, remotestate.Scope{OrgID: "o", ProjectID: "p"}), "r1")
	b := NewCoordBackend(coord, inner, "r1")

	job := model.PlanJob{ID: "h", Labels: map[string]string{HermeticLabel: "true"}, Steps: []model.PlanStep{{ID: "s1", Run: "echo hi"}}}
	res, err := b.ClaimJob(context.Background(), "run-1", job, "r1")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if !res.Cached || res.Claimed {
		t.Fatalf("cached claim: got Cached=%v Claimed=%v, want Cached=true Claimed=false", res.Cached, res.Claimed)
	}
	if res.ResultDigest != "sha256:deadbeefcafe0000" {
		t.Fatalf("cached ResultDigest = %q, want sha256:deadbeefcafe0000", res.ResultDigest)
	}
	if res.CurrentStatus != "success" {
		t.Fatalf("cached CurrentStatus = %q, want success (adopt-by-skip)", res.CurrentStatus)
	}
}

// A non-hermetic job sends no memo hints and pushes no result object.
func TestCoordBackendNonHermeticNoMemo(t *testing.T) {
	var claimBody, completeBody map[string]any
	objectCalls := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, ":claim"):
			_ = json.NewDecoder(r.Body).Decode(&claimBody)
			_, _ = w.Write([]byte(`{"claimed":true,"leaseEpoch":1,"leaseExpiresAt":"t","attempt":1,"leaseSeconds":60,"heartbeatIntervalSeconds":20}`))
		case strings.Contains(path, "/objects/"):
			objectCalls++
			_, _ = w.Write([]byte(`{"missing":[]}`))
		case strings.HasSuffix(path, ":complete"):
			_ = json.NewDecoder(r.Body).Decode(&completeBody)
			_, _ = w.Write([]byte(`{"seq":2}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	coord := &CoordClient{HTTP: srv.Client(), BaseURL: srv.URL + "/v1/organizations/o/projects/p/state"}
	inner := NewRemoteStateBackend(
		remotestate.NewClientWithScope(srv.URL, "test", stubToken{}, remotestate.Scope{OrgID: "o", ProjectID: "p"}), "r1")
	b := NewCoordBackend(coord, inner, "r1")
	ctx := context.Background()

	job := model.PlanJob{ID: "n", Steps: []model.PlanStep{{ID: "s1", Run: "echo hi"}}}
	if _, err := b.ClaimJob(ctx, "run-1", job, "r1"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, ok := claimBody["hermetic"]; ok {
		t.Fatalf("non-hermetic claim leaked hermetic: %+v", claimBody)
	}
	if _, ok := claimBody["jobInputHash"]; ok {
		t.Fatalf("non-hermetic claim leaked jobInputHash: %+v", claimBody)
	}
	if err := b.UpdateJob(ctx, "run-1", "n", "r1", JobStatusSuccess, ""); err != nil {
		t.Fatalf("update: %v", err)
	}
	if objectCalls != 0 {
		t.Fatalf("non-hermetic completion pushed an object (%d calls)", objectCalls)
	}
	if _, ok := completeBody["jobInputHash"]; ok {
		t.Fatalf("non-hermetic complete leaked jobInputHash: %+v", completeBody)
	}
}

// LoadRunState folds the native …/log stream (not the OP2 read model): it pulls
// the event log, fetches the plan object for the DAG, and reduces both into
// ExecState/ExecMetadata — recovering per-job timestamps from event stamps.
func TestCoordBackendLoadRunStateFoldsNativeLog(t *testing.T) {
	planDigest := "sha256:" + strings.Repeat("a", 64)
	planJSON := `{"version":"sourceplane.io/v1","jobs":[{"jobId":"a","deps":[]},{"jobId":"b","deps":["a"]}]}`
	eventsJSON := `{"events":[
		{"seq":1,"kind":"state.run.created","runId":"R","actor":{"id":"u1","type":"user"},"at":"2026-01-01T00:00:00Z","payload":{"planDigest":"` + planDigest + `","sourceHash":"sha256:s"}},
		{"seq":2,"kind":"state.job.ready","runId":"R","jobId":"a","actor":{"id":"sys","type":"system"},"at":"2026-01-01T00:00:10Z","payload":{"attempt":1}},
		{"seq":3,"kind":"state.job.claimed","runId":"R","jobId":"a","actor":{"id":"r1","type":"workflow"},"at":"2026-01-01T00:01:00Z","payload":{"runnerId":"r1","leaseEpoch":1,"leaseExpiresAt":"2026-01-01T00:02:00Z","attempt":1}},
		{"seq":4,"kind":"state.job.succeeded","runId":"R","jobId":"a","actor":{"id":"r1","type":"workflow"},"at":"2026-01-01T00:01:30Z","payload":{"runnerId":"r1","leaseEpoch":1,"resultDigest":"sha256:ra"}},
		{"seq":5,"kind":"state.job.ready","runId":"R","jobId":"b","actor":{"id":"sys","type":"system"},"at":"2026-01-01T00:01:31Z","payload":{"attempt":1}}
	]}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/log"):
			_, _ = w.Write([]byte(eventsJSON))
		case strings.Contains(r.URL.Path, "/objects/"):
			_, _ = w.Write([]byte(planJSON))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	coord := &CoordClient{HTTP: srv.Client(), BaseURL: srv.URL + "/v1/organizations/o/projects/p/state"}
	inner := NewRemoteStateBackend(
		remotestate.NewClientWithScope(srv.URL, "test", stubToken{}, remotestate.Scope{OrgID: "o", ProjectID: "p"}), "r1")
	b := NewCoordBackend(coord, inner, "r1")

	st, meta, err := b.LoadRunState(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(st.Jobs) != 2 {
		t.Fatalf("jobs: got %d, want 2 (plan DAG recovered)", len(st.Jobs))
	}
	if st.PlanChecksum != planDigest {
		t.Fatalf("planChecksum: got %q, want %q", st.PlanChecksum, planDigest)
	}
	a := st.Jobs["a"]
	if a == nil || a.Status != "completed" {
		t.Fatalf("job a: %+v, want completed", a)
	}
	if a.StartedAt != "2026-01-01T00:01:00Z" || a.FinishedAt != "2026-01-01T00:01:30Z" {
		t.Fatalf("job a timestamps recovered wrong: started=%q finished=%q", a.StartedAt, a.FinishedAt)
	}
	if b2 := st.Jobs["b"]; b2 == nil || b2.Status != "pending" {
		t.Fatalf("job b: %+v, want pending (in plan, not yet claimed)", b2)
	}
	if meta.JobTotal != 2 || meta.JobDone != 1 || meta.JobFailed != 0 {
		t.Fatalf("counts: total=%d done=%d failed=%d, want 2/1/0", meta.JobTotal, meta.JobDone, meta.JobFailed)
	}
	if meta.Status != "running" {
		t.Fatalf("run status: got %q, want running", meta.Status)
	}
	if meta.User != "u1" || meta.StartedAt != "2026-01-01T00:00:00Z" {
		t.Fatalf("meta header: user=%q startedAt=%q", meta.User, meta.StartedAt)
	}
}

// With no native events (legacy/OP2-only run), LoadRunState falls back to inner.
func TestCoordBackendLoadRunStateFallsBackWhenNoEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/log") {
			_, _ = w.Write([]byte(`{"events":[]}`))
			return
		}
		// inner GetRun → minimal run so the fallback path returns without error.
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	coord := &CoordClient{HTTP: srv.Client(), BaseURL: srv.URL + "/v1/organizations/o/projects/p/state"}
	inner := NewRemoteStateBackend(
		remotestate.NewClientWithScope(srv.URL, "test", stubToken{}, remotestate.Scope{OrgID: "o", ProjectID: "p"}), "r1")
	b := NewCoordBackend(coord, inner, "r1")

	// The inner fallback hits GetRun (404 here) → an error, proving the fallback
	// path was taken rather than the native fold (which would have panicked on a
	// nil plan / empty events).
	_, _, err := b.LoadRunState(context.Background(), "run-1")
	if err == nil {
		t.Fatalf("expected the inner fallback to surface its GetRun error on an empty native log")
	}
}

// WaitForRunEvents long-polls the event stream and returns the advanced head.
func TestCoordBackendWaitForRunEvents(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.RawQuery, "wait=") {
			_, _ = w.Write([]byte(`{"events":[{"seq":3,"kind":"state.job.claimed","runId":"R","jobId":"a"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"events":[]}`))
	}))
	defer srv.Close()
	coord := &CoordClient{HTTP: srv.Client(), BaseURL: srv.URL + "/v1/organizations/o/projects/p/state"}
	b := NewCoordBackend(coord, nil, "r1")

	head, err := b.WaitForRunEvents(context.Background(), "run-1", 2, 15)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if head != 3 {
		t.Fatalf("head: got %d, want 3 (advanced past the appended event)", head)
	}
	if !strings.Contains(gotQuery, "from=2") || !strings.Contains(gotQuery, "wait=15") {
		t.Fatalf("long-poll query missing from/wait: %q", gotQuery)
	}
}

// On a lapsed wait (no new event) the head is returned unchanged.
func TestCoordBackendWaitForRunEventsTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"events":[]}`))
	}))
	defer srv.Close()
	coord := &CoordClient{HTTP: srv.Client(), BaseURL: srv.URL + "/v1/organizations/o/projects/p/state"}
	b := NewCoordBackend(coord, nil, "r1")

	head, err := b.WaitForRunEvents(context.Background(), "run-1", 5, 1)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if head != 5 {
		t.Fatalf("head on timeout: got %d, want 5 (unchanged)", head)
	}
}

// A CI rerun resumes one execution with --retry: the failed job is re-opened
// (state.job.retried), re-claimed and succeeds. The fold must read it as
// completed; it used to stay failed (terminal-sticky), and a lane waiting on it
// under --retry sat until its upstream-wait timeout (cirrus-baseline run
// 38511459165, attempt 2).
func TestCoordBackendLoadRunStateFoldsRetriedJob(t *testing.T) {
	planDigest := "sha256:" + strings.Repeat("b", 64)
	planJSON := `{"version":"sourceplane.io/v1","jobs":[{"jobId":"a","deps":[]},{"jobId":"b","deps":["a"]}]}`
	eventsJSON := `{"events":[
		{"seq":1,"kind":"state.run.created","runId":"R","actor":{"id":"u1","type":"user"},"at":"2026-01-01T00:00:00Z","payload":{"planDigest":"` + planDigest + `","sourceHash":"sha256:s"}},
		{"seq":2,"kind":"state.job.claimed","runId":"R","jobId":"a","actor":{"id":"r1","type":"workflow"},"at":"2026-01-01T00:01:00Z","payload":{"runnerId":"gha-1-1-run","leaseEpoch":1,"leaseExpiresAt":"2026-01-01T00:02:00Z","attempt":1}},
		{"seq":3,"kind":"state.job.failed","runId":"R","jobId":"a","actor":{"id":"r1","type":"workflow"},"at":"2026-01-01T00:01:20Z","payload":{"runnerId":"gha-1-1-run","leaseEpoch":1,"reason":"step_failed","errorText":"text file busy"}},
		{"seq":4,"kind":"state.job.retried","runId":"R","jobId":"a","actor":{"id":"r2","type":"workflow"},"at":"2026-01-01T00:05:00Z","payload":{"runnerId":"gha-1-2-run"}},
		{"seq":5,"kind":"state.job.claimed","runId":"R","jobId":"a","actor":{"id":"r2","type":"workflow"},"at":"2026-01-01T00:05:00Z","payload":{"runnerId":"gha-1-2-run","leaseEpoch":2,"leaseExpiresAt":"2026-01-01T00:06:00Z","attempt":2}},
		{"seq":6,"kind":"state.job.succeeded","runId":"R","jobId":"a","actor":{"id":"r2","type":"workflow"},"at":"2026-01-01T00:06:00Z","payload":{"runnerId":"gha-1-2-run","leaseEpoch":2,"resultDigest":""}}
	]}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/log"):
			_, _ = w.Write([]byte(eventsJSON))
		case strings.Contains(r.URL.Path, "/objects/"):
			_, _ = w.Write([]byte(planJSON))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	coord := &CoordClient{HTTP: srv.Client(), BaseURL: srv.URL + "/v1/organizations/o/projects/p/state"}
	inner := NewRemoteStateBackend(
		remotestate.NewClientWithScope(srv.URL, "test", stubToken{}, remotestate.Scope{OrgID: "o", ProjectID: "p"}), "r2")
	b := NewCoordBackend(coord, inner, "r2")

	st, meta, err := b.LoadRunState(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if a := st.Jobs["a"]; a == nil || a.Status != "completed" {
		t.Fatalf("job a: %+v, want completed after retry", a)
	}
	if meta.JobDone != 1 || meta.JobFailed != 0 {
		t.Fatalf("counts: done=%d failed=%d, want 1/0", meta.JobDone, meta.JobFailed)
	}
}

// viewServer is a stand-in for the native log and object routes that counts
// what it serves: the run's events from `from`, a claim of job b appended
// while a long-poll holds, and the plan object.
type viewServer struct {
	planDigest string
	mu         sync.Mutex
	reads      []string // "state" / "log from=N wait=W" / "object" / "frontier"
	appended   bool
	noState    bool // an older server: no state read (404)
	renewals   bool // a progress-woken wait lapses with one renewal on the page
}

func newViewServer(t *testing.T) (*viewServer, *httptest.Server) {
	t.Helper()
	vs := &viewServer{planDigest: "sha256:" + strings.Repeat("b", 64)}
	events := []string{
		`{"seq":1,"kind":"state.run.created","runId":"R","actor":{"id":"u1","type":"user"},"at":"2026-01-01T00:00:00Z","payload":{"planDigest":"` + vs.planDigest + `","sourceHash":"sha256:s"}}`,
		`{"seq":2,"kind":"state.job.ready","runId":"R","jobId":"a","actor":{"id":"sys","type":"system"},"at":"2026-01-01T00:00:10Z","payload":{"attempt":1}}`,
		`{"seq":3,"kind":"state.job.claimed","runId":"R","jobId":"a","actor":{"id":"r1","type":"workflow"},"at":"2026-01-01T00:01:00Z","payload":{"runnerId":"r1","leaseEpoch":1,"leaseExpiresAt":"2026-01-01T00:02:00Z","attempt":1}}`,
		`{"seq":4,"kind":"state.job.succeeded","runId":"R","jobId":"a","actor":{"id":"r1","type":"workflow"},"at":"2026-01-01T00:01:30Z","payload":{"runnerId":"r1","leaseEpoch":1,"resultDigest":"sha256:ra"}}`,
		`{"seq":5,"kind":"state.job.ready","runId":"R","jobId":"b","actor":{"id":"sys","type":"system"},"at":"2026-01-01T00:01:31Z","payload":{"attempt":1}}`,
	}
	claimB := `{"seq":6,"kind":"state.job.claimed","runId":"R","jobId":"b","actor":{"id":"r2","type":"workflow"},"at":"2026-01-01T00:02:00Z","payload":{"runnerId":"r2","leaseEpoch":1,"leaseExpiresAt":"2026-01-01T00:03:00Z","attempt":1}}`
	// The server's fold of the five events (the wire RunFoldState, nulls and
	// all), and of the six once b's claim was appended.
	jobA := `"a":{"jobId":"a","phase":"succeeded","holder":null,"leaseEpoch":null,"leaseExpiresAt":null,"attempt":1,"resultDigest":"sha256:ra","errorText":null}`
	timesA := `"a":{"startedAt":"2026-01-01T00:01:00Z","finishedAt":"2026-01-01T00:01:30Z"}`
	state5 := `{"runId":"R","planDigest":"` + vs.planDigest + `","sourceHash":"sha256:s","phase":"running","jobs":{` + jobA + `,"b":{"jobId":"b","phase":"queued","holder":null,"leaseEpoch":null,"leaseExpiresAt":null,"attempt":1,"resultDigest":null,"errorText":null}},"frontier":["b"],"lastSeq":5,"timings":{"startedAt":"2026-01-01T00:01:00Z","finishedAt":null,"jobs":{` + timesA + `,"b":{"startedAt":null,"finishedAt":null}}}}`
	state6 := `{"runId":"R","planDigest":"` + vs.planDigest + `","sourceHash":"sha256:s","phase":"running","jobs":{` + jobA + `,"b":{"jobId":"b","phase":"claimed","holder":"r2","leaseEpoch":1,"leaseExpiresAt":"2026-01-01T00:03:00Z","attempt":1,"resultDigest":null,"errorText":null}},"frontier":[],"lastSeq":6,"timings":{"startedAt":"2026-01-01T00:01:00Z","finishedAt":null,"jobs":{` + timesA + `,"b":{"startedAt":"2026-01-01T00:02:00Z","finishedAt":null}}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		q := r.URL.Query()
		vs.mu.Lock()
		defer vs.mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/state"):
			vs.reads = append(vs.reads, "state")
			if vs.noState {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if vs.appended {
				_, _ = w.Write([]byte(state6))
			} else {
				_, _ = w.Write([]byte(state5))
			}
		case strings.HasSuffix(r.URL.Path, "/log") && q.Get("wake") == "progress" && vs.renewals:
			vs.reads = append(vs.reads, "log from="+q.Get("from")+" wait="+q.Get("wait")+" wake=progress")
			_, _ = w.Write([]byte(`{"events":[{"seq":7,"kind":"state.job.lease_renewed","runId":"R","jobId":"b","actor":{"id":"r2","type":"workflow"},"at":"2026-01-01T00:02:20Z","payload":{"runnerId":"r2","leaseEpoch":1,"leaseExpiresAt":"2026-01-01T00:03:20Z"}}],"head":{"seq":7,"progressSeq":6}}`))
		case strings.HasSuffix(r.URL.Path, "/log"):
			from, _ := strconv.Atoi(q.Get("from"))
			vs.reads = append(vs.reads, "log from="+q.Get("from")+" wait="+q.Get("wait"))
			all := events
			if q.Get("wait") != "" || vs.appended {
				vs.appended = true
				all = append(all, claimB)
			}
			var page []string
			for i, e := range all {
				if i+1 > from {
					page = append(page, e)
				}
			}
			_, _ = w.Write([]byte(`{"events":[` + strings.Join(page, ",") + `],"head":{"seq":` + strconv.Itoa(len(all)) + `}}`))
		case strings.Contains(r.URL.Path, "/objects/missing"):
			vs.reads = append(vs.reads, "missing")
			_, _ = w.Write([]byte(`{"data":{"missing":[]}}`))
		case strings.Contains(r.URL.Path, "/objects/"):
			vs.reads = append(vs.reads, "object")
			_, _ = w.Write([]byte(`{"version":"sourceplane.io/v1","jobs":[{"jobId":"a","deps":[]},{"jobId":"b","deps":["a"]}]}`))
		case strings.HasSuffix(r.URL.Path, "/frontier"):
			vs.reads = append(vs.reads, "frontier")
			_, _ = w.Write([]byte(`{"jobs":["server-says"]}`))
		case strings.HasSuffix(r.URL.Path, "/runs") && r.Method == http.MethodPost:
			vs.reads = append(vs.reads, "create")
			_, _ = w.Write([]byte(`{"data":{"run":{"runId":"R","status":"running","planDigest":"` + vs.planDigest + `","createdAt":"2026-01-01T00:00:00Z","jobCounts":{"queued":2}}}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return vs, srv
}

func viewBackend(srv *httptest.Server) *CoordBackend {
	coord := &CoordClient{HTTP: srv.Client(), BaseURL: srv.URL + "/v1/organizations/o/projects/p/state"}
	inner := NewRemoteStateBackend(
		remotestate.NewClientWithScope(srv.URL, "test", stubToken{}, remotestate.Scope{OrgID: "o", ProjectID: "p"}), "r1")
	return NewCoordBackend(coord, inner, "r1")
}

func (vs *viewServer) served() []string {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	return append([]string(nil), vs.reads...)
}

// A wake costs the long-poll alone: the events it returns are folded onto
// the view, and the state and frontier reads a waiter makes right after it
// ask the server nothing (specs/orun-sync-efficiency P1).
func TestCoordBackendRunView_WakeCostsOneRequest(t *testing.T) {
	vs, srv := newViewServer(t)
	b := viewBackend(srv)
	ctx := context.Background()

	st, _, err := b.LoadRunState(ctx, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if st.Jobs["b"].Status != "pending" {
		t.Fatalf("b before the wake: %+v", st.Jobs["b"])
	}
	if got := vs.served(); !reflect.DeepEqual(got, []string{"state", "object"}) {
		t.Fatalf("the first read is the server's fold and the plan, once: %v", got)
	}

	head, err := b.WaitForRunEvents(ctx, "run-1", 5, 15)
	if err != nil || head != 6 {
		t.Fatalf("wait: head=%d err=%v", head, err)
	}
	st, _, err = b.LoadRunState(ctx, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if st.Jobs["b"].Status != "running" || st.Jobs["b"].StartedAt != "2026-01-01T00:02:00Z" {
		t.Fatalf("the long-poll's event was not folded onto the view: %+v", st.Jobs["b"])
	}
	frontier, err := b.RunnableJobs(ctx, "run-1")
	if err != nil || len(frontier) != 0 {
		t.Fatalf("frontier from the view (b is claimed): %v %v", frontier, err)
	}
	if got := vs.served(); !reflect.DeepEqual(got, []string{"state", "object", "log from=5 wait=15"}) {
		t.Fatalf("after the wake, the state and frontier reads cost nothing: %v", got)
	}

	// The head read of a new waiter continues from the view, not from 0.
	if _, err := b.WaitForRunEvents(ctx, "run-1", 0, 0); err != nil {
		t.Fatal(err)
	}
	if got := vs.served(); got[len(got)-1] != "log from=6 wait=" {
		t.Fatalf("the head read re-read the log from 0: %v", got)
	}
}

// A view that has not been synced for a while reads only the events past
// its last seq, never the whole log or the plan again.
func TestCoordBackendRunView_StaleViewReadsTheDelta(t *testing.T) {
	old := viewFreshFor
	viewFreshFor = 0
	t.Cleanup(func() { viewFreshFor = old })
	vs, srv := newViewServer(t)
	b := viewBackend(srv)
	ctx := context.Background()
	if _, _, err := b.LoadRunState(ctx, "run-1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.LoadRunState(ctx, "run-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.RunnableJobs(ctx, "run-1"); err != nil {
		t.Fatal(err)
	}
	if got := vs.served(); !reflect.DeepEqual(got, []string{"state", "object", "log from=5 wait=", "log from=5 wait="}) {
		t.Fatalf("a stale view reads the delta only: %v", got)
	}
}

// The first read of a run is the server's fold in one request, with the
// timings the presenter shows, instead of the whole event log
// (specs/orun-sync-efficiency P1, SE2).
func TestCoordBackendRunView_FirstReadIsTheStateRead(t *testing.T) {
	vs, srv := newViewServer(t)
	b := viewBackend(srv)
	ctx := context.Background()
	st, meta, err := b.LoadRunState(ctx, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if got := vs.served(); !reflect.DeepEqual(got, []string{"state", "object"}) {
		t.Fatalf("served: %v", got)
	}
	a := st.Jobs["a"]
	if a.Status != "completed" || a.StartedAt != "2026-01-01T00:01:00Z" || a.FinishedAt != "2026-01-01T00:01:30Z" {
		t.Fatalf("a from the state read: %+v", a)
	}
	if st.Jobs["b"].Status != "pending" || st.Jobs["b"].StartedAt != "" {
		t.Fatalf("b from the state read: %+v", st.Jobs["b"])
	}
	if meta.StartedAt != "2026-01-01T00:01:00Z" || meta.FinishedAt != "" || meta.JobTotal != 2 || meta.JobDone != 1 || meta.Status != "running" {
		t.Fatalf("meta from the state read: %+v", meta)
	}
	frontier, err := b.RunnableJobs(ctx, "run-1")
	if err != nil || !reflect.DeepEqual(frontier, []string{"b"}) {
		t.Fatalf("frontier from the view: %v %v", frontier, err)
	}
	if got := vs.served(); len(got) != 2 {
		t.Fatalf("the frontier read right after cost nothing: %v", got)
	}
}

// A server without the state read (an older orun-cloud, or a run its shard
// does not back) answers 404: the view is primed from the log as before,
// and the state read is not asked again for the run.
func TestCoordBackendRunView_NoStateReadFallsBackToTheLog(t *testing.T) {
	old := viewFreshFor
	viewFreshFor = 0
	t.Cleanup(func() { viewFreshFor = old })
	vs, srv := newViewServer(t)
	vs.noState = true
	b := viewBackend(srv)
	ctx := context.Background()
	st, _, err := b.LoadRunState(ctx, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if st.Jobs["a"].Status != "completed" || st.Jobs["a"].StartedAt != "2026-01-01T00:01:00Z" {
		t.Fatalf("a from the log: %+v", st.Jobs["a"])
	}
	if _, _, err := b.LoadRunState(ctx, "run-1"); err != nil {
		t.Fatal(err)
	}
	if got := vs.served(); !reflect.DeepEqual(got, []string{"state", "log from=0 wait=", "object", "log from=5 wait="}) {
		t.Fatalf("the log primes the view once the state read answered 404, which is not asked again: %v", got)
	}
}

// The process that created or joined the run with its own plan never
// fetches the plan object: InitRun seeds the view's dependency graph.
func TestCoordBackendRunView_SeededPlanSkipsThePlanObject(t *testing.T) {
	vs, srv := newViewServer(t)
	b := viewBackend(srv)
	ctx := context.Background()
	plan := &model.Plan{Jobs: []model.PlanJob{{ID: "a"}, {ID: "b", DependsOn: []string{"a"}}}}
	if _, err := b.InitRun(ctx, plan, InitRunOptions{RunID: "run-1"}); err != nil {
		t.Fatal(err)
	}
	st, _, err := b.LoadRunState(ctx, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Jobs) != 2 || st.Jobs["b"].Status != "pending" {
		t.Fatalf("state from the seeded plan: %+v", st.Jobs)
	}
	for _, r := range vs.served() {
		if r == "object" {
			t.Fatalf("the plan object was fetched although this process holds the plan: %v", vs.served())
		}
	}
	if got := vs.served(); got[len(got)-1] != "state" {
		t.Fatalf("the first read is the one state read: %v", got)
	}
}

// A progress wait asks the server to hold through lease renewals; a lapse
// that carries only a renewal is reported as no progress, with the cursor
// moved past it and the renewal folded onto the view
// (specs/orun-sync-efficiency F3, SE3).
func TestCoordBackendRunView_ProgressWaitSleepsThroughRenewals(t *testing.T) {
	vs, srv := newViewServer(t)
	b := viewBackend(srv)
	ctx := context.Background()
	if _, _, err := b.LoadRunState(ctx, "run-1"); err != nil {
		t.Fatal(err)
	}
	// b is claimed by the wake at seq 6 (progress); then its runner renews.
	if head, progressed, err := b.WaitForRunProgress(ctx, "run-1", 5, 15); err != nil || head != 6 || !progressed {
		t.Fatalf("the claim is progress: head=%d progressed=%v err=%v", head, progressed, err)
	}
	vs.renewals = true
	head, progressed, err := b.WaitForRunProgress(ctx, "run-1", 6, 15)
	if err != nil {
		t.Fatal(err)
	}
	if head != 7 || progressed {
		t.Fatalf("a renewal-only lapse: head=%d progressed=%v; want the cursor past the renewal and no progress", head, progressed)
	}
	if got := vs.served(); got[len(got)-1] != "log from=6 wait=15 wake=progress" {
		t.Fatalf("the wait must ask for a progress wake: %v", got)
	}
	st, _, err := b.LoadRunState(ctx, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if st.Jobs["b"].HeartbeatAt != "2026-01-01T00:03:20Z" {
		t.Fatalf("the renewal on the page was not folded onto the view: %+v", st.Jobs["b"])
	}
	if got := vs.served(); len(got) != 4 {
		t.Fatalf("the state read after the wait cost nothing: %v", got)
	}
	// A new waiter continues from the view's seq, past the renewal.
	vs.renewals = false
	if _, _, err := b.WaitForRunProgress(ctx, "run-1", 0, 0); err != nil {
		t.Fatal(err)
	}
	if got := vs.served(); got[len(got)-1] != "log from=7 wait=" {
		t.Fatalf("the head read must continue from the view: %v", got)
	}
}

// The memo key moves with the job's source tree and the plan's composition
// lock, and a hermetic job whose source cannot be digested is claimed without
// a key: it runs instead of adopting a result keyed on too little
// (specs/orun-cache-efficiency F1).
func TestCoordBackendMemoKeyCoversSourceAndLock(t *testing.T) {
	coord := &CoordClient{HTTP: http.DefaultClient, BaseURL: "http://unused.invalid"}
	b := NewCoordBackend(coord, nil, "r1")
	job := model.PlanJob{ID: "h", Path: "apps/web", Labels: map[string]string{HermeticLabel: "true"}, Steps: []model.PlanStep{{ID: "s1", Run: "build"}}}

	if _, err := b.jobInputHashFor(job); err == nil {
		t.Fatal("no source digest function: no key")
	}
	digest := "sha256:aaaa"
	b.SetSourceDigest(func(model.PlanJob) (string, error) { return digest, nil })
	first, err := b.jobInputHashFor(job)
	if err != nil {
		t.Fatal(err)
	}
	digest = "sha256:bbbb"
	second, _ := b.jobInputHashFor(job)
	if first == second {
		t.Fatal("a changed source tree is a different key")
	}
	b.lockDigest = CompositionLockDigest(&model.Plan{Spec: model.PlanSpec{CompositionSources: []model.ResolvedCompositionSource{{Name: "core", Kind: "oci", Ref: "oci://r/core:v1", ResolvedDigest: "sha256:c1"}}}})
	third, _ := b.jobInputHashFor(job)
	if third == second || b.lockDigest == "" {
		t.Fatal("a changed composition lock is a different key")
	}
	if CompositionLockDigest(&model.Plan{}) != "" {
		t.Fatal("a plan without composition sources has no lock digest")
	}
	a := CompositionLockDigest(&model.Plan{Spec: model.PlanSpec{CompositionSources: []model.ResolvedCompositionSource{{Name: "b", ResolvedDigest: "2"}, {Name: "a", ResolvedDigest: "1"}}}})
	z := CompositionLockDigest(&model.Plan{Spec: model.PlanSpec{CompositionSources: []model.ResolvedCompositionSource{{Name: "a", ResolvedDigest: "1"}, {Name: "b", ResolvedDigest: "2"}}}})
	if a != z {
		t.Fatal("the lock digest does not depend on source order")
	}

	// An undigestable source: the claim carries no memo key and a note is given.
	var claimBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewDecoder(r.Body).Decode(&claimBody)
		_, _ = w.Write([]byte(`{"claimed":true,"leaseEpoch":1,"leaseExpiresAt":"t","attempt":1,"leaseSeconds":60,"heartbeatIntervalSeconds":20}`))
	}))
	defer srv.Close()
	b = NewCoordBackend(&CoordClient{HTTP: srv.Client(), BaseURL: srv.URL + "/v1/organizations/o/projects/p/state"}, nil, "r1")
	b.SetSourceDigest(func(model.PlanJob) (string, error) { return "", errors.New("no checkout") })
	var note string
	b.SetMemoWarn(func(s string) { note = s })
	res, err := b.ClaimJob(context.Background(), "run-1", job, "r1")
	if err != nil || !res.Claimed {
		t.Fatalf("claim: %+v %v", res, err)
	}
	if _, ok := claimBody["jobInputHash"]; ok || claimBody["hermetic"] == true {
		t.Fatalf("an unkeyable hermetic job is claimed without a memo key: %+v", claimBody)
	}
	if !strings.Contains(note, "not memoized") || !strings.Contains(note, "no checkout") {
		t.Fatalf("the note says why: %q", note)
	}
	if b.hash("h") != "" {
		t.Fatal("no key is remembered for the completion")
	}
}
