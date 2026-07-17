// relay_ws.go — the attach binding v2 (orun-agents-native AN0, the transport
// swap attach-protocol.md §6.3 reserved): the body's chatty relay legs ride
// ONE outbound WebSocket. Down the socket come head input frames
// (steer/verdict/interrupt/end) at push latency — no poll interval in the
// path; up the socket go the body's acks (inline) and best-effort deltas.
//
// The durable event batches deliberately KEEP the confirmed POST /events
// carriage: durability needs delivery confirmation plus retry-and-drop-loudly
// semantics, the frame vocabulary is frozen (a batch-ack frame would be a
// protocol revision, which AN0 is not), and an HTTP response is exactly that
// confirmation. So the socket replaces the legs where latency and chatter
// live — the long-poll and the per-delta POST — and the log keeps the leg
// where durability lives. See specs/orun-agents-native/README.md (amended
// decision 1a).
//
// Locks honored: frames are frozen (the socket carries attach-v1 frames,
// NDJSON-per-message, byte-identical to the HTTP bodies); HTTP stays (dial WS,
// fall back to the long-poll on refusal or mid-session drop — a body never
// strands on transport); same auth, same lease (the dial presents the live
// session bearer, re-dials on rotation, and a refused bearer degrades to the
// HTTP loop whose own loud-failure posture already covers kill/runaway).

package attach

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/sourceplane/orun/internal/agent"
)

// wireSuffix is the body-facing WS route beside the HTTP legs. The cloud
// relay upgrades it behind the same three-way session gate as /events.
const wireSuffix = "/wire"

// wsWire is the shared socket handle: pumpDown owns dial/read/lifecycle;
// pumpUp borrows it for best-effort delta sends. nil-safe throughout.
type wsWire struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

// set installs (or clears) the live socket.
func (w *wsWire) set(c *websocket.Conn) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.conn = c
	w.mu.Unlock()
}

// send writes one attach-v1 frame as a single WS text message. Returns false
// when the wire is down (caller falls back to HTTP) or the write fails.
func (w *wsWire) send(ctx context.Context, f Frame) bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	c := w.conn
	w.mu.Unlock()
	if c == nil {
		return false
	}
	b, err := marshalFrame(f)
	if err != nil {
		return false
	}
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.Write(wctx, websocket.MessageText, b) == nil
}

func marshalFrame(f Frame) ([]byte, error) {
	var sb strings.Builder
	if err := WriteFrame(&sb, f); err != nil {
		return nil, err
	}
	// WriteFrame appends the NDJSON newline; the WS message boundary makes it
	// redundant but harmless — the bytes stay identical to every other
	// transport, which is the AN0 conformance property.
	return []byte(sb.String()), nil
}

// wireDialTimeout bounds the WS handshake; the session's liveness is the
// heartbeat's job, so a slow dial just falls back.
const wireDialTimeout = 15 * time.Second

// dialWire opens the body wire socket, presenting the live session bearer
// exactly as the HTTP legs do.
func dialWire(ctx context.Context, cfg RelayConfig) (*websocket.Conn, error) {
	dctx, cancel := context.WithTimeout(ctx, wireDialTimeout)
	defer cancel()
	hdr := http.Header{}
	if tok := cfg.authToken(); tok != "" {
		hdr.Set("Authorization", "Bearer "+tok)
	}
	// coder/websocket accepts http(s) URLs directly. The handshake client is
	// deliberately timeout-free (the dial context bounds it); an http.Client
	// timeout would sever the upgraded connection mid-session.
	conn, resp, err := websocket.Dial(dctx, cfg.BaseURL+wireSuffix, &websocket.DialOptions{
		HTTPHeader: hdr,
		HTTPClient: cfg.wsClient(),
	})
	if err != nil {
		if resp != nil {
			return nil, &relayHTTPError{Method: http.MethodGet, Path: wireSuffix, Status: resp.StatusCode, status: resp.Status}
		}
		return nil, err
	}
	return conn, nil
}

// wsClient returns the handshake client: the configured one if the caller set
// it (tests inject the fixture server's), else a fresh timeout-free client.
func (c RelayConfig) wsClient() *http.Client {
	if c.WSHTTP != nil {
		return c.WSHTTP
	}
	return &http.Client{}
}

// wireUnsupported reports a capability-absent refusal: the relay (or an
// intermediary) does not speak the wire at all, so this session stays on the
// HTTP binding — the binding that remains valid indefinitely (lock 2).
func wireUnsupported(err error) bool {
	var he *relayHTTPError
	if errors.As(err, &he) {
		switch he.Status {
		case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusUpgradeRequired, http.StatusNotImplemented, http.StatusBadRequest:
			return true
		}
	}
	return false
}

// wireOutcome says why a wire session ended.
type wireOutcome int

const (
	wireDropped wireOutcome = iota // transport drop → redial
	wireRotated                    // bearer rotated → redial with the new one
	wireBye                        // relay said bye → session over, stop pumps
	wireCtx                        // context canceled
)

