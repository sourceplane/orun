package attach

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/agent"
	"github.com/sourceplane/orun/internal/agent/driver"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
)

// headReader pumps a HeadConn's feed into a channel so tests can wait with a
// deadline instead of blocking forever on a bug.
type headReader struct {
	ch   chan Frame
	done chan struct{}
}

func newHeadReader(h *HeadConn) *headReader {
	r := &headReader{ch: make(chan Frame, 64), done: make(chan struct{})}
	go func() {
		defer close(r.done)
		for {
			f, ok := h.Recv()
			if !ok {
				close(r.ch)
				return
			}
			r.ch <- f
		}
	}()
	return r
}

// waitFor returns the first frame matching pred, failing the test after 5s.
func (r *headReader) waitFor(t *testing.T, what string, pred func(Frame) bool) Frame {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case f, ok := <-r.ch:
			if !ok {
				t.Fatalf("waiting for %s: feed closed", what)
			}
			if pred(f) {
				return f
			}
		case <-deadline:
			t.Fatalf("waiting for %s: timed out", what)
		}
	}
}

func eventKind(kind string) func(Frame) bool {
	return func(f Frame) bool { return f.T == TEvent && f.Kind == kind }
}

// startInteractive launches an interactive stub session wired to an attach
// server, returning the server and a channel carrying Run's result.
func startInteractive(t *testing.T, sessionID string) (*Server, *agent.InputQueue, chan agent.RunResult, objectstore.ObjectStore) {
	t.Helper()
	ctx := context.Background()
	store := objectstore.NewMemStore(objectstore.AlgoSHA256)
	brief, err := agent.AssembleBrief(ctx, store, agent.BriefInput{RunKind: nodes.RunKindInteractive, Task: "ORN-7"})
	if err != nil {
		t.Fatal(err)
	}
	q := agent.NewInputQueue()
	srv := NewServer(SessionInfo{SessionID: sessionID, BriefID: brief.ID, AgentType: "implementer",
		Task: "ORN-7", RunKind: "interactive", Harness: "stub"}, q)
	resCh := make(chan agent.RunResult, 1)
	go func() {
		res, err := agent.Run(ctx, store, agent.RunOptions{
			SessionID:    sessionID,
			Driver:       &driver.Stub{Interactive: true},
			Brief:        brief,
			Branch:       "agent/ORN-7-x",
			Policy:       agent.NewToolPolicy(nodes.AgentToolPolicy{Allow: []string{"catalog_affected"}, Deny: []string{"*"}}),
			Inputs:       q,
			Observe:      srv.Observe,
			ObserveDelta: srv.ObserveDelta,
		})
		if err != nil {
			t.Errorf("run: %v", err)
		}
		srv.Close("terminal")
		resCh <- res
	}()
	return srv, q, resCh, store
}

