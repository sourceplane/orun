package remotestate_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/remotestate"
)

func newTestClient(srv *httptest.Server) *remotestate.Client {
	return remotestate.NewClient(srv.URL, "test", remotestate.NewStaticTokenSource("test-token"))
}

// data wraps a payload in the platform success envelope ({data, meta}); the
// client unwraps `.data` on every 2xx body.
func data(payload interface{}) map[string]interface{} {
	return map[string]interface{}{"data": payload, "meta": map[string]interface{}{"requestId": "req_test"}}
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func runEnvelope(runID string) map[string]interface{} {
	return data(map[string]interface{}{"run": map[string]interface{}{
		"runId": runID, "status": "pending", "planDigest": "sha256:abc",
		"source": "cli", "createdAt": "2024-01-01T00:00:00Z",
		"git":       map[string]interface{}{"commit": "", "ref": "", "dirty": false},
		"createdBy": map[string]interface{}{"id": "usr_1", "kind": "user"},
		"jobCounts": map[string]interface{}{"queued": 0, "running": 0, "succeeded": 0, "failed": 0},
	}})
}

func TestClient_AuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		writeJSON(w, 200, runEnvelope("run-123"))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	if _, err := c.GetRun(context.Background(), "run-123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("expected Bearer test-token, got %q", gotAuth)
	}
}

func TestClient_ContractVersionHeader(t *testing.T) {
	var gotVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotVersion = r.Header.Get("Orun-Contract-Version")
		writeJSON(w, 200, runEnvelope("r"))
	}))
	defer srv.Close()

	newTestClient(srv).GetRun(context.Background(), "r")
	if gotVersion != "1" {
		t.Errorf("expected contract version 1, got %q", gotVersion)
	}
}

func TestClient_ErrorEnvelopeParsed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 404, map[string]interface{}{
			"error": map[string]interface{}{"code": "not_found", "message": "run not found", "requestId": "req_x"},
		})
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetRun(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not_found") || !strings.Contains(err.Error(), "req_x") {
		t.Errorf("expected code + requestId in error, got: %v", err)
	}
}

func TestClient_AuthErrorReturnsHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 401, map[string]interface{}{
			"error": map[string]interface{}{"code": "unauthorized", "message": "token invalid"},
		})
	}))
	defer srv.Close()

	err := newTestClient(srv).UpdateJob(context.Background(), "run", "job", "runner", "succeeded", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("expected authentication hint in error, got: %v", err)
	}
}

func TestClient_CreateRun(t *testing.T) {
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		writeJSON(w, 201, runEnvelope("01JABCDEF0123456789ABCDEFG"))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	run, err := c.CreateRun(context.Background(), remotestate.CreateRunRequest{
		RunID:      "01JABCDEF0123456789ABCDEFG",
		PlanDigest: "sha256:abc",
		Source:     "cli",
		Jobs:       []remotestate.PlanJobInput{{JobID: "job-1", Deps: []string{}}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.RunID != "01JABCDEF0123456789ABCDEFG" {
		t.Errorf("expected run id, got %q", run.RunID)
	}
	if gotBody["planDigest"] != "sha256:abc" || gotBody["source"] != "cli" {
		t.Errorf("expected planDigest+source in body, got %v", gotBody)
	}
	if gotBody["runId"] != "01JABCDEF0123456789ABCDEFG" {
		t.Errorf("expected runId in body, got %v", gotBody["runId"])
	}
}

func TestClient_ClaimJob_Claimed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, data(map[string]interface{}{"claim": map[string]interface{}{
			"claimed": true, "leaseExpiresAt": "2024-01-01T00:01:00Z", "attempt": 1,
			"leaseSeconds": 60, "heartbeatIntervalSeconds": 20,
		}}))
	}))
	defer srv.Close()

	claim, err := newTestClient(srv).ClaimJob(context.Background(), "run-1", "job-1", "runner-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !claim.Claimed || claim.LeaseSeconds != 60 || claim.HeartbeatIntervalSeconds != 20 {
		t.Errorf("expected claimed with lease tunables, got %+v", claim)
	}
}