// runWireSession serves one connected socket: input frames in → the runtime's
// InputQueue, acks back inline; pings answered; rotation watched. Returns why
// it ended. Never returns while healthy.
func runWireSession(ctx context.Context, conn *websocket.Conn, inputs *agent.InputQueue, cfg RelayConfig, wire *wsWire) wireOutcome {
	dialedToken := cfg.authToken()
	wire.set(conn)
	defer wire.set(nil)

	check := cfg.TokenCheckEvery
	if check <= 0 {
		check = 30 * time.Second
	}

	type readResult struct {
		frame Frame
		err   error
	}
	reads := make(chan readResult)
	rctx, rcancel := context.WithCancel(ctx)
	defer rcancel()
	go func() {
		defer close(reads)
		for {
			_, data, err := conn.Read(rctx)
			if err != nil {
				select {
				case reads <- readResult{err: err}:
				case <-rctx.Done():
				}
				return
			}
			f, perr := parseWireFrame(data)
			if perr != nil {
				cfg.logf("orun agent serve: wire: dropping malformed frame: %v\n", perr)
				continue
			}
			select {
			case reads <- readResult{frame: f}:
			case <-rctx.Done():
				return
			}
		}
	}()

	ticker := time.NewTicker(check)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = conn.Close(websocket.StatusNormalClosure, "context canceled")
			return wireCtx
		case <-ticker.C:
			if tok := cfg.authToken(); tok != dialedToken {
				cfg.logf("orun agent serve: wire: session token rotated — re-dialing\n")
				_ = conn.Close(websocket.StatusNormalClosure, "token rotated")
				return wireRotated
			}
		case r, ok := <-reads:
			if !ok || r.err != nil {
				return wireDropped
			}
			f := r.frame
			switch f.T {
			case TSteer, TVerdict, TInterrupt, TEnd:
				applyInputWire(ctx, inputs, f, cfg, wire)
			case TPing:
				_ = wire.send(ctx, PongFrame(f.At))
			case TBye:
				cfg.logf("orun agent serve: wire: relay said bye (%s)\n", f.Reason)
				_ = conn.Close(websocket.StatusNormalClosure, "bye")
				return wireBye
			default:
				// Unknown frame types MUST be ignored (forward compatibility).
			}
		}
	}
}

func parseWireFrame(data []byte) (Frame, error) {
	d := NewDecoder(strings.NewReader(string(data) + "\n"))
	return d.Next()
}

// applyInputWire routes one pushed head input into the runtime and acks it
// inline on the socket, falling back to the HTTP ack door if the socket
// write fails mid-flight (the console's blocking POST /input must resolve).
func applyInputWire(ctx context.Context, inputs *agent.InputQueue, f Frame, cfg RelayConfig, wire *wsWire) {
	var err error
	switch f.T {
	case TSteer:
		err = inputs.Steer(f.Text, principalOf(f))
	case TVerdict:
		approved := f.Approved != nil && *f.Approved
		err = inputs.Verdict(f.RequestID, approved, f.Reason, principalOf(f))
	case TInterrupt:
		err = inputs.Interrupt(principalOf(f))
	case TEnd:
		err = inputs.End(principalOf(f))
	default:
		return
	}
	ack := AckFrame(f.Ref, err == nil, ackReason(err))
	if !wire.send(ctx, ack) {
		_ = postJSON(ctx, cfg, "/inputs/ack", ack)
	}
}

// pumpDownWire is the preferred-with-fallback down pump (AN0): dial the wire;
// on capability absence stay on the HTTP long-poll for good; on transient
// failure or mid-session drop, run the HTTP loop for a window and re-probe.
// Frames lost to the seam are nobody's problem by construction: the relay
// re-pushes unacked inputs on reconnect and the long-poll cursor re-reads
// them; input refs make re-application idempotent at the queue.
func pumpDownWire(ctx context.Context, inputs *agent.InputQueue, cfg RelayConfig, poll time.Duration, wire *wsWire) (err error) {
	cfg.logf("orun agent serve: pumpDown started — wire %s%s (long-poll fallback armed)\n", cfg.BaseURL, wireSuffix)
	defer func() { cfg.logf("orun agent serve: pumpDown exited: %v\n", err) }()

	retryEvery := cfg.WSRetryEvery
	if retryEvery <= 0 {
		retryEvery = 30 * time.Second
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		conn, derr := dialWire(ctx, cfg)
		if derr != nil {
			if wireUnsupported(derr) {
				cfg.logf("orun agent serve: wire unsupported by relay (%v) — HTTP long-poll binding for this session\n", derr)
				return pumpDownPoll(ctx, inputs, cfg, poll)
			}
			cfg.logf("orun agent serve: wire dial failed (%v) — HTTP long-poll for %s, then re-probe\n", derr, retryEvery)
			if herr := pollWindow(ctx, inputs, cfg, poll, retryEvery); herr != nil {
				return herr
			}
			continue
		}
		cfg.logf("orun agent serve: wire connected — inputs now push-latency\n")
		switch runWireSession(ctx, conn, inputs, cfg, wire) {
		case wireCtx:
			return ctx.Err()
		case wireBye:
			return nil
		case wireRotated:
			continue // immediate redial with the fresh bearer
		case wireDropped:
			cfg.logf("orun agent serve: wire dropped — HTTP long-poll for %s, then re-dial\n", retryEvery)
			if herr := pollWindow(ctx, inputs, cfg, poll, retryEvery); herr != nil {
				return herr
			}
		}
	}
}

// pollWindow runs the HTTP long-poll loop for at most `window`, so steering
// keeps flowing while the wire is down. Returns nil on window expiry.
func pollWindow(ctx context.Context, inputs *agent.InputQueue, cfg RelayConfig, poll, window time.Duration) error {
	wctx, cancel := context.WithTimeout(ctx, window)
	defer cancel()
	err := pumpDownPoll(wctx, inputs, cfg, poll)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return nil // window over — caller re-probes the wire
	}
	return err
}
