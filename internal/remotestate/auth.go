// Package remotestate provides the HTTP client, token resolution, plan
// conversion, and run-ID derivation for orun-backend remote state.
package remotestate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/sourceplane/orun/internal/cliauth"
)

const (
	// refreshSkew refreshes the access token this long before its expiry, so a
	// command (or a long-running `run`) doesn't start work with a token that
	// expires mid-flight. The access token is ~15m; a 60s skew refreshes in the
	// final ~7% of its life — frequent enough to avoid mid-request expiry,
	// infrequent enough not to churn the rotating refresh token.
	refreshSkew = 60 * time.Second

	// refreshLockTimeout bounds how long we wait for the cross-process refresh
	// lock when the caller's context has no deadline of its own.
	refreshLockTimeout = 30 * time.Second
)

// TokenSource resolves the bearer token for backend requests.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// OIDCTokenSource requests a GitHub Actions OIDC token from the GitHub
// Actions token endpoint with the configured audience.
type OIDCTokenSource struct {
	Audience   string
	httpClient *http.Client
}

// NewOIDCTokenSource returns an OIDCTokenSource using the given audience
// (default "orun" if empty).
func NewOIDCTokenSource(audience string) *OIDCTokenSource {
	if audience == "" {
		audience = "orun"
	}
	return &OIDCTokenSource{
		Audience:   audience,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Token fetches a fresh OIDC token from the GitHub Actions token endpoint.
func (o *OIDCTokenSource) Token(ctx context.Context) (string, error) {
	requestURL := os.Getenv("ACTIONS_ID_TOKEN_REQUEST_URL")
	requestToken := os.Getenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN")
	if requestURL == "" || requestToken == "" {
		return "", fmt.Errorf(
			"GitHub Actions OIDC token not available: " +
				"ACTIONS_ID_TOKEN_REQUEST_URL and ACTIONS_ID_TOKEN_REQUEST_TOKEN must be set; " +
				"add `id-token: write` to your workflow permissions")
	}

	u, err := url.Parse(requestURL)
	if err != nil {
		return "", fmt.Errorf("invalid ACTIONS_ID_TOKEN_REQUEST_URL: %w", err)
	}
	q := u.Query()
	q.Set("audience", o.Audience)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("building OIDC token request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+requestToken)
	req.Header.Set("Accept", "application/json")

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("requesting OIDC token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OIDC token request returned status %d", resp.StatusCode)
	}

	var result struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding OIDC token response: %w", err)
	}
	if result.Value == "" {
		return "", fmt.Errorf("OIDC token response missing value field")
	}
	return result.Value, nil
}

// StaticTokenSource returns a pre-configured bearer token as-is.
type StaticTokenSource struct {
	token string
}

// NewStaticTokenSource wraps a fixed bearer token (e.g. from ORUN_TOKEN).
func NewStaticTokenSource(token string) *StaticTokenSource {
	return &StaticTokenSource{token: token}
}

// Token returns the static token or an error if it is empty.
func (s *StaticTokenSource) Token(_ context.Context) (string, error) {
	if s.token == "" {
		return "", fmt.Errorf("ORUN_TOKEN is not set")
	}
	return s.token, nil
}

// SessionTokenSource resolves and refreshes a local Orun CLI session token.
type SessionTokenSource struct {
	BackendURL string
	Version    string
}

// refreshGroup coalesces concurrent refreshes within a single process: when
// many goroutines (e.g. a `run` firing parallel state requests) hit an expired
// access token at once, exactly one performs the rotating-refresh redemption
// and the rest share its result. Keyed by backend URL. The cross-process race
// (two orun invocations) is handled by the file lock in refreshOnce.
var refreshGroup singleflight.Group

// Token returns the current access token, refreshing it if needed. The refresh
// token is rotating and single-use: RefreshSession persists the new one. If the
// platform reports the token family revoked (single-use reuse, or a console-side
// revoke), RefreshSession clears the local session and returns
// cliauth.ErrSessionRevoked; this surfaces it as a single actionable error
// ("run `orun auth login`") rather than per-call spam.
//
// Concurrency: a rotating, single-use refresh token MUST be redeemed exactly
// once. Token serializes refresh across goroutines (singleflight) and across
// processes (an advisory file lock in refreshOnce), and re-checks the stored
// token after winning the lock, so siblings reuse a freshly rotated token
// instead of replaying a spent one (which would trip reuse-detection and revoke
// the whole session).
func (s *SessionTokenSource) Token(ctx context.Context) (string, error) {
	creds, err := cliauth.LoadSession()
	if err != nil {
		return "", err
	}
	if creds == nil {
		return "", os.ErrNotExist
	}
	if creds.AccessToken != "" && !tokenExpired(creds.AccessExpiryTime()) {
		return creds.AccessToken, nil
	}
	if strings.TrimSpace(creds.RefreshToken) == "" {
		return "", cliauth.ErrSessionRevoked
	}

	token, err, _ := refreshGroup.Do(s.BackendURL, func() (interface{}, error) {
		return s.refreshOnce(ctx)
	})
	if err != nil {
		return "", err
	}
	return token.(string), nil
}

// refreshOnce performs a single, serialized refresh. It takes the cross-process
// lock, then re-loads the session and re-checks expiry: a sibling process may
// have rotated the token while we waited, in which case we return its fresh
// access token without redeeming our (now-spent) refresh token. The lock is
// best-effort — if it cannot be acquired we still refresh, just unserialized.
func (s *SessionTokenSource) refreshOnce(ctx context.Context) (string, error) {
	lockCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		lockCtx, cancel = context.WithTimeout(ctx, refreshLockTimeout)
		defer cancel()
	}
	if lock, lockErr := cliauth.AcquireRefreshLock(lockCtx); lockErr == nil {
		defer func() { _ = lock.Release() }()
	}

	// Double-checked reload under the lock: a sibling may have refreshed while
	// we waited. Reuse its token rather than replaying our spent refresh token.
	creds, err := cliauth.LoadSession()
	if err != nil {
		return "", err
	}
	if creds == nil {
		return "", os.ErrNotExist
	}
	if creds.AccessToken != "" && !tokenExpired(creds.AccessExpiryTime()) {
		return creds.AccessToken, nil
	}
	if strings.TrimSpace(creds.RefreshToken) == "" {
		return "", cliauth.ErrSessionRevoked
	}

	refreshed, err := cliauth.RefreshSession(ctx, s.BackendURL, s.Version, creds)
	if err != nil {
		if errors.Is(err, cliauth.ErrSessionRevoked) {
			// Session already cleared by RefreshSession; do not wrap so the
			// caller sees the single actionable message.
			return "", cliauth.ErrSessionRevoked
		}
		return "", fmt.Errorf("refresh Orun login: %w", err)
	}
	if refreshed.AccessToken == "" {
		return "", fmt.Errorf("refreshed Orun login did not return an access token")
	}
	return refreshed.AccessToken, nil
}

