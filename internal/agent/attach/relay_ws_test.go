package attach

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/sourceplane/orun/internal/agent"
	"github.com/sourceplane/orun/internal/agent/driver"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
)

// fakeWireRelay is the AN0 fixture relay: the fakeRelay HTTP surface PLUS the
// body wire — GET /s/wire upgrades to a WebSocket that pushes queued head
// inputs and records everything the body sends back (acks, deltas, pongs).
// Unacked inputs are re-pushed on reconnect and remain readable over the HTTP
// long-poll, which is how a mid-session downgrade loses nothing.
type fakeWireRelay struct {
	mu       sync.Mutex
	inputs   []Frame          // every head input ever queued (the return queue)
	acked    map[string]Frame // ref → ack (from either carriage)
	ackCh    chan Frame
	deltas   []Frame  // deltas received over the wire
	bearers  []string // Authorization presented on each wire dial
	conns    []*websocket.Conn
	dials    int
	events   [][]Frame // durable batches (HTTP POST /events — the log)
	street   []Frame   // deltas received over HTTP POST /stream
	rejectWS bool      // 404 the wire (capability absent)
	pushBye  bool      // greet the next wire with a bye
}

func newFakeWireRelay() *fakeWireRelay {
	return &fakeWireRelay{acked: map[string]Frame{}, ackCh: make(chan Frame, 64)}
}

// queueInput adds a head input; live wires get it pushed immediately.
func (r *fakeWireRelay) queueInput(f Frame) {
	r.mu.Lock()
	r.inputs = append(r.inputs, f)
	conns := append([]*websocket.Conn(nil), r.conns...)
	r.mu.Unlock()
	for _, c := range conns {
		b, _ := json.Marshal(f)
		_ = c.Write(context.Background(), websocket.MessageText, b)
	}
}

func (r *fakeWireRelay) dropWires() {
	r.mu.Lock()
	conns := r.conns
	r.conns = nil
	r.mu.Unlock()
	for _, c := range conns {
		_ = c.Close(websocket.StatusGoingAway, "relay restart")
	}
}

func (r *fakeWireRelay) wireCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.conns)
}

func (r *fakeWireRelay) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/s/wire", func(w http.ResponseWriter, req *http.Request) {
		if r.rejectWS {
			http.NotFound(w, req)
			return
		}
		conn, err := websocket.Accept(w, req, nil)
		if err != nil {
			return
		}
		r.mu.Lock()
		r.dials++
		r.bearers = append(r.bearers, req.Header.Get("Authorization"))
		r.conns = append(r.conns, conn)
		pushBye := r.pushBye
		// Re-push everything unacked (the reconnect discipline).
		var pending []Frame
		for _, f := range r.inputs {
			if _, ok := r.acked[f.Ref]; !ok {
				pending = append(pending, f)
			}
		}
		r.mu.Unlock()
		bg := context.Background()
		if pushBye {
			b, _ := json.Marshal(ByeFrame("terminal"))
			_ = conn.Write(bg, websocket.MessageText, b)
		}
		for _, f := range pending {
			b, _ := json.Marshal(f)
			_ = conn.Write(bg, websocket.MessageText, b)
		}
		go func() {
			for {
				_, data, err := conn.Read(context.Background())
				if err != nil {
					return
				}
				var f Frame
				if json.Unmarshal(data, &f) != nil {
					continue
				}
				r.mu.Lock()
				switch f.T {
				case TAck:
					r.acked[f.Ref] = f
					select {
					case r.ackCh <- f:
					default:
					}
				case TDelta:
					r.deltas = append(r.deltas, f)
				}
				r.mu.Unlock()
			}
		}()
	})
	mux.HandleFunc("/s/events", func(w http.ResponseWriter, req *http.Request) {
		var batch []Frame
		json.NewDecoder(req.Body).Decode(&batch)
		r.mu.Lock()
		r.events = append(r.events, batch)
		r.mu.Unlock()
		w.WriteHeader(200)
	})
	mux.HandleFunc("/s/stream", func(w http.ResponseWriter, req *http.Request) {
		var f Frame
		json.NewDecoder(req.Body).Decode(&f)
		r.mu.Lock()
		r.street = append(r.street, f)
		r.mu.Unlock()
		w.WriteHeader(200)
	})
	mux.HandleFunc("/s/inputs", func(w http.ResponseWriter, req *http.Request) {
		cursor, _ := strconv.Atoi(req.URL.Query().Get("cursor"))
		r.mu.Lock()
		items := append([]Frame(nil), r.inputs[cursor:]...)
		next := len(r.inputs)
		r.mu.Unlock()
		json.NewEncoder(w).Encode(inputsResponse{Items: items, Cursor: next})
	})
	mux.HandleFunc("/s/inputs/ack", func(w http.ResponseWriter, req *http.Request) {
		var ack Frame
		json.NewDecoder(req.Body).Decode(&ack)
		r.mu.Lock()
		r.acked[ack.Ref] = ack
		r.mu.Unlock()
		select {
		case r.ackCh <- ack:
		default:
		}
		w.WriteHeader(200)
	})
	return mux
}

