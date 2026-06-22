package statebackend

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// tokenFunc adapts a function to the TokenSource interface.
type tokenFunc func(ctx context.Context) (string, error)

func (f tokenFunc) Token(ctx context.Context) (string, error) { return f(ctx) }

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
