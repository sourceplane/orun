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
	BaseURL string // e.g. https://api…/v1/organizations/<org>/agents/sessions/<id>
	Token   string // the session bearer (ORUN_SESSION_TOKEN) — the boot value
	// TokenFn, when set, supplies the live bearer for each request, overriding
	// Token. Wire it to Heartbeat.Token so the relay's auth tracks the token
	// refreshes and does not lapse ~15m in with the expired boot token.
	TokenFn func() string
	HTTP    *http.Client
	FlushEvery time.Duration // event-batch flush cadence (default 200ms)
	PollEvery  time.Duration // input long-poll spacing on empty (default 1s)

	// Write-path resilience. POST /events is the DURABLE console log (the cloud
	// DB the console polls via GET /events) — not a handshake and not liveness
	// (the heartbeat owns that). So event posting must never wedge the stream: a
	// failed batch is retried with backoff, then LOUDLY logged and dropped, and
	// the pump keeps running. One transient must not black out the whole session
	// the way a single returned error used to.
	PostRetries  int           // per-batch POST /events attempts before dropping (default 4)
	RetryBackoff time.Duration // base backoff between attempts (default 500ms)
	Log          io.Writer     // where write-path failures are reported (nil = discard)
}

func (c RelayConfig) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

// authToken is the bearer to present on a relay request: the live TokenFn value
// when available, else the static boot Token.
func (c RelayConfig) authToken() string {
	if c.TokenFn != nil {
		if t := c.TokenFn(); t != "" {
			return t
		}
	}
	return c.Token
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

// DialToRelay attaches to the body's Server and spawns the background event/
// input pumps. It does NOT gate on a synchronous dial-home: liveness and
// reachability are the heartbeat's job (already proven before serve runs the
// agent), and the /events write-path is the durable console log — it must keep
// trying for the whole session, not be disabled because one early POST failed.
// The pumps post events resiliently (retry + loud log + drop, never wedge) and
// drain the input return-queue. The only error returned is an attach failure.
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
			if err != nil && !errors.Is(err, context.Canceled) {
				cfg.logf("orun agent serve: relay pump ended: %v\n", err)
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

// pumpUp posts the runtime's DURABLE event frames to /events — the store the
// console polls. Only t:"event" frames go to the durable log; hello/live/bye and
// other attach-protocol control frames are NOT events and are skipped (posting
// them would write junk rows the console can't render). Deltas go to /stream
// (ephemeral, best-effort). A forwarding goroutine unblocks Recv so the flush
// ticker fires even when the frame stream is momentarily idle. Crucially, a POST
// failure NEVER returns from the pump: postEventBatch retries, logs loudly, then
// drops — one transient must not black out the rest of the session.
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
	send := func() {
		if len(batch) == 0 {
			return
		}
		postEventBatch(ctx, cfg, batch)
		batch = batch[:0]
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			send()
		case f, ok := <-frames:
			if !ok {
				send()
				return nil
			}
			switch f.T {
			case TEvent:
				batch = append(batch, f)
				if len(batch) >= 100 {
					send()
				}
			case TDelta:
				// Ephemeral live-token streaming: SSE-only on the cloud, never in
				// the durable poll model. Best-effort, never blocks the log.
				_ = postJSON(ctx, cfg, "/stream", f)
			case TBye:
				// Session over: flush the durable tail and stop. The terminal
				// state itself reaches the log as a state_changed event, not via
				// this control frame.
				send()
				return nil
			}
		}
	}
}

// postEventBatch POSTs a durable event batch to /events, retrying transient
// failures with backoff, then LOUDLY logging and dropping so the pump survives.
// The cloud dedupes by (sessionId, seq), so a retried batch is safe.
func postEventBatch(ctx context.Context, cfg RelayConfig, batch []Frame) {
	tries := cfg.PostRetries
	if tries <= 0 {
		tries = 4
	}
	backoff := cfg.RetryBackoff
	if backoff <= 0 {
		backoff = 500 * time.Millisecond
	}
	var lastErr error
	for i := 0; i < tries; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 8*time.Second {
				backoff *= 2
			}
		}
		if err := postJSON(ctx, cfg, "/events", batch); err == nil {
			return
		} else {
			lastErr = err
			cfg.logf("orun agent serve: POST /events failed (attempt %d/%d): %v\n", i+1, tries, err)
		}
	}
	cfg.logf("orun agent serve: POST /events dropping %d event(s) after %d failed attempts — console log will miss them: %v\n",
		len(batch), tries, lastErr)
}

// pumpDown long-polls the input return-queue (the cloud holds GET /inputs open
// ~25s) and feeds head inputs into the runtime, acking each so the console's
// POST /input (which blocks up to 25s for the ack) resolves promptly with
// ok:true. A cursor advances past consumed items. Persistent failures are
// logged LOUDLY (a wedged return-queue silently swallows every steer) rather
// than the old silent backoff.
func pumpDown(ctx context.Context, inputs *agent.InputQueue, cfg RelayConfig, poll time.Duration) error {
	cursor := 0
	consecutiveErr := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		items, next, err := getInputs(ctx, cfg, cursor)
		if err != nil {
			consecutiveErr++
			// Loud on the first failure and periodically after, so a wedged
			// input path (e.g. the route still 404ing) is visible instead of
			// silently eating the user's steers.
			if consecutiveErr == 1 || consecutiveErr%10 == 0 {
				cfg.logf("orun agent serve: GET /inputs failing (x%d) — steering degraded until it recovers: %v\n", consecutiveErr, err)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(poll):
			}
			continue
		}
		consecutiveErr = 0
		if len(items) == 0 {
			// The server's long-poll window elapsed empty; re-poll immediately
			// (the ~25s block IS the spacing), but guard a misconfigured instant
			// return with a small floor so we don't hot-loop.
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
	if tok := cfg.authToken(); tok != "" {
		req.Header.Set("authorization", "Bearer "+tok)
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
	if tok := cfg.authToken(); tok != "" {
		req.Header.Set("authorization", "Bearer "+tok)
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