func wireTestConfig(ts *httptest.Server, extra func(*RelayConfig)) RelayConfig {
	cfg := RelayConfig{
		BaseURL:         ts.URL + "/s",
		Token:           "tok-boot",
		HTTP:            ts.Client(),
		WSHTTP:          ts.Client(),
		PollEvery:       20 * time.Millisecond,
		FlushEvery:      20 * time.Millisecond,
		WSRetryEvery:    150 * time.Millisecond,
		TokenCheckEvery: 20 * time.Millisecond,
	}
	if extra != nil {
		extra(&cfg)
	}
	return cfg
}

// startWireBody runs a live interactive stub session (the agent.Run loop
// consuming the InputQueue — the same arrangement `orun agent serve` has) and
// bridges it to the fixture relay with the given config.
func startWireBody(t *testing.T, ts *httptest.Server, extra func(*RelayConfig)) (sess *RelaySession, cleanup func()) {
	t.Helper()
	store := objectstore.NewMemStore(objectstore.AlgoSHA256)
	brief, err := agent.AssembleBrief(context.Background(), store, agent.BriefInput{RunKind: nodes.RunKindInteractive})
	if err != nil {
		t.Fatal(err)
	}
	q := agent.NewInputQueue()
	srv := NewServer(SessionInfo{SessionID: "as_wire", RunKind: "interactive", Harness: "stub"}, q)
	runCtx, runCancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		_, _ = agent.Run(runCtx, store, agent.RunOptions{
			SessionID: "as_wire", Driver: &driver.Stub{Interactive: true}, Brief: brief,
			Inputs: q, Observe: srv.Observe, ObserveDelta: srv.ObserveDelta,
		})
		srv.Close("terminal")
	}()
	ctx, cancel := context.WithCancel(context.Background())
	sess, err = DialToRelay(ctx, srv, q, wireTestConfig(ts, extra))
	if err != nil {
		runCancel()
		t.Fatalf("dial: %v", err)
	}
	return sess, func() {
		sess.Close()
		cancel()
		runCancel()
		<-runDone
	}
}

func waitAck(t *testing.T, r *fakeWireRelay, ref string) Frame {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case a := <-r.ackCh:
			if a.Ref == ref {
				return a
			}
		case <-deadline:
			t.Fatalf("no ack for %s", ref)
		}
	}
}

