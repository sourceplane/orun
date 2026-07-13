package attach

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
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
		// The body posts only durable events to /events; the cloud synthesizes
		// the attach-protocol framing (hello + live) for a head from the session
		// it already knows. Model that so the remote head still initializes.
		writeSSE(HelloFrame(SessionInfo{SessionID: "as_relay1", RunKind: "interactive", Harness: "stub"}, "running", len(replay)-1))
		writeSSE(LiveFrame(-1))
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

// waitUntil polls cond until it holds or the deadline elapses (test helper for
// the async relay pumps).
func waitUntil(t *testing.T, d time.Duration, cond func() bool, msg string) {
	t.Helper()
	for start := time.Now(); time.Since(start) < d; {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !cond() {
		t.Fatalf("timeout: %s", msg)
	}
}

// TestRelayPostsOnlyEventFrames proves the durable /events log carries event
// frames only — hello/live/other control frames are not written as events.
func TestRelayPostsOnlyEventFrames(t *testing.T) {
	var mu sync.Mutex
	var posted []Frame
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/events" {
			var batch []Frame
			json.NewDecoder(req.Body).Decode(&batch)
			mu.Lock()
			posted = append(posted, batch...)
			mu.Unlock()
		}
		w.WriteHeader(200)
	}))
	defer ts.Close()

	srv := NewServer(SessionInfo{SessionID: "as_ev", RunKind: "interactive", Harness: "stub"}, agent.NewInputQueue())
	sess, err := DialToRelay(context.Background(), srv, agent.NewInputQueue(), RelayConfig{
		BaseURL: ts.URL, Token: "tok", HTTP: ts.Client(),
		FlushEvery: 5 * time.Millisecond, PollEvery: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	srv.Observe(agent.SessionEvent{Seq: 0, Kind: nodes.SessionEventMessageAgent, Payload: map[string]any{"text": "hi"}})

	waitUntil(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(posted) >= 1
	}, "expected the event to be posted to /events")

	mu.Lock()
	defer mu.Unlock()
	sawEvent := false
	for _, f := range posted {
		if f.T != TEvent {
			t.Fatalf("only event frames belong in the durable log, got t=%q on /events", f.T)
		}
		if f.Kind == nodes.SessionEventMessageAgent {
			sawEvent = true
		}
	}
	if !sawEvent {
		t.Fatal("message_agent event never reached /events")
	}
}

// TestRelayPumpSurvivesPostFailure proves one failed /events POST does not kill
// the write-path: after a transient 503 the pump keeps running and posts the
// next event once the endpoint recovers. This is the fix for the whole-session
// blackout where a single returned error stopped all further posting.
func TestRelayPumpSurvivesPostFailure(t *testing.T) {
	var mu sync.Mutex
	fail := true
	okPosts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/events" {
			mu.Lock()
			f := fail
			if !f {
				okPosts++
			}
			mu.Unlock()
			if f {
				w.WriteHeader(503)
				return
			}
		}
		w.WriteHeader(200)
	}))
	defer ts.Close()

	srv := NewServer(SessionInfo{SessionID: "as_resil", RunKind: "interactive", Harness: "stub"}, agent.NewInputQueue())
	sess, err := DialToRelay(context.Background(), srv, agent.NewInputQueue(), RelayConfig{
		BaseURL: ts.URL, Token: "tok", HTTP: ts.Client(),
		PostRetries: 2, RetryBackoff: time.Millisecond,
		FlushEvery: 5 * time.Millisecond, PollEvery: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	// First event: POST /events 503s, retried, then dropped — the pump must live.
	srv.Observe(agent.SessionEvent{Seq: 0, Kind: nodes.SessionEventMessageAgent, Payload: map[string]any{"text": "one"}})
	time.Sleep(60 * time.Millisecond)

	// Endpoint recovers; the next event must still post (pump survived).
	mu.Lock()
	fail = false
	mu.Unlock()
	srv.Observe(agent.SessionEvent{Seq: 1, Kind: nodes.SessionEventMessageAgent, Payload: map[string]any{"text": "two"}})

	waitUntil(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return okPosts >= 1
	}, "pump should have survived the failure and posted the recovered event")
}

// TestRelayUsesLiveTokenFn proves the relay presents the live TokenFn bearer
// (the heartbeat's refreshed token), not the static boot token — so relay auth
// does not lapse ~15m in when the boot token expires.
func TestRelayUsesLiveTokenFn(t *testing.T) {
	var mu sync.Mutex
	var auths []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/events" {
			mu.Lock()
			auths = append(auths, req.Header.Get("Authorization"))
			mu.Unlock()
		}
		w.WriteHeader(200)
	}))
	defer ts.Close()

	// tokBox stands in for the heartbeat's mutex-guarded live token.
	var tokBox struct {
		sync.Mutex
		val string
	}
	tokBox.val = "boot-token"
	tokenFn := func() string { tokBox.Lock(); defer tokBox.Unlock(); return tokBox.val }

	srv := NewServer(SessionInfo{SessionID: "as_tok", RunKind: "interactive", Harness: "stub"}, agent.NewInputQueue())
	sess, err := DialToRelay(context.Background(), srv, agent.NewInputQueue(), RelayConfig{
		BaseURL: ts.URL, TokenFn: tokenFn,
		HTTP: ts.Client(), FlushEvery: 5 * time.Millisecond, PollEvery: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	srv.Observe(agent.SessionEvent{Seq: 0, Kind: nodes.SessionEventMessageAgent, Payload: map[string]any{"text": "boot"}})
	waitUntil(t, time.Second, func() bool { mu.Lock(); defer mu.Unlock(); return len(auths) >= 1 }, "first event should post")
	mu.Lock()
	if auths[0] != "Bearer boot-token" {
		mu.Unlock()
		t.Fatalf("expected the TokenFn bearer, got %q", auths[0])
	}
	before := len(auths)
	mu.Unlock()

	// Simulate the heartbeat refreshing the token: a later event must carry it.
	tokBox.Lock()
	tokBox.val = "refreshed-token"
	tokBox.Unlock()
	srv.Observe(agent.SessionEvent{Seq: 1, Kind: nodes.SessionEventMessageAgent, Payload: map[string]any{"text": "later"}})
	waitUntil(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(auths) > before && auths[len(auths)-1] == "Bearer refreshed-token"
	}, "relay must track the refreshed token")
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