func TestInteractiveLifecycleThroughHeads(t *testing.T) {
	srv, _, resCh, store := startInteractive(t, "as_attach1")

	h1, err := srv.Attach(-1, "tui", "usr_alice")
	if err != nil {
		t.Fatal(err)
	}
	r1 := newHeadReader(h1)

	// Attach = hello, replay, live.
	hello := r1.waitFor(t, "hello", func(f Frame) bool { return f.T == THello })
	if hello.SessionID != "as_attach1" || hello.RunKind != "interactive" {
		t.Fatalf("hello = %+v", hello)
	}
	r1.waitFor(t, "live marker", func(f Frame) bool { return f.T == TLive })

	// Steer: the ack is synchronous; the log echoes an attributed
	// message_user at injection, then the stub streams a delta and echoes.
	if err := h1.Steer("hello agent"); err != nil {
		t.Fatalf("steer: %v", err)
	}
	mu := r1.waitFor(t, "message_user", eventKind("message_user"))
	if mu.Payload["text"] != "hello agent" || mu.Payload["principal"] != "usr_alice" {
		t.Fatalf("message_user = %+v", mu.Payload)
	}
	r1.waitFor(t, "delta", func(f Frame) bool { return f.T == TDelta })
	r1.waitFor(t, "agent echo", func(f Frame) bool {
		return f.T == TEvent && f.Kind == "message_agent" && f.Payload["text"] == "echo: hello agent"
	})

	// Gated tool: approval_requested renders on every head; a SECOND head
	// attaches mid-run (replay catches it up) and answers first.
	if err := h1.Steer("/ask contract_propose"); err != nil {
		t.Fatal(err)
	}
	ask := r1.waitFor(t, "approval_requested", eventKind("approval_requested"))
	reqID, _ := ask.Payload["requestId"].(string)
	if reqID == "" {
		t.Fatalf("approval without requestId: %+v", ask.Payload)
	}

	h2, err := srv.Attach(-1, "console", "usr_bob")
	if err != nil {
		t.Fatal(err)
	}
	r2 := newHeadReader(h2)
	replayed := r2.waitFor(t, "replayed ask", eventKind("approval_requested"))
	if replayed.Payload["requestId"] != reqID {
		t.Fatalf("replay lost the pending ask: %+v", replayed.Payload)
	}

	if err := h2.Verdict(reqID, true, "lgtm"); err != nil {
		t.Fatalf("verdict: %v", err)
	}
	res1 := r1.waitFor(t, "approval_resolved", eventKind("approval_resolved"))
	if res1.Payload["approved"] != true || res1.Payload["principal"] != "usr_bob" {
		t.Fatalf("resolution not attributed: %+v", res1.Payload)
	}
	// First valid verdict won; a late verdict is a no-op ack.
	if err := h1.Verdict(reqID, false, "too late"); !errors.Is(err, agent.ErrNotPending) {
		t.Fatalf("late verdict = %v, want ErrNotPending", err)
	}
	r1.waitFor(t, "verdict outcome message", func(f Frame) bool {
		return f.T == TEvent && f.Kind == "message_agent" && f.Payload["text"] == "contract_propose approved"
	})

	// Interrupt stops the turn, not the session.
	if err := h1.Interrupt(); err != nil {
		t.Fatal(err)
	}
	r1.waitFor(t, "interrupt logged", func(f Frame) bool {
		return f.T == TEvent && f.Kind == "harness_event" && f.Payload["phase"] == "interrupted"
	})

	// Detach h2: the session continues; h1 keeps steering.
	h2.Detach()
	if err := h1.Steer("/done"); err != nil {
		t.Fatal(err)
	}

	res := <-resCh
	if res.Outcome.Status != "completed" {
		t.Fatalf("outcome = %+v", res.Outcome)
	}
	// The feed drains to bye{terminal}, then EOF.
	r1.waitFor(t, "bye", func(f Frame) bool { return f.T == TBye })

	// The sealed segments carry the conversation: attributed user turns and
	// the attributed resolution — replay reproduces the whole exchange.
	ctxbg := context.Background()
	var sawUser, sawResolved bool
	for _, segID := range res.Segments {
		_, body, err := store.Get(ctxbg, objectstore.ObjectID(segID))
		if err != nil {
			t.Fatal(err)
		}
		seg, err := nodes.Decode[nodes.AgentSessionSegment](body)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range seg.Entries {
			if e.Kind == nodes.SessionEventMessageUser && e.Payload["principal"] == "usr_alice" {
				sawUser = true
			}
			if e.Kind == nodes.SessionEventApprovalResolved && e.Payload["principal"] == "usr_bob" {
				sawResolved = true
			}
		}
	}
	if !sawUser || !sawResolved {
		t.Fatalf("sealed log missing the conversation: user=%v resolved=%v", sawUser, sawResolved)
	}

	// After the terminal, every input refuses.
	if err := h1.Steer("late"); !errors.Is(err, agent.ErrSessionDone) {
		t.Fatalf("post-terminal steer = %v, want ErrSessionDone", err)
	}
}