func waitCond(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout: %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestWirePushInputs: with the HTTP long-poll effectively disabled (10-minute
// spacing), a steer still reaches the runtime and acks — the wire alone
// carried it, at push latency. The acid test for "no poll interval in the
// path" (AN0 done-when).
func TestWirePushInputs(t *testing.T) {
	relay := newFakeWireRelay()
	ts := httptest.NewServer(relay.handler())
	defer ts.Close()

	_, cleanup := startWireBody(t, ts, func(c *RelayConfig) {
		c.PollEvery = 10 * time.Minute // only the wire can deliver
	})
	defer cleanup()
	waitCond(t, "wire up", func() bool { return relay.wireCount() > 0 })

	relay.queueInput(SteerFrame("in-1", "hello over the wire"))
	ack := waitAck(t, relay, "in-1")
	if ack.OK == nil || !*ack.OK {
		t.Fatalf("steer not acked ok: %+v", ack)
	}
}

// TestWireDowngradeLosesNothing: a mid-session socket drop falls back to the
// HTTP long-poll; an input queued during the outage is delivered and acked —
// the sealed session shows no gap (AN0 done-when).
func TestWireDowngradeLosesNothing(t *testing.T) {
	relay := newFakeWireRelay()
	ts := httptest.NewServer(relay.handler())
	defer ts.Close()

	_, cleanup := startWireBody(t, ts, func(c *RelayConfig) {
		c.WSRetryEvery = 5 * time.Second // the fallback long-poll must do the work
	})
	defer cleanup()
	waitCond(t, "wire up", func() bool { return relay.wireCount() > 0 })

	relay.queueInput(SteerFrame("in-1", "first, over the wire"))
	waitAck(t, relay, "in-1")

	relay.dropWires()
	relay.queueInput(SteerFrame("in-2", "second, during the outage"))
	ack := waitAck(t, relay, "in-2")
	if ack.OK == nil || !*ack.OK {
		t.Fatalf("post-drop steer not acked ok: %+v", ack)
	}
}

// TestWireReconnectRepush: after a drop, the next wire dial receives the
// unacked backlog (the relay re-pushes) — nothing sealed is lost across the
// seam, and the redial happens on its own.
func TestWireReconnectRepush(t *testing.T) {
	relay := newFakeWireRelay()
	ts := httptest.NewServer(relay.handler())
	defer ts.Close()

	_, cleanup := startWireBody(t, ts, func(c *RelayConfig) {
		c.PollEvery = 10 * time.Minute // deliveries must ride a wire, first or second
		c.WSRetryEvery = 100 * time.Millisecond
	})
	defer cleanup()
	waitCond(t, "wire up", func() bool { return relay.wireCount() > 0 })

	relay.dropWires()

	// The body notices the drop, runs its HTTP window, and re-dials on its
	// own — no input traffic needed to provoke it.
	waitCond(t, "automatic re-dial", func() bool {
		relay.mu.Lock()
		defer relay.mu.Unlock()
		return relay.dials >= 2 && len(relay.conns) > 0
	})

	// Push over the NEW wire still lands at push latency (long-poll asleep).
	relay.queueInput(SteerFrame("in-1", "after the redial"))
	ack := waitAck(t, relay, "in-1")
	if ack.OK == nil || !*ack.OK {
		t.Fatalf("post-redial steer not acked ok: %+v", ack)
	}
}

// TestWireTokenRotationRedials: when the live bearer changes, the wire closes
// and re-dials presenting the fresh token, and inputs keep flowing (AN0
// done-when: a rotated token re-dials without dropping frames).
func TestWireTokenRotationRedials(t *testing.T) {
	relay := newFakeWireRelay()
	ts := httptest.NewServer(relay.handler())
	defer ts.Close()

	var mu sync.Mutex
	token := "tok-1"
	_, cleanup := startWireBody(t, ts, func(c *RelayConfig) {
		c.PollEvery = 10 * time.Minute
		c.TokenFn = func() string { mu.Lock(); defer mu.Unlock(); return token }
	})
	defer cleanup()
	waitCond(t, "wire up", func() bool { return relay.wireCount() > 0 })

	relay.queueInput(SteerFrame("in-1", "pre-rotation"))
	waitAck(t, relay, "in-1")

	mu.Lock()
	token = "tok-2"
	mu.Unlock()

	waitCond(t, "re-dial with rotated bearer", func() bool {
		relay.mu.Lock()
		defer relay.mu.Unlock()
		n := len(relay.bearers)
		return n > 0 && relay.bearers[n-1] == "Bearer tok-2"
	})

	relay.queueInput(SteerFrame("in-2", "post-rotation"))
	ack := waitAck(t, relay, "in-2")
	if ack.OK == nil || !*ack.OK {
		t.Fatalf("post-rotation steer not acked ok: %+v", ack)
	}
}

// TestWireCapabilityFallback: a relay that does not speak the wire (404) gets
// the HTTP long-poll binding — everything still works, no wire connection, no
// retry storm (the HTTP binding remains valid indefinitely, lock 2).
func TestWireCapabilityFallback(t *testing.T) {
	relay := newFakeWireRelay()
	relay.rejectWS = true
	ts := httptest.NewServer(relay.handler())
	defer ts.Close()

	_, cleanup := startWireBody(t, ts, nil)
	defer cleanup()

	relay.queueInput(SteerFrame("in-1", "over the long-poll"))
	ack := waitAck(t, relay, "in-1")
	if ack.OK == nil || !*ack.OK {
		t.Fatalf("steer not acked over HTTP: %+v", ack)
	}
	if n := relay.wireCount(); n != 0 {
		t.Fatalf("no wire should exist, got %d", n)
	}
}

// TestWireDeltasRideTheSocket: with the wire up, deltas go over the socket
// (no HTTP /stream traffic); with the wire disabled they take POST /stream —
// and the DURABLE event batches take POST /events in both worlds, so the
// cloud-side log is identical whichever carriage the chatter used (AN0
// done-when: identical cloud-side logs over WS and HTTP).
func TestWireDeltasRideTheSocket(t *testing.T) {
	run := func(disable bool) (wireDeltas, httpDeltas int, durable []Frame) {
		relay := newFakeWireRelay()
		ts := httptest.NewServer(relay.handler())
		defer ts.Close()
		inputs := agent.NewInputQueue()
		srv := NewServer(SessionInfo{SessionID: "as_delta", RunKind: "interactive"}, inputs)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		sess, err := DialToRelay(ctx, srv, inputs, wireTestConfig(ts, func(c *RelayConfig) {
			c.DisableWS = disable
		}))
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer sess.Close()
		if !disable {
			waitCond(t, "wire up", func() bool { return relay.wireCount() > 0 })
		}
		srv.ObserveDelta(driver.Event{Text: "strea", Fields: map[string]any{"turn": 2}})
		srv.Observe(agent.SessionEvent{Seq: 0, Kind: nodes.SessionEventMessageAgent, Payload: map[string]any{"text": "hello"}})
		waitCond(t, "delta+events arrive", func() bool {
			relay.mu.Lock()
			defer relay.mu.Unlock()
			return len(relay.deltas)+len(relay.street) > 0 && len(relay.events) > 0
		})
		relay.mu.Lock()
		defer relay.mu.Unlock()
		return len(relay.deltas), len(relay.street), append([]Frame(nil), relay.events[0]...)
	}

	wsD, wsH, wsLog := run(false)
	if wsD == 0 || wsH != 0 {
		t.Fatalf("wire-up deltas should ride the socket (wire=%d http=%d)", wsD, wsH)
	}
	pollD, pollH, pollLog := run(true)
	if pollH == 0 || pollD != 0 {
		t.Fatalf("disabled-wire deltas should take POST /stream (wire=%d http=%d)", pollD, pollH)
	}
	a, _ := json.Marshal(wsLog)
	b, _ := json.Marshal(pollLog)
	if string(a) != string(b) {
		t.Fatalf("durable logs differ across bindings:\nws:   %s\npoll: %s", a, b)
	}
}

// TestWireByeStopsThePumps: a relay-initiated bye on the wire ends the down
// pump cleanly (the session is over) instead of re-dialing forever.
func TestWireByeStopsThePumps(t *testing.T) {
	relay := newFakeWireRelay()
	relay.pushBye = true
	ts := httptest.NewServer(relay.handler())
	defer ts.Close()

	inputs := agent.NewInputQueue()
	srv := NewServer(SessionInfo{SessionID: "as_bye", RunKind: "interactive"}, inputs)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sess, err := DialToRelay(ctx, srv, inputs, wireTestConfig(ts, nil))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	select {
	case <-sess.Done():
		// pumpDown exited on the bye.
	case <-time.After(5 * time.Second):
		t.Fatal("pumps did not stop on wire bye")
	}
	sess.Close()
}
