package attach

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sourceplane/orun/internal/agent"
)

// relay.go — the cloud transport (attach-protocol.md §6.3): the body
// (`orun agent serve`) dials OUT to the per-session Durable Object relay
// (NAT-safe, the shipped posture). Same frames as the socket; only the
// carriage differs. Two directions:
//
//   up   — event batches POST /events (the shipped ingest route, now
//          frame-shaped) + best-effort delta POST /stream.
//   down — the return queue: long-poll GET /inputs?cursor=N for head input
//          frames (steer/verdict/interrupt/end), fed into the runtime's
//          InputQueue; acks post back on /inputs/ack.
//
// The head side of the relay (SSE /attach + POST /input) lands in cloud AL6;
// the RelayHeadClient below is the terminal-side remote head that consumes it
// (`orun agent attach as_…`).

// RelayConfig points the body at its session relay.
type RelayConfig struct {
	BaseURL    string // e.g. https://api…/v1/organizations/<org>/agents/sessions/<id>
	Token      string // the session bearer (ORUN_SESSION_TOKEN)
	HTTP       *http.Client
	FlushEvery time.Duration // event-batch flush cadence (default 200ms)
	PollEvery  time.Duration // input long-poll spacing on empty (default 1s)

	// Dial-home hardening. The FIRST POST /events is the box's first contact
	// with the cloud: it proves the relay URL resolves, the network is up, and
	// the session token is accepted. A cold snapshot pull + install can outrun a
	// transient relay/token-propagation window, so it is retried; a persistent
	// failure is reported LOUDLY rather than swallowed, so a session that can't
	// dial home fails where someone can read it instead of silently.
	// NOTE: this is the /events attach channel — it is NOT the session
	// /heartbeat endpoint the control plane uses to flip provisioning→running.
	// A green dial-home here does not by itself create the lease.
	DialRetries  int           // first /events dial-home attempts before giving up (default 6)
	RetryBackoff time.Duration // base backoff between attempts (default 500ms)
	Log          io.Writer     // where a mid-session pump failure is reported (nil = discard)
}

func (c RelayConfig) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func (c RelayConfig) logf(format string, args ...any) {
	if c.Log != nil {
		fmt.Fprintf(c.Log, format, args...)
	}
}

// RelaySession is a live body↔relay bridge whose first heartbeat has already
// landed (the cloud lease exists). Its pumps run in the background until Close
// or ctx cancellation.
type RelaySession struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// Close stops the pumps and waits for them to drain (so a sealed bye posted on
// teardown makes it out before the process exits). Idempotent.
func (s *RelaySession) Close() {
	if s == nil {
		return
	}
	s.cancel()
	<-s.done
}

// Done is closed when the pumps have exited on their own (a fatal relay error
// or the session's bye). Lets the caller observe an unexpected relay drop.
func (s *RelaySession) Done() <-chan struct{} { return s.done }

// DialToRelay attaches to the body's Server, performs the first /events
// dial-home SYNCHRONOUSLY (with retry/backoff), and only on success spawns the
// background event/input pumps. It returns an error — loudly, not swallowed —
// when the dial-home never lands, so `orun agent serve` can exit non-zero with
// a diagnostic instead of running a local agent whose output the cloud never
// sees. This turns the silent 30-min `lease_lost` into a readable failure: the
// dial-home either provably reaches the cloud (URL resolves, token accepted) or
// fails where someone can read it.
func DialToRelay(ctx context.Context, srv *Server, inputs *agent.InputQueue, cfg RelayConfig) (*RelaySession, error) {
	flush := cfg.FlushEvery
	if flush <= 0 {
		flush = 200 * time.Millisecond
	}
	poll := cfg.PollEvery
	if poll <= 0 {
		poll = time.Second
	}
	head, err := srv.Attach(-1, "relay", "cloud")
	if err != nil {
		return nil, err
	}

	// The first dial-home: the catch-up frames enqueued at Attach (hello, any
	// replayed events, the live marker) are the initial batch. Drain and POST
	// them with retry — this proves reachability and that the token is accepted.
	initial, err := drainInitial(head)
	if err != nil {
		head.Detach()
		return nil, err
	}
	if err := postFirstDialHome(ctx, cfg, initial); err != nil {
		head.Detach()
		return nil, err
	}

	pctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer head.Detach()
		errc := make(chan error, 2)
		go func() { errc <- pumpUp(pctx, head, cfg, flush) }()
		go func() { errc <- pumpDown(pctx, inputs, cfg, poll) }()
		select {
		case <-pctx.Done():
		case err := <-errc:
			// A pump that dies mid-session (the relay became unreachable, the
			// token was revoked) is not fatal to the local run, but it must not
			// be silent — the cloud is now blind to this session.
			if err != nil && !errors.Is(err, context.Canceled) {
				cfg.logf("orun agent serve: relay pump ended, cloud is no longer receiving this session: %v\n", err)
			}
		}
	}()
	return &RelaySession{cancel: cancel, done: done}, nil
}

