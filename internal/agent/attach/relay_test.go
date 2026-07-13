package attach

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/agent"
	"github.com/sourceplane/orun/internal/agent/driver"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
)

// fakeRelay is a minimal in-memory stand-in for the cloud DO relay (cloud
// AL6): it stores event frames the body posts, serves them (plus live tail)
// to head SSE clients, and bridges head input POSTs into the body's input
// long-poll — exactly the two directions attach-protocol.md §6.3 specifies.
// It proves serve↔attach over HTTP with no Daytona.
type fakeRelay struct {
	mu       sync.Mutex
	events   []Frame          // everything the body posted to /events (hello/event/live)
	subs     []chan Frame     // live head SSE feeds
	inputs   []Frame          // head inputs awaiting the body's long-poll
	acks     map[string]Frame // ref → ack (for head sync waits)
	ackWaits map[string]chan Frame
}

func newFakeRelay() *fakeRelay {
	return &fakeRelay{acks: map[string]Frame{}, ackWaits: map[string]chan Frame{}}
}

func (r *fakeRelay) handler() http.Handler {
	mux := http.NewServeMux()
	// Body → relay: event batches.
	mux.HandleFunc("/s/events", func(w http.ResponseWriter, req *http.Request) {
		var batch []Frame
		json.NewDecoder(req.Body).Decode(&batch)
		r.mu.Lock()
		for _, f := range batch {
			r.events = append(r.events, f)
			for _, sub := range r.subs {
				select {
				case sub <- f:
				default:
				}
			}
		}
		r.mu.Unlock()
		w.WriteHeader(200)
	})
	mux.HandleFunc("/s/stream", func(w http.ResponseWriter, req *http.Request) { w.WriteHeader(200) })
	// Body → relay: pull head inputs (long-poll simplified to immediate).
	mux.HandleFunc("/s/inputs", func(w http.ResponseWriter, req *http.Request) {
		cursor, _ := strconv.Atoi(req.URL.Query().Get("cursor"))
		r.mu.Lock()
		items := append([]Frame(nil), r.inputs[cursor:]...)
		next := len(r.inputs)
		r.mu.Unlock()
		json.NewEncoder(w).Encode(inputsResponse{Items: items, Cursor: next})
	})
	// Body → relay: ack for a head input.
	mux.HandleFunc("/s/inputs/ack", func(w http.ResponseWriter, req *http.Request) {
		var ack Frame
		json.NewDecoder(req.Body).Decode(&ack)
		r.mu.Lock()
		r.acks[ack.Ref] = ack
		if ch := r.ackWaits[ack.Ref]; ch != nil {
			ch <- ack
			delete(r.ackWaits, ack.Ref)
		}
		r.mu.Unlock()
		w.WriteHeader(200)
	})
	// Head → relay: SSE attach feed.
	mux.HandleFunc("/s/attach", func(w http.ResponseWriter, req *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flush", 500)
			return
		}
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(200)
		sub := make(chan Frame, 256)
		r.mu.Lock()
		replay := append([]Frame(nil), r.events...)
		r.subs = append(r.subs, sub)
		r.mu.Unlock()
		writeSSE := func(f Frame) {
			b, _ := json.Marshal(f)
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		}
		for _, f := range replay {
			writeSSE(f)
		}
		for {
			select {
			case <-req.Context().Done():
				return
			case f := <-sub:
				writeSSE(f)
				if f.T == TBye {
					return
				}
			}
		}
	})
	// Head → relay: input POST, answered with the body's ack.
	mux.HandleFunc("/s/input", func(w http.ResponseWriter, req *http.Request) {
		var f Frame
		json.NewDecoder(req.Body).Decode(&f)
		wait := make(chan Frame, 1)
		r.mu.Lock()
		if ack, done := r.acks[f.Ref]; done {
			wait <- ack
		} else {
			r.ackWaits[f.Ref] = wait
		}
		r.inputs = append(r.inputs, f)
		r.mu.Unlock()
		select {
		case ack := <-wait:
			json.NewEncoder(w).Encode(ack)
		case <-time.After(3 * time.Second):
			json.NewEncoder(w).Encode(AckFrame(f.Ref, false, ReasonTerminal))
		}
	})
	return mux
}

// relayClient returns a fresh HTTP client trusting the test server — one per
// simulated process, so the SSE stream and the poll/post traffic never share a
// transport.
func relayClient(ts *httptest.Server) *http.Client {
	return &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}}
}

