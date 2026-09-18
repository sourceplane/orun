package ground

// The repo-token door client (GS1). A grounded session never receives a git
// credential: it mints one, per operation, from the control plane's
// session-authenticated door, scoped to exactly the repository it is bound to
// and cut to that link's live access tier (cloud design §4).
//
// Nothing here is durable. The only thing written to disk is an
// expiry-aware 0600 cache so a burst of git subprocesses does not mint a token
// each — and it is worthless the moment the token lapses (≤1h).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// tokenSkew is how long before real expiry a cached token is treated as spent.
// A token that expires mid-clone is worse than one minted a minute early.
const tokenSkew = 5 * time.Minute

// RepoToken is one minted credential. The token itself is never logged, never
// persisted outside the cache file, and never placed in a URL.
type RepoToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (t RepoToken) valid(now time.Time) bool {
	return t.Token != "" && t.ExpiresAt.After(now.Add(tokenSkew))
}

// SessionEnv is the identity a grounded sandbox carries. It is what addresses
// the door and what authenticates to it.
type SessionEnv struct {
	CloudAPI  string // ORUN_CLOUD_API
	OrgID     string // ORUN_ORG_ID (the workspace)
	SessionID string // ORUN_SESSION_ID
	Token     string // the ORUN_TOKEN_FILE contents, else ORUN_SESSION_TOKEN
	FullName  string // ORUN_REPO_FULL_NAME — the ONLY repo this session may mint for
}

// SessionFromEnv reads the session identity, preferring the rotation-carrying
// token file (GS2: serve writes every token the session holds to it) and
// falling back to ORUN_SESSION_TOKEN.
//
// THE FILE FIRST. ORUN_SESSION_TOKEN is the BOOT token: serve rotates the
// session's credential about every fifteen minutes and publishes each one to
// the file, but the environment keeps the first. Every process serve starts
// inherits both, so preferring the environment minted with a credential that
// had died a quarter-hour into the session — a build hours long asks for a
// repo token long after that.
func SessionFromEnv(getenv func(string) string) (SessionEnv, error) {
	s := SessionEnv{
		CloudAPI:  strings.TrimSpace(getenv("ORUN_CLOUD_API")),
		OrgID:     strings.TrimSpace(getenv("ORUN_ORG_ID")),
		SessionID: strings.TrimSpace(getenv("ORUN_SESSION_ID")),
		FullName:  strings.TrimSpace(getenv("ORUN_REPO_FULL_NAME")),
	}
	if path := strings.TrimSpace(getenv("ORUN_TOKEN_FILE")); path != "" {
		if b, err := os.ReadFile(path); err == nil {
			s.Token = strings.TrimSpace(string(b))
		}
	}
	if s.Token == "" {
		s.Token = strings.TrimSpace(getenv("ORUN_SESSION_TOKEN"))
	}
	var missing []string
	for _, f := range []struct {
		name  string
		value string
	}{
		{"ORUN_CLOUD_API", s.CloudAPI},
		{"ORUN_ORG_ID", s.OrgID},
		{"ORUN_SESSION_ID", s.SessionID},
		{"ORUN_SESSION_TOKEN", s.Token},
	} {
		if f.value == "" {
			missing = append(missing, f.name)
		}
	}
	if len(missing) > 0 {
		return s, fmt.Errorf("not a grounded session: %s unset", strings.Join(missing, ", "))
	}
	return s, nil
}

// ErrRefused is a door refusal the caller must not retry: the session went
// terminal, its lease lapsed, or a human lowered the repository's access tier.
// Git reports an ordinary auth failure; retrying would only repeat it.
var ErrRefused = errors.New("repo-token door refused")