// ServeToRelay bridges a live session's attach Server to the cloud relay until
// ctx is canceled or the session ends: it dials home (first heartbeat, with
// retry) and then runs the pumps to completion. Blocking. Returns an error if
// the first heartbeat never lands. Kept as the one-call form for callers that
// don't need the dial-home result before proceeding.
func ServeToRelay(ctx context.Context, srv *Server, inputs *agent.InputQueue, cfg RelayConfig) error {
	sess, err := DialToRelay(ctx, srv, inputs, cfg)
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		sess.Close()
		return ctx.Err()
	case <-sess.Done():
		return nil
	}
}

// drainInitial pulls the catch-up frames Attach enqueued — hello, any replayed
// events, up to and including the live marker — into the first dial-home batch.
// Those frames are all queued before Attach returns, so this does not block on
// the live agent; it stops at the live marker (or a bye / closed queue).
func drainInitial(head *HeadConn) ([]Frame, error) {
	var batch []Frame
	for {
		f, ok := head.Recv()
		if !ok {
			// The session ended before it even went live. Send what we have so
			// the cloud still records the session rather than timing out blind.
			return batch, nil
		}
		batch = append(batch, f)
		if f.T == TLive || f.T == TBye {
			return batch, nil
		}
	}
}

// postFirstDialHome POSTs the initial batch to /events, retrying transient
// failures with exponential backoff. A cold box (snapshot pull + orun install)
// can momentarily outrun the relay being reachable or the session token being
// active at the DO, so a few retries turn a race into a success. A persistent
// failure returns a diagnostic that names the likely cause — an expired token,
// no reachability — instead of a bare status line.
func postFirstDialHome(ctx context.Context, cfg RelayConfig, batch []Frame) error {
	attempts := cfg.DialRetries
	if attempts <= 0 {
		attempts = 6
	}
	backoff := cfg.RetryBackoff
	if backoff <= 0 {
		backoff = 500 * time.Millisecond
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			cfg.logf("orun agent serve: dial-home attempt %d/%d failed (%v); retrying in %s\n",
				i, attempts, lastErr, backoff)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			if backoff < 8*time.Second {
				backoff *= 2
			}
		}
		if err := postJSON(ctx, cfg, "/events", batch); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	var httpErr *relayHTTPError
	if errors.As(lastErr, &httpErr) && (httpErr.Status == http.StatusUnauthorized || httpErr.Status == http.StatusForbidden) {
		return fmt.Errorf("dial-home (POST /events) rejected with HTTP %d after %d attempts — the session token (ORUN_SESSION_TOKEN) is invalid or expired; a cold boot can outlast its short TTL, in which case the cloud must mint a fresh token or extend the TTL: %w",
			httpErr.Status, attempts, lastErr)
	}
	return fmt.Errorf("dial-home (POST /events) never landed after %d attempts (relay %s unreachable or rejecting) — the cloud will see no event stream from this session: %w",
		attempts, cfg.BaseURL, lastErr)
}