func TestClient_ClaimJob_Refused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, data(map[string]interface{}{"claim": map[string]interface{}{
			"claimed": false, "reason": "deps_not_ready",
		}}))
	}))
	defer srv.Close()

	claim, err := newTestClient(srv).ClaimJob(context.Background(), "run-1", "job-1", "runner-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if claim.Claimed || claim.Reason != "deps_not_ready" {
		t.Errorf("expected refusal deps_not_ready, got %+v", claim)
	}
}

func TestClient_Heartbeat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, data(map[string]interface{}{
			"leaseExpiresAt": "2024-01-01T00:01:00Z", "leaseSeconds": 60, "heartbeatIntervalSeconds": 20,
		}))
	}))
	defer srv.Close()

	info, err := newTestClient(srv).Heartbeat(context.Background(), "run-1", "job-1", "runner-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.LeaseExpiresAt == "" || info.LeaseSeconds != 60 {
		t.Errorf("expected lease info, got %+v", info)
	}
}

func TestClient_Heartbeat_LeaseLost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 409, map[string]interface{}{
			"error": map[string]interface{}{"code": "lease_lost", "message": "lease lapsed"},
		})
	}))
	defer srv.Close()

	_, err := newTestClient(srv).Heartbeat(context.Background(), "run-1", "job-1", "runner-1")
	if err == nil {
		t.Fatal("expected lease_lost error")
	}
	var apiErr *remotestate.APIError
	if !asAPIError(err, &apiErr) || !apiErr.IsLeaseLost() {
		t.Errorf("expected IsLeaseLost APIError, got: %v", err)
	}
}

func TestClient_AppendLog(t *testing.T) {
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		writeJSON(w, 200, data(map[string]interface{}{"seq": 7}))
	}))
	defer srv.Close()

	seq, err := newTestClient(srv).AppendLog(context.Background(), "run-1", "job-1", "runner-1", "some log output")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seq != 7 {
		t.Errorf("expected seq 7, got %d", seq)
	}
	if gotBody["runnerId"] != "runner-1" || gotBody["content"] != "some log output" {
		t.Errorf("expected runnerId+content in body, got %v", gotBody)
	}
}

func TestClient_ReadLog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("fromSeq"); got != "3" {
			t.Errorf("expected fromSeq=3, got %q", got)
		}
		writeJSON(w, 200, data(map[string]interface{}{
			"content": "tail output", "nextSeq": 9, "complete": true,
		}))
	}))
	defer srv.Close()

	res, err := newTestClient(srv).ReadLog(context.Background(), "run-1", "job-1", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Content != "tail output" || res.NextSeq != 9 || !res.Complete {
		t.Errorf("unexpected read result: %+v", res)
	}
}

func TestClient_ListRunnable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, data(map[string]interface{}{"jobs": []map[string]interface{}{
			{"jobId": "job-a", "deps": []string{}},
			{"jobId": "job-b", "deps": []string{}},
		}}))
	}))
	defer srv.Close()

	jobs, err := newTestClient(srv).ListRunnable(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(jobs) != 2 || jobs[0].JobID != "job-a" || jobs[1].JobID != "job-b" {
		t.Errorf("expected [job-a job-b], got %v", jobs)
	}
}

