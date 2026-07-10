package attach

import (
	"bytes"
	"context"
	"encoding/json"
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
}

func (c RelayConfig) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

// ServeToRelay bridges a live session's attach Server to the cloud relay until
// ctx is canceled or the session ends. The Server is the body's single
// in-process head sink; ServeToRelay attaches to it, batches its event feed
// upstream, streams deltas, and pumps the input return-queue into inputs.
// Blocking; run it in the serve command's foreground.
func ServeToRelay(ctx context.Context, srv *Server, inputs *agent.InputQueue, cfg RelayConfig) error {
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
		return err
	}
	defer head.Detach()

	errc := make(chan error, 2)
	go func() { errc <- pumpUp(ctx, head, cfg, flush) }()
	go func() { errc <- pumpDown(ctx, inputs, cfg, poll) }()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errc:
		return err
	}
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
		return fmt.Errorf("attach relay: POST %s: %s", path, resp.Status)
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
