package attach

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// heartbeat.go — the session liveness contract (saas-agents AG6). This is the
// ONLY thing that keeps a cloud session alive:
//
//   POST {base}/heartbeat   Authorization: Bearer {token}   (no body)
//
// The first successful beat is the sole trigger that flips the session
// provisioning→running, stamps started_at, and sets the lease to now+15m; miss
// it and the control plane reclaims the box as lease_lost with no logs. POST
// /events (the event mirror) never touches the lease, and /stream, /inputs,
// /inputs/ack are not routes at all (cloud tracking issue #466) — so the body
// MUST heartbeat here or nothing else matters.
//
// Cadence is a fixed 5 min (lease TTL 15m, sweep grace 5m → ~20m hard deadline;
// the beat response does not echo the expiry, so we do not try to read it back).
// ORUN_SESSION_TOKEN has a 15-min TTL, so the loop also refreshes it via
// POST {base}/token → {token, expiresAt}; beating with a stale token 401s after
// 15 min. Beats continue through awaiting_approval (a non-terminal, still-swept
// state), so the loop never gates on whether the agent is currently working.
//
// Errors: 5xx/network → backoff+retry; 403/409/404 → terminal (a console kill is
// the cloud refusing to extend the lease — honor it, do not retry-storm); 401 →
// one token refresh, then terminal if it still fails.

const (
	defaultBeatInterval   = 5 * time.Minute
	defaultRefreshMargin  = 5 * time.Minute
	defaultFirstBeatTries = 6
)

// HeartbeatConfig configures the session heartbeat loop.
type HeartbeatConfig struct {
	BaseURL string // …/v1/organizations/<org>/agents/sessions/<id>
	Token   string // initial ORUN_SESSION_TOKEN
	HTTP    *http.Client
	Log     io.Writer

	Interval       time.Duration // beat cadence (default 5m)
	RefreshMargin  time.Duration // refresh the token when this close to expiry (default 5m)
	FirstBeatTries int           // 5xx/network retries for the first beat (default 6)
	RetryBackoff   time.Duration // base backoff for first-beat retries (default 500ms)
}

func (c HeartbeatConfig) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c HeartbeatConfig) logf(format string, args ...any) {
	if c.Log != nil {
		fmt.Fprintf(c.Log, format, args...)
	}
}

// Heartbeat owns the session lease. One goroutine sends beats and refreshes the
// token; expiry is touched only by that goroutine (plus the synchronous first
// beat before it starts). The token is guarded by mu because the relay reads it
// concurrently via Token() so its auth tracks refreshes instead of pinning the
// 15-min boot token.
type Heartbeat struct {
	cfg    HeartbeatConfig
	mu     sync.Mutex
	token  string
	expiry time.Time // best-known expiry of the current token; zero = unknown
}

// Token returns the current (possibly refreshed) session token. Safe for
// concurrent use — pass it as RelayConfig.TokenFn so the relay's bearer tracks
// the heartbeat's refreshes rather than expiring mid-run with the boot token.
func (h *Heartbeat) Token() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.token
}

func (h *Heartbeat) setToken(tok string) {
	h.mu.Lock()
	h.token = tok
	h.mu.Unlock()
}

// StartHeartbeat sends the first beat synchronously — the call that flips
// provisioning→running — and, on success, runs the beat+refresh loop in the
// background until ctx is canceled or the cloud ends the session. A failed first
// beat is returned loudly so serve can exit with a diagnostic instead of running
// an agent the cloud will silently reclaim. onTerminal fires at most once if the
// loop later hits a terminal state (a console kill, a lapsed lease) so serve can
// stop the agent and exit.
func StartHeartbeat(ctx context.Context, cfg HeartbeatConfig, onTerminal func(reason string)) (*Heartbeat, error) {
	h := &Heartbeat{cfg: cfg, token: cfg.Token}
	if err := h.firstBeat(ctx); err != nil {
		return nil, err
	}
	go h.run(ctx, onTerminal)
	return h, nil
}

func (h *Heartbeat) interval() time.Duration {
	if h.cfg.Interval > 0 {
		return h.cfg.Interval
	}
	return defaultBeatInterval
}