// TestDialToRelayRetriesFirstDialHome proves the first /events dial-home
// survives a cold-boot race: the relay rejects the first two POST /events
// (relay not warm / token not yet active) and DialToRelay retries until it
// lands, rather than giving up and leaving the cloud blind.
func TestDialToRelayRetriesFirstDialHome(t *testing.T) {
	var mu sync.Mutex
	var eventsPosts int
	var gotHello, gotLive bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/events" {
			w.WriteHeader(200)
			return
		}
		mu.Lock()
		eventsPosts++
		n := eventsPosts
		mu.Unlock()
		if n <= 2 {
			w.WriteHeader(503) // relay not warm yet
			return
		}
		var batch []Frame
		json.NewDecoder(req.Body).Decode(&batch)
		mu.Lock()
		for _, f := range batch {
			if f.T == THello {
				gotHello = true
			}
			if f.T == TLive {
				gotLive = true
			}
		}
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer ts.Close()

	srv := NewServer(SessionInfo{SessionID: "as_retry", RunKind: "interactive", Harness: "stub"}, agent.NewInputQueue())
	sess, err := DialToRelay(context.Background(), srv, agent.NewInputQueue(), RelayConfig{
		BaseURL: ts.URL, Token: "tok", HTTP: ts.Client(),
		DialRetries: 5, RetryBackoff: time.Millisecond,
		FlushEvery: 10 * time.Millisecond, PollEvery: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("DialToRelay should have retried past the transient 503s: %v", err)
	}
	defer sess.Close()
	mu.Lock()
	defer mu.Unlock()
	if eventsPosts < 3 {
		t.Fatalf("expected the first dial-home to be retried; only %d posts", eventsPosts)
	}
	if !gotHello || !gotLive {
		t.Fatalf("first dial-home should carry the hello+live catch-up (hello=%v live=%v)", gotHello, gotLive)
	}
}

// TestDialToRelayFailsLoudOnAuthReject proves an expired/invalid session token
// (candidate: the 15-min TTL outlasting a cold boot) fails LOUDLY with a
// token-specific diagnostic instead of being swallowed into a 30-min silent
// lease_lost.
func TestDialToRelayFailsLoudOnAuthReject(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()

	srv := NewServer(SessionInfo{SessionID: "as_401", RunKind: "interactive", Harness: "stub"}, agent.NewInputQueue())
	sess, err := DialToRelay(context.Background(), srv, agent.NewInputQueue(), RelayConfig{
		BaseURL: ts.URL, Token: "expired", HTTP: ts.Client(),
		DialRetries: 2, RetryBackoff: time.Millisecond,
	})
	if err == nil {
		sess.Close()
		t.Fatal("DialToRelay must fail when the first heartbeat is rejected 401, not run blind")
	}
	if sess != nil {
		t.Fatal("no RelaySession should be returned on a failed dial-home")
	}
	msg := err.Error()
	if !strings.Contains(msg, "401") || !strings.Contains(msg, "ORUN_SESSION_TOKEN") {
		t.Fatalf("error should name the 401 and the token; got: %v", err)
	}
}