func TestClient_ObjectsMissingAndPut(t *testing.T) {
	var putDigest, putKind string
	var putBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/objects/missing"):
			writeJSON(w, 200, data(map[string]interface{}{"missing": []string{"sha256:gap"}}))
		case r.Method == http.MethodPut:
			putKind = r.Header.Get("Orun-Object-Kind")
			putBody, _ = io.ReadAll(r.Body)
			putDigest = strings.TrimPrefix(r.URL.Path[strings.LastIndex(r.URL.Path, "/objects/")+len("/objects/"):], "")
			writeJSON(w, 201, data(map[string]interface{}{"created": true}))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv)
	missing, err := c.ObjectsMissing(context.Background(), []string{"sha256:gap", "sha256:have"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(missing) != 1 || missing[0] != "sha256:gap" {
		t.Errorf("expected [sha256:gap], got %v", missing)
	}

	blob := []byte("plan-bytes")
	digest := remotestate.Digest(blob)
	if err := c.PutObject(context.Background(), digest, remotestate.ObjectKindPlan, blob); err != nil {
		t.Fatalf("unexpected put error: %v", err)
	}
	if putKind != remotestate.ObjectKindPlan {
		t.Errorf("expected kind plan, got %q", putKind)
	}
	if string(putBody) != "plan-bytes" {
		t.Errorf("expected blob body, got %q", putBody)
	}
	if !strings.Contains(putDigest, "sha256") {
		t.Errorf("expected digest in path, got %q", putDigest)
	}
}

func TestClient_PutObject_RawDigestColonInPath(t *testing.T) {
	// Regression: the digest's "sha256:" colon is path-legal and the server
	// matches it raw; percent-encoding it ("%3A") makes the state-worker router
	// 404 "Route not found". The object path must carry the raw colon.
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		writeJSON(w, 201, data(map[string]interface{}{"created": true}))
	}))
	defer srv.Close()

	digest := remotestate.Digest([]byte("x"))
	if err := newTestClient(srv).PutObject(context.Background(), digest, remotestate.ObjectKindPlan, []byte("x")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(gotPath, "%3A") {
		t.Errorf("digest colon was percent-encoded in path: %q", gotPath)
	}
	if !strings.HasSuffix(gotPath, "/objects/"+digest) {
		t.Errorf("path %q does not end with raw /objects/%s", gotPath, digest)
	}
}

func TestClient_PutObject_RetriesOnRateLimit(t *testing.T) {
	// A rate-limited (429) blob PUT must be retried — honoring Retry-After —
	// rather than aborting, so a large catalog push rides through the
	// per-identity limiter instead of failing the whole sync on the first 429.
	var puts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("unexpected method %s", r.Method)
			w.WriteHeader(500)
			return
		}
		puts++
		if puts == 1 {
			w.Header().Set("Retry-After", "1")
			writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
				"error": map[string]interface{}{
					"code":    "rate_limited",
					"message": "Rate limit exceeded for identity scope. Retry after 1 seconds.",
					"details": map[string]interface{}{"scope": "identity", "retryAfterSeconds": 1},
				},
			})
			return
		}
		writeJSON(w, 201, data(map[string]interface{}{"created": true}))
	}))
	defer srv.Close()

	blob := []byte("blob-bytes")
	digest := remotestate.Digest(blob)
	if err := newTestClient(srv).PutObject(context.Background(), digest, remotestate.ObjectKindPlan, blob); err != nil {
		t.Fatalf("PutObject should succeed after a retried 429, got: %v", err)
	}
	if puts != 2 {
		t.Errorf("expected 2 PUT attempts (429 then success), got %d", puts)
	}
}

func TestClient_PutObject_RateLimitCanceledByContext(t *testing.T) {
	// Retries must observe context cancellation: a 429 with a long Retry-After
	// must not pin the goroutine past a canceled/expired context.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3600")
		writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
			"error": map[string]interface{}{"code": "rate_limited", "message": "rate limited"},
		})
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled → first retry wait returns immediately
	err := newTestClient(srv).PutObject(ctx, remotestate.Digest([]byte("x")), remotestate.ObjectKindPlan, []byte("x"))
	if err == nil {
		t.Fatal("expected an error when the context is canceled during retry backoff")
	}
}

func TestClient_EnsureObject_SkipsWhenPresent(t *testing.T) {
	putCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			putCalled = true
		}
		// Report nothing missing → no PUT should follow.
		writeJSON(w, 200, data(map[string]interface{}{"missing": []string{}}))
	}))
	defer srv.Close()

	if _, err := newTestClient(srv).EnsureObject(context.Background(), remotestate.ObjectKindPlan, []byte("x")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if putCalled {
		t.Error("expected no PUT when the object is already present")
	}
}

// asAPIError is a thin wrapper over errors.As to keep the tests terse.
func asAPIError(err error, target **remotestate.APIError) bool {
	for err != nil {
		if e, ok := err.(*remotestate.APIError); ok {
			*target = e
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