func (h *Heartbeat) refreshMargin() time.Duration {
	if h.cfg.RefreshMargin > 0 {
		return h.cfg.RefreshMargin
	}
	return defaultRefreshMargin
}

// firstBeat blocks until the first beat lands (lease created) or fails
// terminally. 5xx/network are retried with exponential backoff; a 401 triggers
// one token refresh and a retry; 403/409/404 are terminal immediately.
func (h *Heartbeat) firstBeat(ctx context.Context) error {
	tries := h.cfg.FirstBeatTries
	if tries <= 0 {
		tries = defaultFirstBeatTries
	}
	backoff := h.cfg.RetryBackoff
	if backoff <= 0 {
		backoff = 500 * time.Millisecond
	}
	var lastErr error
	for i := 0; i < tries; i++ {
		if i > 0 {
			h.cfg.logf("orun agent serve: heartbeat attempt %d/%d failed (%v); retrying in %s\n", i, tries, lastErr, backoff)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			if backoff < 8*time.Second {
				backoff *= 2
			}
		}
		status, err := h.postHeartbeat(ctx)
		if err == nil {
			h.cfg.logf("orun agent serve: first heartbeat landed — session running, lease held (now+15m)\n")
			return nil
		}
		lastErr = err
		switch {
		case isTerminalStatus(status):
			return fmt.Errorf("first heartbeat rejected with HTTP %d (terminal — the cloud is refusing this session): %w", status, err)
		case status == http.StatusUnauthorized:
			// The token-TTL race (Candidate 3): the env token expired during a
			// slow cold boot. One refresh, then terminal per the contract.
			if rerr := h.refresh(ctx); rerr != nil {
				return fmt.Errorf("first heartbeat 401 and token refresh failed — ORUN_SESSION_TOKEN expired before the first beat (cold boot outlasted its 15m TTL); the cloud must extend the TTL or speed the boot: %w", rerr)
			}
			status2, err2 := h.postHeartbeat(ctx)
			if err2 == nil {
				h.cfg.logf("orun agent serve: first heartbeat landed after token refresh — session running\n")
				return nil
			}
			if isTerminalStatus(status2) || status2 == http.StatusUnauthorized {
				return fmt.Errorf("first heartbeat still HTTP %d after token refresh: %w", status2, err2)
			}
			lastErr = err2 // 5xx after refresh — fall through to backoff retry
		default:
			// 5xx / network — retry.
		}
	}
	return fmt.Errorf("first heartbeat never landed after %d attempts (cloud unreachable or 5xx) — the session will be reclaimed as lease_lost: %w", tries, lastErr)
}

// run beats on a fixed cadence and refreshes the token before it expires, until
// ctx is canceled or a terminal state ends the session.
func (h *Heartbeat) run(ctx context.Context, onTerminal func(reason string)) {
	// Now that the lease exists, get a full-TTL token: the env token may already
	// be partway through its 15m and its exact expiry is unknown to us.
	if terminal, err := h.maybeRefresh(ctx, true); err != nil && terminal {
		h.terminate(onTerminal, err)
		return
	}
	ticker := time.NewTicker(h.interval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if terminal, err := h.maybeRefresh(ctx, false); err != nil {
				if terminal {
					h.terminate(onTerminal, err)
					return
				}
				h.cfg.logf("orun agent serve: token refresh transient failure (%v); keeping current token\n", err)
			}
			if terminal, err := h.beat(ctx); err != nil {
				if terminal {
					h.terminate(onTerminal, err)
					return
				}
				h.cfg.logf("orun agent serve: heartbeat transient failure (%v); retrying next cadence\n", err)
			}
		}
	}
}

func (h *Heartbeat) terminate(onTerminal func(reason string), err error) {
	h.cfg.logf("orun agent serve: heartbeat terminal — stopping session: %v\n", err)
	if onTerminal != nil {
		onTerminal(err.Error())
	}
}