// ResolveOptions controls token and namespace resolution.
type ResolveOptions struct {
	BackendURL   string
	Version      string
	Interactive  bool
	RequireLogin bool
	NamespaceID  string
}

// ResolvedAuth describes the selected auth source and optional local namespace.
type ResolvedAuth struct {
	TokenSource  TokenSource
	NamespaceID  string
	GitHubLogin  string
	ResolvedMode string
}

// ResolveAuth returns the appropriate remote-state auth information.
func ResolveAuth(ctx context.Context, opts ResolveOptions) (*ResolvedAuth, error) {
	if isGitHubActionsOIDC() {
		return &ResolvedAuth{
			TokenSource:  NewOIDCTokenSource("orun"),
			ResolvedMode: "oidc",
		}, nil
	}
	if token := strings.TrimSpace(os.Getenv("ORUN_TOKEN")); token != "" {
		return &ResolvedAuth{
			TokenSource:  NewStaticTokenSource(token),
			NamespaceID:  strings.TrimSpace(opts.NamespaceID),
			ResolvedMode: "static",
		}, nil
	}
	if strings.TrimSpace(opts.BackendURL) == "" {
		return nil, fmt.Errorf("missing backend URL for local Orun session auth")
	}
	creds, err := cliauth.LoadSession()
	if err != nil {
		if opts.Interactive {
			return nil, fmt.Errorf("no local Orun login found; run `orun auth login` or `orun auth login --device`")
		}
		return nil, fmt.Errorf("no local Orun login found; run `orun auth login --device` or set ORUN_TOKEN")
	}
	if creds == nil {
		if opts.Interactive {
			return nil, fmt.Errorf("no local Orun login found; run `orun auth login` or `orun auth login --device`")
		}
		return nil, fmt.Errorf("no local Orun login found; run `orun auth login --device` or set ORUN_TOKEN")
	}
	if strings.TrimSpace(creds.BackendURL) != "" && !sameURL(creds.BackendURL, opts.BackendURL) {
		return nil, fmt.Errorf("stored Orun login targets %s; run `orun auth login --backend-url %s`", creds.BackendURL, opts.BackendURL)
	}
	namespaceID := strings.TrimSpace(opts.NamespaceID)
	if namespaceID != "" && !containsString(creds.AllowedNamespaceIDs, namespaceID) {
		cfgLink, linkErr := cliauth.FindRepoLinkByNamespaceID(opts.BackendURL, namespaceID)
		if linkErr == nil && cfgLink != nil {
			// cached link confirms this namespace; JWT claims may lag a re-login.
		} else {
			return nil, fmt.Errorf("current Orun login is not authorized for namespace %s; run `orun auth login` again or relink the repo", namespaceID)
		}
	}
	return &ResolvedAuth{
		TokenSource: &SessionTokenSource{
			BackendURL: opts.BackendURL,
			Version:    opts.Version,
		},
		NamespaceID:  namespaceID,
		GitHubLogin:  creds.GitHubLogin,
		ResolvedMode: "session",
	}, nil
}

// ResolveTokenSource returns the remote-state token source plus local namespace.
func ResolveTokenSource(ctx context.Context, opts ResolveOptions) (TokenSource, string, string, error) {
	resolved, err := ResolveAuth(ctx, opts)
	if err != nil {
		return nil, "", "", err
	}
	return resolved.TokenSource, resolved.NamespaceID, resolved.GitHubLogin, nil
}

// isGitHubActionsOIDC reports whether OIDC token acquisition is possible.
func isGitHubActionsOIDC() bool {
	return os.Getenv("GITHUB_ACTIONS") == "true" &&
		os.Getenv("ACTIONS_ID_TOKEN_REQUEST_URL") != "" &&
		os.Getenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN") != ""
}

func tokenExpired(exp time.Time) bool {
	if exp.IsZero() {
		return false
	}
	return time.Now().Add(refreshSkew).After(exp)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func sameURL(a, b string) bool {
	return strings.EqualFold(strings.TrimRight(strings.TrimSpace(a), "/"), strings.TrimRight(strings.TrimSpace(b), "/"))
}