// pumpUp batches the head's event frames to /events and forwards deltas to
// /stream. A bye ends the pump (the session is over). A forwarding goroutine
// unblocks the Recv loop so the flush ticker fires even when the frame stream
// is momentarily idle (a settled batch must still flush).
func pumpUp(ctx context.Context, head *HeadConn, cfg RelayConfig, flush time.Duration) error {
	ticker := time.NewTicker(flush)
	defer ticker.Stop()

	frames := make(chan Frame, 64)
	go func() {
		defer close(frames)
		for {
			f, ok := head.Recv()
			if !ok {
				return
			}
			select {
			case frames <- f:
			case <-ctx.Done():
				return
			}
		}
	}()

	var batch []Frame
	send := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := postJSON(ctx, cfg, "/events", batch); err != nil {
			return err
		}
		batch = batch[:0]
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := send(); err != nil {
				return err
			}
		case f, ok := <-frames:
			if !ok {
				return send()
			}
			switch f.T {
			case TEvent, THello, TLive:
				batch = append(batch, f)
				if len(batch) >= 100 {
					if err := send(); err != nil {
						return err
					}
				}
			case TDelta:
				_ = postJSON(ctx, cfg, "/stream", f)
			case TBye:
				// Flush the sealed tail, then forward the bye so the relay can
				// close its head feeds (the session is over).
				if err := send(); err != nil {
					return err
				}
				_ = postJSON(ctx, cfg, "/events", []Frame{f})
				return nil
			}
		}
	}
}

// pumpDown long-polls the input return-queue and feeds head inputs into the
// runtime. A cursor advances past consumed items; acks post back so a head's
// SocketClient-style sync wait resolves.
func pumpDown(ctx context.Context, inputs *agent.InputQueue, cfg RelayConfig, poll time.Duration) error {
	cursor := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		items, next, err := getInputs(ctx, cfg, cursor)
		if err != nil {
			// Transient relay hiccup: back off, keep the cursor.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(poll):
			}
			continue
		}
		if len(items) == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(poll):
			}
			continue
		}
		for _, f := range items {
			applyInput(inputs, f, cfg)
		}
		cursor = next
	}
}

// applyInput routes one head input frame into the runtime and posts its ack.
func applyInput(inputs *agent.InputQueue, f Frame, cfg RelayConfig) {
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
	_ = postJSON(context.Background(), cfg, "/inputs/ack", ack)
}

// principalOf reads the edge-stamped principal from an input frame envelope
// (api-edge sets it; a body never trusts a self-declared one — but on the
// relay path the relay is the trust boundary, so we read what it forwarded).
func principalOf(f Frame) string {
	if p, ok := f.Payload["principal"].(string); ok && p != "" {
		return p
	}
	return "cloud"
}

func ackReason(err error) string {
	switch {
	case err == nil:
		return ""
	case err == agent.ErrNotPending:
		return ReasonNotPending
	default:
		return ReasonTerminal
	}
}

// relayHTTPError carries the status code of a non-2xx relay response so callers
// can distinguish an auth rejection (401/403 — a token problem) from a
// transport or 5xx failure and report the right diagnostic.
type relayHTTPError struct {
	Method string
	Path   string
	Status int
	status string
}

func (e *relayHTTPError) Error() string {
	return fmt.Sprintf("attach relay: %s %s: %s", e.Method, e.Path, e.status)
}

func postJSON(ctx context.Context, cfg RelayConfig, path string, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	if cfg.Token != "" {
		req.Header.Set("authorization", "Bearer "+cfg.Token)
	}
	resp, err := cfg.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		return &relayHTTPError{Method: http.MethodPost, Path: path, Status: resp.StatusCode, status: resp.Status}
	}
	return nil
}

type inputsResponse struct {
	Items  []Frame `json:"items"`
	Cursor int     `json:"cursor"`
}

func getInputs(ctx context.Context, cfg RelayConfig, cursor int) ([]Frame, int, error) {
	url := fmt.Sprintf("%s/inputs?cursor=%d", cfg.BaseURL, cursor)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, cursor, err
	}
	if cfg.Token != "" {
		req.Header.Set("authorization", "Bearer "+cfg.Token)
	}
	resp, err := cfg.client().Do(req)
	if err != nil {
		return nil, cursor, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		io.Copy(io.Discard, resp.Body)
		return nil, cursor, fmt.Errorf("attach relay: GET inputs: %s", resp.Status)
	}
	var out inputsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, cursor, err
	}
	return out.Items, out.Cursor, nil
}
