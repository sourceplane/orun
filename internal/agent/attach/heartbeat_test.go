package attach

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// hbServer is a configurable stand-in for the agents-worker session routes
// (/heartbeat, /token) so the heartbeat loop can be exercised without the cloud.
type hbServer struct {
	mu           sync.Mutex
	beats        int
	tokenCalls   int
	lastAuth     string          // Authorization on the most recent /heartbeat
	heartbeatFn  func(n int) int // beat number (1-based) → status to return
	freshToken   string          // token /token hands out
	expiresAt    string          // expiresAt /token returns (RFC3339)
	requireToken string          // if set, /heartbeat 401s unless the bearer matches
}

func (s *hbServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.beats++
		n := s.beats
		s.lastAuth = r.Header.Get("Authorization")
		require := s.requireToken
		fn := s.heartbeatFn
		s.mu.Unlock()
		if require != "" && r.Header.Get("Authorization") != "Bearer "+require {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		status := 200
		if fn != nil {
			status = fn(n)
		}
		w.WriteHeader(status)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.tokenCalls++
		tok := s.freshToken
		if tok == "" {
			tok = "refreshed"
		}
		exp := s.expiresAt
		if exp == "" {
			exp = "2999-01-01T00:00:00Z"
		}
		s.requireToken = "" // once refreshed, the new token is accepted
		s.mu.Unlock()
		fmt.Fprintf(w, `{"token":%q,"expiresAt":%q}`, tok, exp)
	})
	return mux
}

func (s *hbServer) beatCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.beats
}

func newHB(t *testing.T, srv *hbServer, cfg HeartbeatConfig, onTerminal func(string)) (*Heartbeat, error) {
	t.Helper()
	ts := httptest.NewServer(srv.handler())
	t.Cleanup(ts.Close)
	cfg.BaseURL = ts.URL
	cfg.HTTP = ts.Client()
	return StartHeartbeat(context.Background(), cfg, onTerminal)
}

// TestHeartbeatFirstBeatSucceeds: a 200 first beat flips the session and starts
// the loop; the beat carried the bearer token.
func TestHeartbeatFirstBeatSucceeds(t *testing.T) {
	srv := &hbServer{}
	hb, err := newHB(t, srv, HeartbeatConfig{Token: "env-tok", Interval: time.Hour}, nil)
	if err != nil {
		t.Fatalf("first beat should succeed: %v", err)
	}
	_ = hb
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.beats < 1 {
		t.Fatal("expected at least one heartbeat")
	}
	if srv.lastAuth != "Bearer env-tok" {
		t.Fatalf("heartbeat must carry the bearer token, got %q", srv.lastAuth)
	}
}

// TestHeartbeatFirstBeatTerminal: a 403 first beat is terminal — no session, and
// (implicitly) no retry-storm past the single classification.
func TestHeartbeatFirstBeatTerminal(t *testing.T) {
	for _, status := range []int{403, 409, 404} {
		srv := &hbServer{heartbeatFn: func(int) int { return status }}
		_, err := newHB(t, srv, HeartbeatConfig{Token: "t", Interval: time.Hour, FirstBeatTries: 4, RetryBackoff: time.Millisecond}, nil)
		if err == nil {
			t.Fatalf("HTTP %d first beat must be terminal", status)
		}
		if got := srv.beatCount(); got != 1 {
			t.Fatalf("terminal status must not retry-storm; want 1 beat, got %d (status %d)", got, status)
		}
	}
}

// TestHeartbeatFirstBeat401Refreshes: a 401 first beat triggers one /token
// refresh and a retry that succeeds with the fresh token (the token-TTL race).
func TestHeartbeatFirstBeat401Refreshes(t *testing.T) {
	srv := &hbServer{requireToken: "good", freshToken: "good"}
	// The env token is "stale" (≠ "good") so the first beat 401s; after /token
	// hands out "good", requireToken is cleared and the retry lands.
	hb, err := newHB(t, srv, HeartbeatConfig{Token: "stale", Interval: time.Hour}, nil)
	if err != nil {
		t.Fatalf("first beat should recover via refresh: %v", err)
	}
	_ = hb
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.tokenCalls < 1 {
		t.Fatal("a 401 must trigger a token refresh")
	}
	if srv.beats < 2 {
		t.Fatalf("expected the beat to be retried after refresh, got %d beats", srv.beats)
	}
}

// TestHeartbeatFirstBeatRetriesTransient: 5xx first beats are retried with
// backoff and the loop starts once one lands.
func TestHeartbeatFirstBeatRetriesTransient(t *testing.T) {
	srv := &hbServer{heartbeatFn: func(n int) int {
		if n <= 2 {
			return 503
		}
		return 200
	}}
	_, err := newHB(t, srv, HeartbeatConfig{Token: "t", Interval: time.Hour, FirstBeatTries: 5, RetryBackoff: time.Millisecond}, nil)
	if err != nil {
		t.Fatalf("first beat should retry past 503s: %v", err)
	}
	if got := srv.beatCount(); got < 3 {
		t.Fatalf("expected retries, got %d beats", got)
	}
}

// TestHeartbeatTerminalMidLoop: once running, a terminal beat (console kill)
// fires onTerminal so serve can stop the agent.
func TestHeartbeatTerminalMidLoop(t *testing.T) {
	srv := &hbServer{heartbeatFn: func(n int) int {
		if n >= 2 {
			return 403 // killed from the console after the first beat
		}
		return 200
	}}
	terminated := make(chan string, 1)
	_, err := newHB(t, srv, HeartbeatConfig{
		Token: "t", Interval: 10 * time.Millisecond, RefreshMargin: time.Millisecond,
	}, func(reason string) { terminated <- reason })
	if err != nil {
		t.Fatalf("first beat should succeed: %v", err)
	}
	select {
	case <-terminated:
	case <-time.After(2 * time.Second):
		t.Fatal("a terminal mid-loop beat must call onTerminal")
	}
}
