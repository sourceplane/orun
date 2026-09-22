package statebackend

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// tokenFunc adapts a function to the TokenSource interface.
type tokenFunc func(ctx context.Context) (string, error)

func (f tokenFunc) Token(ctx context.Context) (string, error) { return f(ctx) }

func TestCoordClientClaim404IsRetryableSentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Run not yet visible to coordination (sibling matrix jobs initializing).
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := &CoordClient{BaseURL: srv.URL}
	_, err := c.Claim(context.Background(), "run_1", "job_a", ClaimRequest{RunnerID: "r1"})
	if !errors.Is(err, ErrClaimTargetNotFound) {
		t.Fatalf("claim 404 should map to ErrClaimTargetNotFound (retryable), got %v", err)
	}
}

func TestCoordClientClaimOtherStatusIsHardError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := &CoordClient{BaseURL: srv.URL}
	_, err := c.Claim(context.Background(), "run_1", "job_a", ClaimRequest{RunnerID: "r1"})
	if err == nil || errors.Is(err, ErrClaimTargetNotFound) {
		t.Fatalf("non-404 should be a hard error, got %v", err)
	}
}

func TestCoordClientTokenSourceAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"claimed":true,"leaseEpoch":1,"leaseExpiresAt":"t"}`))
	}))
	defer srv.Close()

	// TokenSource takes precedence over the static Token (CI OIDC path).
	c := &CoordClient{
		BaseURL:     srv.URL,
		Token:       "static",
		TokenSource: tokenFunc(func(context.Context) (string, error) { return "oidc-exchanged", nil }),
	}
	if _, err := c.Claim(context.Background(), "r", "j", ClaimRequest{RunnerID: "runner"}); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if gotAuth != "Bearer oidc-exchanged" {
		t.Fatalf("Authorization = %q, want the resolved OIDC token", gotAuth)
	}

	// A token-resolution failure aborts the request (no unauthenticated call).
	bad := &CoordClient{
		BaseURL:     srv.URL,
		TokenSource: tokenFunc(func(context.Context) (string, error) { return "", errors.New("exchange failed") }),
	}
	if _, err := bad.Claim(context.Background(), "r", "j", ClaimRequest{RunnerID: "runner"}); err == nil {
		t.Fatal("expected a token-resolution error to propagate")
	}
}

// jobFromPath extracts the jobId from /runs/{run}/jobs/{job}:{verb}.
func jobFromPath(path string) (job, verb string) {
	i := strings.Index(path, "/jobs/")
	if i < 0 {
		return "", ""
	}
	rest := path[i+len("/jobs/"):]
	c := strings.LastIndex(rest, ":")
	if c < 0 {
		return rest, ""
	}
	return rest[:c], rest[c+1:]
}

func coordTestServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Orun-Contract-Version") != "2" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		job, verb := jobFromPath(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch verb {
		case "claim":
			switch job {
			case "jclaimed":
				_, _ = w.Write([]byte(`{"claimed":true,"leaseEpoch":3,"leaseExpiresAt":"2026-06-19T00:01:00Z"}`))
			case "jcached":
				_, _ = w.Write([]byte(`{"claimed":false,"cached":true,"result":{"digest":"sha256:c"}}`))
			case "jdeps":
				_, _ = w.Write([]byte(`{"claimed":false,"reason":"deps_not_ready"}`))
			case "jheld":
				_, _ = w.Write([]byte(`{"claimed":false,"reason":"job_held"}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		case "heartbeat", "complete":
			if job == "lost" {
				w.WriteHeader(http.StatusConflict)
				return
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestCoordClientClaim(t *testing.T) {
	srv := coordTestServer()
	defer srv.Close()
	c := &CoordClient{BaseURL: srv.URL}
	ctx := context.Background()

	cases := []struct {
		job        string
		wantKind   ClaimOutcomeKind
		wantAction RunnerAction
	}{
		{"jclaimed", OutcomeClaimed, ActionExecute},
		{"jcached", OutcomeCached, ActionAdoptCached},
		{"jdeps", OutcomeRejected, ActionWaitDeps},
		{"jheld", OutcomeRejected, ActionSkip},
	}
	for _, tc := range cases {
		t.Run(tc.job, func(t *testing.T) {
			o, err := c.Claim(ctx, "r1", tc.job, ClaimRequest{RunnerID: "runner-1"})
			if err != nil {
				t.Fatalf("claim: %v", err)
			}
			if o.Kind != tc.wantKind {
				t.Fatalf("kind = %d, want %d", o.Kind, tc.wantKind)
			}
			if got := ActionForClaim(o); got != tc.wantAction {
				t.Fatalf("action = %d, want %d", got, tc.wantAction)
			}
		})
	}

	// the claimed case carries the lease tunables through
	o, _ := c.Claim(ctx, "r1", "jclaimed", ClaimRequest{RunnerID: "runner-1"})
	if o.LeaseEpoch != 3 || o.LeaseExpiresAt == "" {
		t.Fatalf("lease not decoded: %+v", o)
	}
	// the cached case carries the result digest
	o, _ = c.Claim(ctx, "r1", "jcached", ClaimRequest{RunnerID: "runner-1"})
	if o.ResultDigest != "sha256:c" {
		t.Fatalf("cached digest = %q", o.ResultDigest)
	}
}

func TestCoordClientHeartbeatAndComplete(t *testing.T) {
	srv := coordTestServer()
	defer srv.Close()
	c := &CoordClient{BaseURL: srv.URL}
	ctx := context.Background()

	if lost, err := c.Heartbeat(ctx, "r1", "ok", "runner-1", 1); err != nil || lost {
		t.Fatalf("healthy heartbeat: lost=%v err=%v", lost, err)
	}
	if lost, _ := c.Heartbeat(ctx, "r1", "lost", "runner-1", 1); !lost || ActionForHeartbeat(lost) != ActionStop {
		t.Fatal("lost heartbeat must report lease lost → stop")
	}
	if lost, err := c.Complete(ctx, "r1", "ok", CompleteRequest{RunnerID: "runner-1", LeaseEpoch: 1, Outcome: "succeeded", ResultDigest: "sha256:x"}); err != nil || lost {
		t.Fatalf("complete: lost=%v err=%v", lost, err)
	}
	if lost, _ := c.Complete(ctx, "r1", "lost", CompleteRequest{RunnerID: "runner-1", LeaseEpoch: 1, Outcome: "failed"}); !lost {
		t.Fatal("complete on a lost lease must report 409")
	}
}

func TestCoordClientContractVersionRequired(t *testing.T) {
	// A server that requires the version header rejects a client that omits it;
	// our client always sends it, so this is a guard that we keep doing so.
	srv := coordTestServer()
	defer srv.Close()
	c := &CoordClient{BaseURL: srv.URL}
	if _, err := c.Claim(context.Background(), "r1", "jclaimed", ClaimRequest{RunnerID: "runner-1"}); err != nil {
		t.Fatalf("client must send Orun-Contract-Version: %v", err)
	}
}

// ── A verb that failed in transit is asked again ─────────────────────────────

func fastCoordRetries(t *testing.T) {
	t.Helper()
	prev := coordRetryWait
	coordRetryWait = func(int, string) time.Duration { return time.Millisecond }
	t.Cleanup(func() { coordRetryWait = prev })
}

// The lane in the field: one reset connection on the claim, before a step had
// run. The claim is asked again and the lane never notices.
func TestCoordClientRetriesAClaimWhoseConnectionWasReset(t *testing.T) {
	fastCoordRetries(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) <= 2 {
			if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
				_ = conn.Close()
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"claimed":true,"leaseEpoch":3,"leaseExpiresAt":"2026-06-19T00:01:00Z"}`))
	}))
	defer srv.Close()
	c := &CoordClient{HTTP: srv.Client(), BaseURL: srv.URL}
	out, err := c.Claim(context.Background(), "run1", "j1", ClaimRequest{RunnerID: "r1"})
	if err != nil {
		t.Fatalf("a reset connection must not fail the claim: %v", err)
	}
	if out.Kind != OutcomeClaimed || out.LeaseEpoch != 3 {
		t.Fatalf("claim outcome: %+v", out)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("the claim was asked %d times, want 3", got)
	}
}

// 503 and 429 are the server not answering this time; a Retry-After hint is
// honoured. The body of a transient answer is drained, never decoded.
func TestCoordClientRetriesTransientStatusesAndHonoursRetryAfter(t *testing.T) {
	var hints []string
	prev := coordRetryWait
	coordRetryWait = func(_ int, hint string) time.Duration {
		hints = append(hints, hint)
		return time.Millisecond
	}
	t.Cleanup(func() { coordRetryWait = prev })
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch atomic.AddInt32(&calls, 1) {
		case 1:
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"slow down"}`))
		case 2:
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer srv.Close()
	c := &CoordClient{HTTP: srv.Client(), BaseURL: srv.URL}
	lost, err := c.Heartbeat(context.Background(), "run1", "j1", "r1", 3)
	if err != nil || lost {
		t.Fatalf("heartbeat after two transient answers: lost=%v err=%v", lost, err)
	}
	if len(hints) != 2 || hints[0] != "2" || hints[1] != "" {
		t.Fatalf("Retry-After hints seen by the wait: %q", hints)
	}
}