// MintRepoToken calls the door and returns a fresh scoped credential.
//
// Retries only what is worth retrying: a 5xx or transport failure (the door
// itself is fine, the provider blipped). A 4xx is terminal by construction.
func MintRepoToken(ctx context.Context, hc *http.Client, s SessionEnv) (RepoToken, error) {
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	url := fmt.Sprintf("%s/v1/organizations/%s/agents/sessions/%s/repo-token",
		strings.TrimRight(s.CloudAPI, "/"), s.OrgID, s.SessionID)

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return RepoToken{}, ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte("{}")))
		if err != nil {
			return RepoToken{}, err
		}
		req.Header.Set("Authorization", "Bearer "+s.Token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := hc.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()

		switch {
		case resp.StatusCode == http.StatusOK:
			var env struct {
				Data struct {
					Token     string `json:"token"`
					ExpiresAt string `json:"expiresAt"`
				} `json:"data"`
			}
			if err := json.Unmarshal(body, &env); err != nil || env.Data.Token == "" {
				lastErr = fmt.Errorf("repo-token door: unreadable response")
				continue
			}
			exp, perr := time.Parse(time.RFC3339, env.Data.ExpiresAt)
			if perr != nil {
				// A token with an unreadable expiry is still usable now; treat
				// it as short-lived rather than discarding a working credential.
				exp = time.Now().Add(tokenSkew + time.Minute)
			}
			return RepoToken{Token: env.Data.Token, ExpiresAt: exp}, nil
		case resp.StatusCode >= 500:
			lastErr = fmt.Errorf("repo-token door: status %d", resp.StatusCode)
		default:
			// 401/403/404/409 — the answer will not change on a retry.
			return RepoToken{}, fmt.Errorf("%w: status %d", ErrRefused, resp.StatusCode)
		}
	}
	if lastErr == nil {
		lastErr = errors.New("repo-token door: unreachable")
	}
	return RepoToken{}, lastErr
}

// ── Cache ──────────────────────────────────────────────────────────────────

// CachePath is where a minted token rests between git invocations: runtime
// state, 0600, never the repo and never $HOME's durable config.
func CachePath(getenv func(string) string) string {
	dir := strings.TrimSpace(getenv("XDG_RUNTIME_DIR"))
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "orun", "repo-token.json")
}

// ReadCachedToken returns a cached token that is still comfortably valid.
func ReadCachedToken(path string, now time.Time) (RepoToken, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return RepoToken{}, false
	}
	var t RepoToken
	if err := json.Unmarshal(b, &t); err != nil || !t.valid(now) {
		return RepoToken{}, false
	}
	return t, true
}

// WriteCachedToken persists a minted token 0600. A failure here is not fatal —
// the caller already holds a working credential; the next git call just mints
// again.
func WriteCachedToken(path string, t RepoToken) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ClearCachedToken drops the cache — the one side effect `git credential
// erase` is honored for.
func ClearCachedToken(path string) error {
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ── The session token file (GS2) ───────────────────────────────────────────

// TokenFilePath is where serve publishes the session bearer for other `orun`
// processes in the sandbox: runtime state, 0600, never the repo.
func TokenFilePath(getenv func(string) string) string {
	if explicit := strings.TrimSpace(getenv("ORUN_TOKEN_FILE")); explicit != "" {
		return explicit
	}
	dir := strings.TrimSpace(getenv("XDG_RUNTIME_DIR"))
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "orun", "session-token")
}

// WriteSessionToken publishes the current session bearer 0600, atomically.
//
// Called on every rotation: the file is the CARRIER of rotation, which is why
// readers (remotestate.FileTokenSource) re-read per call rather than caching. A
// static env var would go stale in ~15 minutes; this does not.
func WriteSessionToken(path, token string) error {
	if strings.TrimSpace(token) == "" {
		return errors.New("refusing to write an empty session token")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(token+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// RepoTokenFromEnv is a GitHub token for the repository this sandbox session
// is grounded on: the cached one while it is comfortably valid, else a fresh
// mint from the platform (which is re-authorised against the link on every
// mint). An error means this process is not in a grounded session, or the
// platform refused.
//
// Cheap to call per request: a cache hit is one small file read, and a mint
// happens once per token lifetime — which is what lets a build that waits an
// hour on CI keep a credential that expires in one.
func RepoTokenFromEnv(ctx context.Context, getenv func(string) string) (string, error) {
	session, err := SessionFromEnv(getenv)
	if err != nil {
		return "", err
	}
	cachePath := CachePath(getenv)
	if tok, ok := ReadCachedToken(cachePath, time.Now()); ok {
		return tok.Token, nil
	}
	tok, err := MintRepoToken(ctx, nil, session)
	if err != nil {
		return "", err
	}
	// A working token in hand beats a cache.
	_ = WriteCachedToken(cachePath, tok)
	return tok.Token, nil
}