// TestRelayUsesLiveTokenFn proves the relay presents the live TokenFn bearer
// (the heartbeat's refreshed token), not the static boot token — so relay auth
// does not lapse ~15m in when the boot token expires.
func TestRelayUsesLiveTokenFn(t *testing.T) {
	var mu sync.Mutex
	var lastAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/events" {
			mu.Lock()
			lastAuth = req.Header.Get("Authorization")
			mu.Unlock()
		}
		w.WriteHeader(200)
	}))
	defer ts.Close()

	// tokenBox stands in for the heartbeat's mutex-guarded live token.
	var tokBox struct {
		sync.Mutex
		val string
	}
	tokBox.val = "boot-token"
	tokenFn := func() string { tokBox.Lock(); defer tokBox.Unlock(); return tokBox.val }

	srv := NewServer(SessionInfo{SessionID: "as_tok", RunKind: "interactive", Harness: "stub"}, agent.NewInputQueue())
	sess, err := DialToRelay(context.Background(), srv, agent.NewInputQueue(), RelayConfig{
		BaseURL: ts.URL, Token: "boot-token", TokenFn: tokenFn,
		HTTP: ts.Client(), FlushEvery: 10 * time.Millisecond, PollEvery: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("dial-home: %v", err)
	}
	mu.Lock()
	firstAuth := lastAuth
	mu.Unlock()
	if firstAuth != "Bearer boot-token" {
		t.Fatalf("expected the TokenFn bearer, got %q", firstAuth)
	}
	sess.Close() // stop the pump before mutating the token

	// Simulate the heartbeat refreshing the token: a later post must carry it.
	tokBox.Lock()
	tokBox.val = "refreshed-token"
	tokBox.Unlock()
	if err := postJSON(context.Background(), RelayConfig{
		BaseURL: ts.URL, TokenFn: tokenFn, HTTP: ts.Client(),
	}, "/events", []Frame{}); err != nil {
		t.Fatalf("post: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if lastAuth != "Bearer refreshed-token" {
		t.Fatalf("relay must track the refreshed token, got %q", lastAuth)
	}
}

func TestServeAndRemoteAttachOverRelay(t *testing.T) {
	relay := newFakeRelay()
	ts := httptest.NewServer(relay.handler())
	defer ts.Close()

	// The body: an interactive stub session, its attach Server bridged to the
	// relay via ServeToRelay (what `orun agent serve` runs).
	store := objectstore.NewMemStore(objectstore.AlgoSHA256)
	brief, err := agent.AssembleBrief(context.Background(), store, agent.BriefInput{RunKind: nodes.RunKindInteractive})
	if err != nil {
		t.Fatal(err)
	}
	q := agent.NewInputQueue()
	srv := NewServer(SessionInfo{SessionID: "as_relay1", RunKind: "interactive", Harness: "stub"}, q)
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		_, _ = agent.Run(context.Background(), store, agent.RunOptions{
			SessionID: "as_relay1", Driver: &driver.Stub{Interactive: true}, Brief: brief,
			Inputs: q, Observe: srv.Observe, ObserveDelta: srv.ObserveDelta,
		})
		srv.Close("terminal")
	}()
	serveCtx, serveCancel := context.WithCancel(context.Background())
	defer serveCancel()
	// The body and the head are separate processes in production; give them
	// separate HTTP clients so the persistent SSE stream never contends with
	// the body's poll/post transport.
	go ServeToRelay(serveCtx, srv, q, RelayConfig{
		BaseURL: ts.URL + "/s", HTTP: relayClient(ts), FlushEvery: 20 * time.Millisecond, PollEvery: 20 * time.Millisecond,
	})

	// The remote head: DialRelay is what `orun agent attach as_…` runs.
	head, err := DialRelay(context.Background(), ts.URL+"/s", "bearer-tok", -1, relayClient(ts))
	if err != nil {
		t.Fatal(err)
	}
	r := clientReader{ch: head.Frames()}

	r.waitFor(t, "hello", func(f Frame) bool { return f.T == THello })
	r.waitFor(t, "live", func(f Frame) bool { return f.T == TLive })

	// Steer over the relay: the ack resolves through the body's return queue.
	if err := head.Steer("hello over the relay"); err != nil {
		t.Fatalf("steer: %v", err)
	}
	r.waitFor(t, "user turn", func(f Frame) bool {
		return f.T == TEvent && f.Kind == "message_user" && f.Payload["text"] == "hello over the relay"
	})
	r.waitFor(t, "agent echo", func(f Frame) bool {
		return f.T == TEvent && f.Kind == "message_agent" && f.Payload["text"] == "echo: hello over the relay"
	})

	// Approval over the relay.
	if err := head.Steer("/ask contract_propose"); err != nil {
		t.Fatal(err)
	}
	ask := r.waitFor(t, "ask", func(f Frame) bool { return f.T == TEvent && f.Kind == "approval_requested" })
	reqID, _ := ask.Payload["requestId"].(string)
	if err := head.Verdict(reqID, true, "lgtm"); err != nil {
		t.Fatalf("verdict: %v", err)
	}
	r.waitFor(t, "resolution", func(f Frame) bool { return f.T == TEvent && f.Kind == "approval_resolved" })

	// End the session through the relay (a /done steer, the proven stub path).
	if err := head.Steer("/done"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runDone:
	case <-time.After(10 * time.Second):
		t.Fatal("body did not end after /done")
	}
	// Explicit teardown: detach the head and stop serve so ts.Close (which
	// waits on the SSE handler) does not block on a live stream.
	head.Detach()
	serveCancel()
}