// A 4xx is an answer. Asking again would not change it, and a 409 on a
// complete MEANS something (the lease was lost) that a retry must not blur.
func TestCoordClientDoesNotRetryAnAnswer(t *testing.T) {
	fastCoordRetries(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusConflict)
	}))
	defer srv.Close()
	c := &CoordClient{HTTP: srv.Client(), BaseURL: srv.URL}
	lost, err := c.Complete(context.Background(), "run1", "j1", CompleteRequest{RunnerID: "r1", LeaseEpoch: 3, Outcome: "succeeded"})
	if err != nil || !lost {
		t.Fatalf("a 409 is a lost lease, not an error: lost=%v err=%v", lost, err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("a 4xx was asked %d times, want 1", got)
	}
}

// The budget is a bound: a server that never answers fails after it, with the
// transport's own error, and a transient status on the last attempt comes
// back as the status it was.
func TestCoordClientGivesUpAfterTheBudget(t *testing.T) {
	fastCoordRetries(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	c := &CoordClient{HTTP: srv.Client(), BaseURL: srv.URL, RetryAttempts: 3}
	_, err := c.Claim(context.Background(), "run1", "j1", ClaimRequest{RunnerID: "r1"})
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("want the last status reported, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("asked %d times, want the budget of 3", got)
	}
}

// The caller giving up is not the network: a cancelled context ends the
// retries at once.
func TestCoordClientStopsRetryingWhenTheContextEnds(t *testing.T) {
	prev := coordRetryWait
	coordRetryWait = func(int, string) time.Duration { return time.Hour }
	t.Cleanup(func() { coordRetryWait = prev })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	c := &CoordClient{HTTP: srv.Client(), BaseURL: srv.URL}
	done := make(chan error, 1)
	go func() {
		_, err := c.Claim(ctx, "run1", "j1", ClaimRequest{RunnerID: "r1"})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a cancelled wait must surface as an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the retry outlived its context")
	}
}