// beat sends one maintenance beat, recovering a single 401 with a token refresh.
// terminal=true means the cloud has ended the session and the loop must stop.
func (h *Heartbeat) beat(ctx context.Context) (terminal bool, err error) {
	status, err := h.postHeartbeat(ctx)
	if err == nil {
		return false, nil
	}
	if isTerminalStatus(status) {
		return true, fmt.Errorf("heartbeat rejected with HTTP %d (terminal): %w", status, err)
	}
	if status == http.StatusUnauthorized {
		if rerr := h.refresh(ctx); rerr != nil {
			return true, fmt.Errorf("heartbeat 401 and token refresh failed: %w", rerr)
		}
		status2, err2 := h.postHeartbeat(ctx)
		if err2 == nil {
			return false, nil
		}
		if isTerminalStatus(status2) || status2 == http.StatusUnauthorized {
			return true, fmt.Errorf("heartbeat still HTTP %d after token refresh: %w", status2, err2)
		}
		return false, err2 // 5xx after refresh — transient
	}
	return false, err // 5xx / network — transient
}

// maybeRefresh refreshes the token when it is within RefreshMargin of expiry (or
// force=true). terminal=true means the refresh hit a terminal/auth status.
func (h *Heartbeat) maybeRefresh(ctx context.Context, force bool) (terminal bool, err error) {
	if !force && !h.expiry.IsZero() && time.Until(h.expiry) > h.refreshMargin() {
		return false, nil
	}
	if rerr := h.refresh(ctx); rerr != nil {
		return isTerminalErr(rerr), rerr
	}
	return false, nil
}

// refresh mints a fresh session token (POST /token, lease-gated) and updates the
// current token and its expiry.
func (h *Heartbeat) refresh(ctx context.Context) error {
	status, body, err := h.doPOST(ctx, "/token")
	if err != nil {
		return &httpStatusErr{status: status, err: fmt.Errorf("token refresh: %w", err)}
	}
	var tr struct {
		Token     string `json:"token"`
		ExpiresAt string `json:"expiresAt"`
	}
	if jerr := json.Unmarshal(body, &tr); jerr != nil || tr.Token == "" {
		return &httpStatusErr{status: status, err: fmt.Errorf("token refresh: malformed response")}
	}
	h.setToken(tr.Token)
	if t, perr := time.Parse(time.RFC3339, tr.ExpiresAt); perr == nil {
		h.expiry = t
	} else {
		// Unknown expiry: assume a full TTL from now so we refresh proactively
		// before the 15m window instead of on every beat.
		h.expiry = time.Now().Add(15 * time.Minute)
	}
	h.cfg.logf("orun agent serve: session token refreshed\n")
	return nil
}

func (h *Heartbeat) postHeartbeat(ctx context.Context) (int, error) {
	status, _, err := h.doPOST(ctx, "/heartbeat")
	return status, err
}

// doPOST issues an authenticated POST with no request body and returns the
// status code (0 on a transport error) and the response body.
func (h *Heartbeat) doPOST(ctx context.Context, path string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.cfg.BaseURL+path, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("authorization", "Bearer "+h.Token())
	resp, err := h.cfg.client().Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode >= 300 {
		return resp.StatusCode, body, fmt.Errorf("POST %s: %s", path, resp.Status)
	}
	return resp.StatusCode, body, nil
}

// isTerminalStatus reports whether a heartbeat/token status means the cloud has
// ended the session: 403/409 (lease refused/conflict — the console-kill path)
// and 404 (session gone). These must stop the loop, never retry-storm.
func isTerminalStatus(status int) bool {
	return status == http.StatusForbidden || status == http.StatusConflict || status == http.StatusNotFound
}

// httpStatusErr carries the status code of a failed heartbeat/token call so the
// proactive-refresh path can tell a terminal/auth rejection from a transient
// one.
type httpStatusErr struct {
	status int
	err    error
}

func (e *httpStatusErr) Error() string { return e.err.Error() }
func (e *httpStatusErr) Unwrap() error { return e.err }

func isTerminalErr(err error) bool {
	var se *httpStatusErr
	if errors.As(err, &se) {
		return isTerminalStatus(se.status) || se.status == http.StatusUnauthorized
	}
	return false // transport/network — transient
}