func TestAttachFromCursorReplaysOnlyTail(t *testing.T) {
	srv := NewServer(SessionInfo{SessionID: "as_cursor"}, nil)
	for i := 0; i < 10; i++ {
		srv.Observe(agent.SessionEvent{Seq: i, Kind: "message_agent", Payload: map[string]any{"text": "t"}})
	}
	h, err := srv.Attach(5, "cli", "usr_cli")
	if err != nil {
		t.Fatal(err)
	}
	r := newHeadReader(h)
	hello := r.waitFor(t, "hello", func(f Frame) bool { return f.T == THello })
	if hello.LatestSeq == nil || *hello.LatestSeq != 9 {
		t.Fatalf("latestSeq = %v", hello.LatestSeq)
	}
	first := r.waitFor(t, "first replayed event", func(f Frame) bool { return f.T == TEvent })
	if *first.Seq != 6 {
		t.Fatalf("replay started at %d, want 6", *first.Seq)
	}
	live := r.waitFor(t, "live", func(f Frame) bool { return f.T == TLive })
	if *live.FromSeq != 5 {
		t.Fatalf("live fromSeq = %d", *live.FromSeq)
	}
}

func TestLaggedHeadIsDisconnectedDeltasShedFirst(t *testing.T) {
	srv := NewServer(SessionInfo{SessionID: "as_lag"}, nil)
	h, err := srv.Attach(-1, "tui", "usr_slow")
	if err != nil {
		t.Fatal(err)
	}
	// Nobody reads. Deltas shed past the soft limit…
	for i := 0; i < softLimit+100; i++ {
		srv.ObserveDelta(driver.Event{Kind: driver.EventDelta, Text: "x"})
	}
	// …and sealed events past the hard limit disconnect the head with a bye.
	for i := 0; i < hardLimit+10; i++ {
		srv.Observe(agent.SessionEvent{Seq: i, Kind: "message_agent"})
	}
	var last Frame
	n := 0
	for {
		f, ok := h.TryRecv()
		if !ok {
			break
		}
		last = f
		n++
	}
	if last.T != TBye || last.Reason != ReasonLagged {
		t.Fatalf("last frame = %+v, want bye{lagged}", last)
	}
	if n > hardLimit+2 {
		t.Fatalf("queue was not bounded: %d frames", n)
	}
	if _, ok := h.Recv(); ok {
		t.Fatal("head must be closed after lag disconnect")
	}
}

func TestVersionRefusalIsLoud(t *testing.T) {
	srv := NewServer(SessionInfo{SessionID: "as_ver"}, nil)
	h, err := srv.Attach(-1, "tui", "usr_x")
	if err != nil {
		t.Fatal(err)
	}
	r := newHeadReader(h)
	r.waitFor(t, "live", func(f Frame) bool { return f.T == TLive })
	h.Submit(Frame{V: 99, T: TSteer, Text: "hi", Ref: "r1"})
	errF := r.waitFor(t, "version error", func(f Frame) bool { return f.T == TError })
	if errF.Code != CodeVersion {
		t.Fatalf("code = %q", errF.Code)
	}
	select {
	case <-r.done:
	case <-time.After(5 * time.Second):
		t.Fatal("head not closed after version refusal")
	}
}

func TestWatchOnlyServerRefusesInputs(t *testing.T) {
	srv := NewServer(SessionInfo{SessionID: "as_watch"}, nil)
	h, err := srv.Attach(-1, "console", "usr_x")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Steer("nope"); !errors.Is(err, agent.ErrSessionDone) {
		t.Fatalf("steer on watch-only = %v", err)
	}
	r := newHeadReader(h)
	h.Submit(SteerFrame("r1", "nope"))
	ack := r.waitFor(t, "refusal ack", func(f Frame) bool { return f.T == TAck })
	if ack.OK == nil || *ack.OK || ack.Reason != ReasonTerminal {
		t.Fatalf("ack = %+v", ack)
	}
}

func TestClosedServerRefusesAttach(t *testing.T) {
	srv := NewServer(SessionInfo{SessionID: "as_closed"}, nil)
	srv.Close("terminal")
	if _, err := srv.Attach(-1, "tui", "usr_x"); !errors.Is(err, ErrClosed) {
		t.Fatalf("attach after close = %v", err)
	}
}
