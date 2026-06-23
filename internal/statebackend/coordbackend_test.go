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
	ctx := context.Background()

	job := model.PlanJob{
		ID:     "h",
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
	if hash != jobInputHashFor(job) || hash == "" {
		t.Fatalf("claim jobInputHash %q != recomputed %q", hash, jobInputHashFor(job))
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
