package attach

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/sourceplane/orun/internal/agent"
)

// relay_head.go — the remote head (`orun agent attach as_…`): an SSE client
// over the cloud relay's head surface (attach-protocol.md §6.3). It mirrors
// the SocketClient contract — Frames() feed + sync-ack input methods — so the
// TUI head renders a Daytona session byte-identically to a local one. The
// transport swap is invisible above this seam (design §6.2).

// RelayHeadClient is a head attached to a session over the cloud relay.
type RelayHeadClient struct {
	baseURL string // …/agents/sessions/<id>
	token   string
	http    *http.Client

	frames chan Frame
	cancel context.CancelFunc

	mu      sync.Mutex
	refSeq  int
	closed  bool
}

// DialRelay attaches to a remote session: opens the SSE feed (GET
// {base}/attach?from=<cursor>) and returns a client whose Frames() streams
// hello → replay → live → live events. bearer is the caller's cliauth token;
// api-edge stamps the principal on inputs.
func DialRelay(ctx context.Context, baseURL, bearer string, from int, httpClient *http.Client) (*RelayHeadClient, error) {
	if httpClient == nil {
		httpClient = &http.Client{} // SSE: no client timeout
	}
	cctx, cancel := context.WithCancel(ctx)
	url := fmt.Sprintf("%s/attach?from=%d", baseURL, from)
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("accept", "text/event-stream")
	if bearer != "" {
		req.Header.Set("authorization", "Bearer "+bearer)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("attach relay: dial: %w", err)
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("attach relay: dial: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	c := &RelayHeadClient{baseURL: baseURL, token: bearer, http: httpClient,
		frames: make(chan Frame, 256), cancel: cancel}
	go c.readSSE(resp.Body)
	return c, nil
}

// readSSE parses the event-stream body: each `data:` line is one JSON frame.
func (c *RelayHeadClient) readSSE(body io.ReadCloser) {
	defer body.Close()
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue // skip event:/id:/comments/blank lines
		}
		payload := bytes.TrimSpace(line[len("data:"):])
		if len(payload) == 0 {
			continue
		}
		var f Frame
		if err := json.Unmarshal(payload, &f); err != nil {
			continue
		}
		select {
		case c.frames <- f:
		default:
			// The head is not draining; drop to protect the reader. A real
			// head always drains via the TUI loop.
		}
		if f.T == TBye {
			break
		}
	}
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	close(c.frames)
}

// Frames is the body→head feed. It closes when the SSE stream ends.
func (c *RelayHeadClient) Frames() <-chan Frame { return c.frames }

// Recv returns the next feed frame; ok=false when the stream is over.
func (c *RelayHeadClient) Recv() (Frame, bool) {
	f, ok := <-c.frames
	return f, ok
}

// input POSTs one head input frame and returns its ack outcome. The relay
// resolves the ack synchronously (it holds the body's return channel), so a
// non-2xx or an ack{ok:false} maps to the agent sentinels.
func (c *RelayHeadClient) input(build func(ref string) Frame) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return agent.ErrSessionDone
	}
	c.refSeq++
	ref := fmt.Sprintf("in-%d", c.refSeq)
	c.mu.Unlock()

	b, _ := json.Marshal(build(ref))
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/input", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	if c.token != "" {
		req.Header.Set("authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return agent.ErrSessionDone
	}
	var ack Frame
	if err := json.NewDecoder(resp.Body).Decode(&ack); err == nil && ack.T == TAck {
		if ack.OK != nil && !*ack.OK {
			if ack.Reason == ReasonNotPending {
				return agent.ErrNotPending
			}
			return agent.ErrSessionDone
		}
	}
	return nil
}

// Steer queues a user turn on the remote body.
func (c *RelayHeadClient) Steer(text string) error {
	return c.input(func(ref string) Frame { return SteerFrame(ref, text) })
}

// Verdict answers a pending approval request.
func (c *RelayHeadClient) Verdict(requestID string, approved bool, reason string) error {
	return c.input(func(ref string) Frame { return VerdictFrame(ref, requestID, approved, reason) })
}

// Interrupt stops the current turn.
func (c *RelayHeadClient) Interrupt() error {
	return c.input(func(ref string) Frame { return InterruptFrame(ref) })
}

// End requests the graceful terminal.
func (c *RelayHeadClient) End() error {
	return c.input(func(ref string) Frame { return EndFrame(ref) })
}

// Detach closes the SSE stream; the session continues.
func (c *RelayHeadClient) Detach() { c.cancel() }
