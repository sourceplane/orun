package cliauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const browserLoginTimeout = 10 * time.Minute

// browserPollInterval is the poll cadence for the browser login flow while
// waiting for the console to approve the cliCode (platform OP1: the CLI polls
// /token until the grant is approved; there is no loopback callback).
const browserPollInterval = 2 * time.Second

// Platform auth endpoint paths (state-api-contract.md §1). Browser-loopback
// login polls cli/token with grantType "cli_code" until the console approves
// the grant; the device flow polls device/poll. Refresh reuses cli/token with
// grantType "refresh_token" (rotating, single-use).
const (
	cliStartPath       = "/v1/auth/cli/start"
	cliTokenPath       = "/v1/auth/cli/token"
	cliRevokePath      = "/v1/auth/cli/revoke"
	cliDeviceStartPath = "/v1/auth/cli/device/start"
	cliDevicePollPath  = "/v1/auth/cli/device/poll"
)

// CLI token grant-type discriminators for POST /v1/auth/cli/token (OP1).
const (
	grantTypeCLICode      = "cli_code"
	grantTypeRefreshToken = "refresh_token"
)

// ErrSessionRevoked is returned when the platform reports that a refresh token
// family has been revoked (rotating-refresh reuse, or a console-side revoke).
// Callers surface a single "run `orun auth login`" message and clear the
// session rather than retrying.
var ErrSessionRevoked = errors.New("Orun session revoked; run `orun auth login`")

// APIError is the backend auth error envelope. It accommodates both the
// platform's nested envelope ({ error: { code, message, details?, requestId } })
// and the OSS backend's flat envelope ({ error, code }). Status carries the HTTP
// status code so callers can distinguish 401 from other failures.
type APIError struct {
	Code      string `json:"code"`
	Message   string `json:"error"`
	RequestID string `json:"-"`
	Status    int    `json:"-"`
	// Details carries the platform error envelope's optional structured detail
	// (e.g. 412 entitlement denials carry { reason: "limit_reached" }).
	Details json.RawMessage `json:"-"`
	// RetryAfter carries the parsed Retry-After header on throttled responses
	// (api-edge 429 rate_limited), so poll loops can back off instead of dying.
	// Zero when the header is absent or unparseable.
	RetryAfter time.Duration `json:"-"`
}

// DetailReason extracts the "reason" field from the error envelope details, if
// present (e.g. "limit_reached" on a 412 entitlement denial).
func (e *APIError) DetailReason() string {
	if e == nil || len(e.Details) == 0 {
		return ""
	}
	var d struct {
		Reason string `json:"reason"`
	}
	if json.Unmarshal(e.Details, &d) != nil {
		return ""
	}
	return d.Reason
}

func (e *APIError) Error() string {
	msg := e.Message
	if e.Code != "" {
		msg = fmt.Sprintf("%s (%s)", msg, e.Code)
	}
	if e.RequestID != "" {
		msg = fmt.Sprintf("%s [requestId: %s]", msg, e.RequestID)
	}
	return msg
}

// isSessionRevoked reports whether an error from the auth API means the stored
// CLI session is no longer usable and the user must re-login. The platform
// (OP1) maps refresh-token reuse/family-revoke and unknown/invalid tokens to
// HTTP 401 code "unauthenticated" (its service-level "not_found"), and an aged
// refresh to HTTP 410 code "expired". The OSS-era aliases are kept for
// back-compat. Any 401/404/410 on a refresh/revoke call is treated as a dead
// session.
func isSessionRevoked(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(apiErr.Code)) {
	case "unauthenticated", "expired", "not_found",
		"family_revoked", "invalid_grant", "session_revoked", "token_reused":
		return true
	}
	switch apiErr.Status {
	case http.StatusUnauthorized, http.StatusNotFound, http.StatusGone:
		return true
	}
	return false
}

// BackendClient talks to the Orun backend auth/account routes.
type BackendClient struct {
	baseURL    string
	userAgent  string
	httpClient *http.Client
}

// CLIStartResponse is the unwrapped data payload of POST /v1/auth/cli/start
// (OP1). The CLI opens AuthorizeURL in a browser; the console approves the
// request keyed by CLICode; the CLI then polls cli/token with grantType
// "cli_code" + cliCode until the grant is approved and a session is minted
// (state-api-contract.md §1).
type CLIStartResponse struct {
	AuthorizeURL string `json:"authorizeUrl"`
	CLICode      string `json:"cliCode"`
	ExpiresAt    string `json:"expiresAt"`
}

// deadline returns the start-grant expiry parsed from ExpiresAt (RFC3339),
// falling back to a 10-minute window from now.
func (s *CLIStartResponse) deadline(now time.Time) time.Time {
	if s.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, s.ExpiresAt); err == nil {
			return t
		}
	}
	return now.Add(10 * time.Minute)
}

// DeviceStartResponse is returned by POST /v1/auth/cli/device/start (RFC-8628).
// VerificationURIComplete and ExpiresIn are accepted as fallbacks for the OSS
// backend's older field names.
type DeviceStartResponse struct {
	DeviceCode              string `json:"deviceCode"`
	UserCode                string `json:"userCode"`
	VerificationURL         string `json:"verificationUrl"`
	VerificationURI         string `json:"verificationUri,omitempty"`
	VerificationURIComplete string `json:"verificationUriComplete,omitempty"`
	ExpiresAt               string `json:"expiresAt,omitempty"`
	ExpiresIn               int    `json:"expiresIn,omitempty"`
	Interval                int    `json:"interval"`
}

// verificationTarget returns the best URL to show the user for device approval.
func (d *DeviceStartResponse) verificationTarget() string {
	if d.VerificationURIComplete != "" {
		return d.VerificationURIComplete
	}
	if d.VerificationURL != "" {
		return d.VerificationURL
	}
	return d.VerificationURI
}

// deadline returns the device-flow expiry, from expiresAt (RFC3339) or
// expiresIn (seconds from now), or a 10-minute default.
func (d *DeviceStartResponse) deadline(now time.Time) time.Time {
	if d.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, d.ExpiresAt); err == nil {
			return t
		}
	}
	if d.ExpiresIn > 0 {
		return now.Add(time.Duration(d.ExpiresIn) * time.Second)
	}
	return now.Add(10 * time.Minute)
}

// DevicePollPendingResponse is the unwrapped data payload returned while device
// auth is pending (OP1: { status: "pending", error: "authorization_pending" }).
type DevicePollPendingResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
}

// SessionUser is the authenticated user in a session payload
// (state-api-contract.md §1: user: { id, email, displayName }).
type SessionUser struct {
	ID          string `json:"id,omitempty"`
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
}

// SessionResponse is returned by the CLI auth routes.
//
// Org model (OC0): Orgs is the platform's org/project-spine field (contract §1).
// AllowedNamespaceIDs is kept for back-compat with the OSS backend, which still
// returns it (retired in OC6); orgs[].id serves the same purpose.
type SessionResponse struct {
	AccessToken         string      `json:"accessToken"`
	ExpiresAt           string      `json:"expiresAt"`
	RefreshToken        string      `json:"refreshToken,omitempty"`
	RefreshExpiresAt    string      `json:"refreshExpiresAt,omitempty"`
	User                SessionUser `json:"user"`
	GitHubLogin         string      `json:"githubLogin,omitempty"`
	Orgs                []OrgRef    `json:"orgs,omitempty"`
	AllowedNamespaceIDs []string    `json:"allowedNamespaceIds,omitempty"`
}

// WorkspaceLink is a single org/project ↔ remote binding (platform OP4). It is
// returned by POST /v1/organizations/{orgId}/cli/links (create) and by GET
// /v1/cli/links/resolve (candidates/links). RemoteURL is the server's canonical
// normalized form (lowercase host/owner/repo, no scheme/.git) — the CLI caches
// THAT, never its own normalization.
type WorkspaceLink struct {
	ID          string          `json:"id,omitempty"`
	OrgID       string          `json:"orgId,omitempty"`
	OrgSlug     string          `json:"orgSlug,omitempty"`
	ProjectID   string          `json:"projectId,omitempty"`
	ProjectSlug string          `json:"projectSlug,omitempty"`
	RemoteURL   string          `json:"remoteUrl,omitempty"`
	CreatedBy   *LinkActor      `json:"createdBy,omitempty"`
	CreatedAt   string          `json:"createdAt,omitempty"`
	LastSeenAt  json.RawMessage `json:"lastSeenAt,omitempty"`
}

// LinkActor identifies who created a workspace link (createdBy in the OP4
// payload).
type LinkActor struct {
	ID   string `json:"id,omitempty"`
	Kind string `json:"kind,omitempty"`
}

// ResolveLinksResponse is the unwrapped data payload of GET
// /v1/cli/links/resolve?remoteUrl=… (OP4). Candidates are the orgs/projects the
// actor may link/use for the remote; Links are existing active links for it.
type ResolveLinksResponse struct {
	Candidates []WorkspaceLink `json:"candidates"`
	Links      []WorkspaceLink `json:"links"`
}

// BrowserOpener opens the system browser.
type BrowserOpener func(string) error

// NewBackendClient returns an auth/account client for the backend.
func NewBackendClient(baseURL, version string) *BackendClient {
	return &BackendClient{
		baseURL:   strings.TrimRight(baseURL, "/"),
		userAgent: "orun-cli/" + version,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// StartCLILogin begins the browser login (POST /v1/auth/cli/start, OP1). The
// body is just the CLI host label shown on the console approval page; the
// platform accepts NO redirect-uri/port and never calls back a loopback
// listener. The CLI opens the returned authorizeUrl and polls cli/token.
func (c *BackendClient) StartCLILogin(ctx context.Context) (*CLIStartResponse, error) {
	body := map[string]string{"host": cliHost()}
	var resp CLIStartResponse
	if err := c.doJSONData(ctx, http.MethodPost, cliStartPath, nil, body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// RedeemCLILogin redeems the console-approved cliCode for a session (POST
// /v1/auth/cli/token with grantType "cli_code", OP1). While the grant is still
// pending the platform returns HTTP 400 code "validation_failed" with message
// "Not yet approved"; callers detect that via cliCodePending and keep polling.
// Terminal failures (not_found / expired) surface as an *APIError.
func (c *BackendClient) RedeemCLILogin(ctx context.Context, cliCode string) (*SessionResponse, error) {
	body := map[string]string{
		"grantType": grantTypeCLICode,
		"cliCode":   cliCode,
	}
	data, _, err := c.do(ctx, http.MethodPost, cliTokenPath, nil, body)
	if err != nil {
		return nil, err
	}
	return decodeSessionData(data)
}

// cliCodePending reports whether an error from RedeemCLILogin means the grant
// has not yet been approved (keep polling) rather than a terminal failure. The
// platform returns the pending state as HTTP 400 invalid_request / "Not yet
// approved" (mapped by api-edge to code "validation_failed"). not_found and
// expired are terminal and are excluded here.
func cliCodePending(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.Status != http.StatusBadRequest {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(apiErr.Code)) {
	case "not_found", "expired", "unauthenticated", "internal_error":
		return false
	}
	// 400 validation_failed / invalid_request: not yet approved (still pending).
	return true
}

// StartDeviceFlow starts the platform device flow (POST /v1/auth/cli/device/start,
// RFC-8628, OP1).
func (c *BackendClient) StartDeviceFlow(ctx context.Context) (*DeviceStartResponse, error) {
	var resp DeviceStartResponse
	if err := c.doJSONData(ctx, http.MethodPost, cliDeviceStartPath, nil, map[string]string{"host": cliHost()}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// PollDeviceFlow polls the platform device-flow endpoint (OP1). The body of a
// pending/complete response is the {data,meta} success envelope:
//   - data.status == "pending"  → still waiting (keep polling at interval).
//   - data.status == "complete" → data.session carries the minted session.
//
// Terminal denials are HTTP errors: 403 access_denied (denied) and 410 expired,
// returned to the caller as *APIError.
func (c *BackendClient) PollDeviceFlow(ctx context.Context, deviceCode string) (*SessionResponse, *DevicePollPendingResponse, error) {
	body := map[string]string{"deviceCode": deviceCode}
	data, _, err := c.do(ctx, http.MethodPost, cliDevicePollPath, nil, body)
	if err != nil {
		return nil, nil, err
	}
	payload, err := unwrapData(data)
	if err != nil {
		return nil, nil, err
	}
	var marker struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(payload, &marker)
	if strings.EqualFold(marker.Status, "complete") {
		session, err := decodeNestedSession(payload)
		if err != nil {
			return nil, nil, err
		}
		return session, nil, nil
	}
	// Anything that is not an explicit "complete" is treated as pending.
	return nil, decodePending(payload), nil
}

// decodePending parses a device-poll pending payload, defaulting Status to
// "pending".
func decodePending(payload []byte) *DevicePollPendingResponse {
	pending := &DevicePollPendingResponse{Status: "pending"}
	_ = json.Unmarshal(payload, pending)
	if pending.Status == "" {
		pending.Status = "pending"
	}
	return pending
}

// decodeNestedSession parses the device-poll complete payload, where the session
// is nested under "session" ({ status: "complete", session: {…} }).
func decodeNestedSession(payload []byte) (*SessionResponse, error) {
	var wrapped struct {
		Session *SessionResponse `json:"session"`
	}
	if err := json.Unmarshal(payload, &wrapped); err != nil {
		return nil, fmt.Errorf("decode device session payload: %w", err)
	}
	if wrapped.Session == nil || wrapped.Session.AccessToken == "" {
		return nil, fmt.Errorf("device poll completed without a session")
	}
	return wrapped.Session, nil
}

// decodeSessionData unwraps the {data,meta} success envelope and parses the
// session that sits directly under data (cli/token responses).
func decodeSessionData(data []byte) (*SessionResponse, error) {
	payload, err := unwrapData(data)
	if err != nil {
		return nil, err
	}
	var session SessionResponse
	if err := json.Unmarshal(payload, &session); err != nil {
		return nil, fmt.Errorf("decode session payload: %w", err)
	}
	return &session, nil
}

// Refresh exchanges a (rotating, single-use) refresh token for a new access
// token and a new refresh token (POST /v1/auth/cli/token, grantType
// "refresh_token", OP1). A revoked/expired/reused token (401/404/410, or a
// revoke/expired code) is returned as ErrSessionRevoked.
func (c *BackendClient) Refresh(ctx context.Context, refreshToken string) (*SessionResponse, error) {
	body := map[string]string{
		"grantType":    grantTypeRefreshToken,
		"refreshToken": refreshToken,
	}
	data, _, err := c.do(ctx, http.MethodPost, cliTokenPath, nil, body)
	if err != nil {
		if isSessionRevoked(err) {
			return nil, ErrSessionRevoked
		}
		return nil, err
	}
	return decodeSessionData(data)
}

// Logout revokes the session (POST /v1/auth/cli/revoke, OP1). An
// already-revoked/expired session (401/404/410) is treated as success — the
// session is already gone.
func (c *BackendClient) Logout(ctx context.Context, refreshToken string) error {
	_, _, err := c.do(ctx, http.MethodPost, cliRevokePath, nil, map[string]string{"refreshToken": refreshToken})
	if err != nil && isSessionRevoked(err) {
		return nil
	}
	return err
}

// ResolveLinks calls GET /v1/cli/links/resolve?remoteUrl=… (OP4). The raw git
// remote is sent verbatim; the server normalizes it and restricts candidates to
// the actor's orgs. A bad remote surfaces as a 422 *APIError.
func (c *BackendClient) ResolveLinks(ctx context.Context, accessToken, remoteURL string) (*ResolveLinksResponse, error) {
	path := "/v1/cli/links/resolve?remoteUrl=" + url.QueryEscape(remoteURL)
	var resp ResolveLinksResponse
	if err := c.doJSONData(ctx, http.MethodGet, path,
		map[string]string{"Authorization": "Bearer " + accessToken}, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CreateLink calls POST /v1/organizations/{orgId}/cli/links (OP4). projectSlug
// is optional; the server creates the project on demand. The returned link
// carries the server's canonical normalized remoteUrl — the caller caches that.
// Errors map to: 422 bad remote, 404 policy/membership denial, 412
// limit_reached (entitlement), 409 already-linked.
func (c *BackendClient) CreateLink(ctx context.Context, accessToken, orgID, remoteURL, projectSlug string) (*WorkspaceLink, error) {
	body := map[string]string{"remoteUrl": remoteURL}
	if s := strings.TrimSpace(projectSlug); s != "" {
		body["projectSlug"] = s
	}
	var wrapped struct {
		Link *WorkspaceLink `json:"link"`
	}
	if err := c.doJSONData(ctx, http.MethodPost,
		"/v1/organizations/"+url.PathEscape(strings.TrimSpace(orgID))+"/cli/links",
		map[string]string{"Authorization": "Bearer " + accessToken}, body, &wrapped); err != nil {
		return nil, err
	}
	if wrapped.Link == nil {
		return nil, &APIError{Code: "INVALID_RESPONSE", Message: "link create returned no link object"}
	}
	return wrapped.Link, nil
}

// ListOrgLinks calls GET /v1/organizations/{orgId}/cli/links (#185) and returns
// the org's full allow-list of active workspace links, following pagination
// cursors. It powers `orun cloud check` and the 404 denial disambiguation:
// because the server hides denials as 404 (resource-hiding), the CLI must not
// infer "not allow-listed" from a status code alone — it consults this listing
// and only claims "not allow-listed" when the repo is genuinely absent. A
// policy/membership denial surfaces as a 404 *APIError.
func (c *BackendClient) ListOrgLinks(ctx context.Context, accessToken, orgID string) ([]WorkspaceLink, error) {
	base := "/v1/organizations/" + url.PathEscape(strings.TrimSpace(orgID)) + "/cli/links"
	headers := map[string]string{"Authorization": "Bearer " + accessToken}
	all := []WorkspaceLink{}
	cursor := ""
	// Bound the walk so a misbehaving server can never spin us forever.
	for page := 0; page < 100; page++ {
		path := base
		if cursor != "" {
			path += "?cursor=" + url.QueryEscape(cursor)
		}
		var resp struct {
			Links      []WorkspaceLink `json:"links"`
			NextCursor *struct {
				CreatedAt string `json:"createdAt"`
				ID        string `json:"id"`
			} `json:"nextCursor"`
		}
		if err := c.doJSONData(ctx, http.MethodGet, path, headers, nil, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Links...)
		if resp.NextCursor == nil || strings.TrimSpace(resp.NextCursor.ID) == "" {
			break
		}
		cursor = resp.NextCursor.CreatedAt + "|" + resp.NextCursor.ID
	}
	return all, nil
}

// CreateOrg creates an organization the actor owns (POST /v1/organizations) and
// returns it as an OrgRef. Used by the zero-org auto-link path (UO2) to
// materialize a personal org on first login. A 409 (slug already taken)
// surfaces as an *APIError so the caller can retry with a different slug.
func (c *BackendClient) CreateOrg(ctx context.Context, accessToken, name, slug string) (*OrgRef, error) {
	body := map[string]string{"name": name}
	if s := strings.TrimSpace(slug); s != "" {
		body["slug"] = s
	}
	var wrapped struct {
		Organization struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Slug string `json:"slug"`
		} `json:"organization"`
	}
	if err := c.doJSONData(ctx, http.MethodPost, "/v1/organizations",
		map[string]string{"Authorization": "Bearer " + accessToken}, body, &wrapped); err != nil {
		return nil, err
	}
	if strings.TrimSpace(wrapped.Organization.ID) == "" {
		return nil, &APIError{Code: "INVALID_RESPONSE", Message: "org create returned no organization"}
	}
	return &OrgRef{
		ID:   wrapped.Organization.ID,
		Slug: wrapped.Organization.Slug,
		Name: wrapped.Organization.Name,
		Role: "owner",
	}, nil
}

// BrowserLogin performs the platform browser login (POST /v1/auth/cli/start,
// OP1). It opens the returned authorizeUrl, then POLLS cli/token (grantType
// "cli_code" + cliCode) until the console approves the grant and a session is
// minted, or the grant's expiresAt deadline passes. The platform never calls
// back a loopback listener — approval happens entirely in the browser/console
// and completion is determined by polling, not by a loopback callback.
func BrowserLogin(ctx context.Context, backendURL, version string, out io.Writer, openBrowser BrowserOpener) (*Credentials, error) {
	client := NewBackendClient(backendURL, version)
	if openBrowser == nil {
		openBrowser = OpenBrowser
	}

	start, err := client.StartCLILogin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin Orun login: %w", err)
	}
	if strings.TrimSpace(start.AuthorizeURL) == "" {
		return nil, fmt.Errorf("backend did not return an authorize URL")
	}
	if strings.TrimSpace(start.CLICode) == "" {
		return nil, fmt.Errorf("backend did not return a CLI code")
	}

	if out != nil {
		fmt.Fprintf(out, "Open the browser to authenticate with Orun:\n%s\n\n", start.AuthorizeURL)
		fmt.Fprintf(out, "Waiting for approval...\n")
	}
	if err := openBrowser(start.AuthorizeURL); err != nil && out != nil {
		fmt.Fprintf(out, "Browser open failed: %v\nContinue with the URL above.\n\n", err)
	}

	loginCtx, cancel := context.WithTimeout(ctx, browserLoginTimeout)
	defer cancel()
	deadline := start.deadline(time.Now())

	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("browser login expired before approval")
		}
		session, err := client.RedeemCLILogin(loginCtx, start.CLICode)
		if err == nil {
			creds := sessionResponseToCredentials(session, backendURL)
			if err := SaveSession(creds); err != nil {
				return nil, err
			}
			return creds, nil
		}
		if cliCodePending(err) {
			if !sleepCtx(loginCtx, browserPollInterval) {
				if errors.Is(loginCtx.Err(), context.DeadlineExceeded) {
					return nil, fmt.Errorf("browser login timed out")
				}
				return nil, loginCtx.Err()
			}
			continue
		}
		return nil, fmt.Errorf("redeem Orun login: %w", err)
	}
}

// DeviceLogin performs the platform device flow (RFC-8628) and stores the
// session. The platform owns the grant; GitHub identity is just one of its
// login methods.
func DeviceLogin(ctx context.Context, backendURL, version string, out io.Writer) (*Credentials, error) {
	client := NewBackendClient(backendURL, version)
	start, err := client.StartDeviceFlow(ctx)
	if err != nil {
		return nil, err
	}
	if out != nil {
		fmt.Fprintf(out, "To authenticate, visit the verification page and enter the code:\n")
		fmt.Fprintf(out, "Code: %s\n", start.UserCode)
		fmt.Fprintf(out, "Verify: %s\n\n", start.verificationTarget())
	}
	interval := start.Interval
	if interval <= 0 {
		interval = 5
	}
	deadline := start.deadline(time.Now())
	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("device login expired")
		}
		session, pending, err := client.PollDeviceFlow(ctx, start.DeviceCode)
		if err != nil {
			var apiErr *APIError
			if errors.As(err, &apiErr) {
				// Terminal denials (OP1): 403 access_denied, 410 expired.
				switch {
				case apiErr.Status == http.StatusForbidden ||
					strings.EqualFold(apiErr.Code, "access_denied"):
					return nil, fmt.Errorf("device login denied")
				case apiErr.Status == http.StatusGone ||
					strings.EqualFold(apiErr.Code, "expired"):
					return nil, fmt.Errorf("device login expired")
				case apiErr.Status == http.StatusTooManyRequests ||
					strings.EqualFold(apiErr.Code, "rate_limited"):
					// api-edge throttles the identity scope aggressively; a
					// 429 mid-poll is transient, not a denial. Honor
					// Retry-After when present (never polling faster than the
					// grant's interval); otherwise back off like RFC-8628
					// slow_down and keep polling.
					wait := apiErr.RetryAfter
					if wait <= 0 {
						interval += 5
						wait = time.Duration(interval) * time.Second
					} else if minWait := time.Duration(interval) * time.Second; wait < minWait {
						wait = minWait
					}
					if !sleepCtx(ctx, wait) {
						return nil, ctx.Err()
					}
					continue
				}
			}
			return nil, err
		}
		if pending != nil {
			// RFC-8628 slow_down: back off the poll interval.
			if strings.EqualFold(pending.Error, "slow_down") {
				interval += 5
			}
			if !sleepCtx(ctx, time.Duration(interval)*time.Second) {
				return nil, ctx.Err()
			}
			continue
		}
		creds := sessionResponseToCredentials(session, backendURL)
		if err := SaveSession(creds); err != nil {
			return nil, err
		}
		return creds, nil
	}
}

// RefreshSession refreshes the local CLI session using the rotating,
// single-use refresh token. On success it persists the NEW refresh token
// returned by the platform (the old one is now spent). When the platform
// reports the refresh-token family revoked (reuse, or a console-side revoke),
// it clears the local session and returns ErrSessionRevoked so the caller can
// surface a single "run `orun auth login`" message rather than retrying.
func RefreshSession(ctx context.Context, backendURL, version string, creds *Credentials) (*Credentials, error) {
	if creds == nil {
		return nil, os.ErrNotExist
	}
	if strings.TrimSpace(creds.BackendURL) != "" && !sameURL(creds.BackendURL, backendURL) {
		return nil, fmt.Errorf("stored login is for %s; run `orun auth login --backend-url %s`", creds.BackendURL, backendURL)
	}
	if strings.TrimSpace(creds.RefreshToken) == "" {
		return nil, ErrSessionRevoked
	}
	client := NewBackendClient(backendURL, version)
	resp, err := client.Refresh(ctx, creds.RefreshToken)
	if err != nil {
		if errors.Is(err, ErrSessionRevoked) {
			// Single-use reuse / family revoked: the stored session is dead.
			_ = ClearSession()
			return nil, ErrSessionRevoked
		}
		return nil, err
	}
	updated := sessionResponseToCredentials(resp, backendURL)
	// Rotating refresh: keep the NEW refresh token if the server rotated it;
	// fall back to the prior one only if the response omitted it (some servers
	// return access-token-only refreshes).
	if strings.TrimSpace(updated.RefreshToken) == "" {
		updated.RefreshToken = creds.RefreshToken
		updated.RefreshTokenExpiry = creds.RefreshTokenExpiry
	}
	// A refresh response may omit the identity/org spine (it only mints a new
	// access token); carry the prior info forward so it is not lost.
	if updated.GitHubLogin == "" {
		updated.GitHubLogin = creds.GitHubLogin
	}
	if updated.User == (SessionUser{}) {
		updated.User = creds.User
	}
	if len(updated.Orgs) == 0 {
		updated.Orgs = append([]OrgRef(nil), creds.Orgs...)
	}
	if len(updated.AllowedNamespaceIDs) == 0 {
		updated.AllowedNamespaceIDs = append([]string(nil), creds.AllowedNamespaceIDs...)
	}
	if err := SaveSession(updated); err != nil {
		return nil, err
	}
	return updated, nil
}

// sleepCtx sleeps for d or until ctx is cancelled. Returns false if cancelled.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// OpenBrowser opens the system browser.
func OpenBrowser(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = execCommand("open", target)
	case "windows":
		cmd = execCommand("cmd", "/c", "start", target)
	default:
		cmd = execCommand("xdg-open", target)
	}
	return cmd.Start()
}

// doJSONData performs a request and decodes the unwrapped `data` payload of the
// platform's `{ data, meta }` success envelope (OP1) into out. All platform
// success responses are enveloped; this is the standard decode path.
func (c *BackendClient) doJSONData(ctx context.Context, method, path string, headers map[string]string, body interface{}, out interface{}) error {
	data, _, err := c.do(ctx, method, path, headers, body)
	if err != nil {
		return err
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	payload, err := unwrapData(data)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("decode backend response: %w", err)
	}
	return nil
}

// unwrapData extracts the `data` field from the platform's `{ data, meta }`
// success envelope (OP1). api-edge forwards the identity-worker body verbatim,
// so every 2xx auth/account response is enveloped. If the body has no `data`
// key (e.g. an OSS backend that does not envelope), the raw body is returned so
// callers still parse the legacy flat shape.
func unwrapData(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return data, nil
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("decode backend envelope: %w", err)
	}
	if len(env.Data) > 0 && string(env.Data) != "null" {
		return env.Data, nil
	}
	// No envelope present (or data:null): fall back to the raw body.
	return data, nil
}

// cliHost returns the host label sent on cli/start and device/start; the console
// shows it on the approval page. Best-effort: an empty label is acceptable.
func cliHost() string {
	if h, err := os.Hostname(); err == nil && strings.TrimSpace(h) != "" {
		return h
	}
	return "orun-cli"
}

func (c *BackendClient) do(ctx context.Context, method, path string, headers map[string]string, body interface{}) ([]byte, int, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		reqBody = strings.NewReader(string(data))
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request backend auth API: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		apiErr := decodeAuthError(data, resp.StatusCode)
		apiErr.RetryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
		return nil, resp.StatusCode, apiErr
	}
	return data, resp.StatusCode, nil
}

// parseRetryAfter parses a Retry-After header value (delta-seconds or an
// HTTP-date, RFC 9110 §10.2.3) into a duration. Returns 0 when the value is
// absent, unparseable, or in the past.
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// decodeAuthError parses both the platform's nested error envelope
// ({ error: { code, message, requestId } }) and the OSS backend's flat envelope
// ({ error, code }), falling back to a status-derived message.
func decodeAuthError(data []byte, status int) *APIError {
	// Platform nested shape.
	var nested struct {
		Error *struct {
			Code      string          `json:"code"`
			Message   string          `json:"message"`
			Details   json.RawMessage `json:"details"`
			RequestID string          `json:"requestId"`
		} `json:"error"`
		RequestID string `json:"requestId"`
	}
	if json.Unmarshal(data, &nested) == nil && nested.Error != nil &&
		(nested.Error.Message != "" || nested.Error.Code != "") {
		reqID := nested.Error.RequestID
		if reqID == "" {
			reqID = nested.RequestID
		}
		return &APIError{
			Code:      nested.Error.Code,
			Message:   nested.Error.Message,
			Details:   nested.Error.Details,
			RequestID: reqID,
			Status:    status,
		}
	}
	// Flat OSS shape: { "error": "msg", "code": "CODE", "requestId": "…" }.
	var flat struct {
		Error     string `json:"error"`
		Code      string `json:"code"`
		RequestID string `json:"requestId"`
	}
	if json.Unmarshal(data, &flat) == nil && flat.Error != "" {
		return &APIError{
			Code:      flat.Code,
			Message:   flat.Error,
			RequestID: flat.RequestID,
			Status:    status,
		}
	}
	return &APIError{
		Code:    fmt.Sprintf("HTTP_%d", status),
		Message: strings.TrimSpace(string(data)),
		Status:  status,
	}
}

func sessionResponseToCredentials(resp *SessionResponse, backendURL string) *Credentials {
	orgs := append([]OrgRef(nil), resp.Orgs...)
	// Back-compat: if the server only sent allowedNamespaceIds (OSS backend),
	// synthesize Orgs from them so the rest of the CLI sees the org spine.
	if len(orgs) == 0 {
		for _, id := range resp.AllowedNamespaceIDs {
			if id = strings.TrimSpace(id); id != "" {
				orgs = append(orgs, OrgRef{ID: id})
			}
		}
	}
	expiry := resp.ExpiresAt
	if strings.TrimSpace(expiry) == "" {
		expiry = jwtExpiry(resp.AccessToken)
	}
	// Keep the legacy githubLogin display field populated from the platform's
	// user payload so existing surfaces that read it still show something.
	display := resp.GitHubLogin
	if display == "" {
		display = resp.User.DisplayName
	}
	if display == "" {
		display = resp.User.Email
	}
	return &Credentials{
		AccessToken:         resp.AccessToken,
		AccessTokenExpiry:   expiry,
		RefreshToken:        resp.RefreshToken,
		RefreshTokenExpiry:  resp.RefreshExpiresAt,
		User:                resp.User,
		GitHubLogin:         display,
		Orgs:                orgs,
		AllowedNamespaceIDs: append([]string(nil), resp.AllowedNamespaceIDs...),
		BackendURL:          backendURL,
	}
}

func jwtExpiry(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var payload struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payloadBytes, &payload); err != nil || payload.Exp <= 0 {
		return ""
	}
	return time.Unix(payload.Exp, 0).UTC().Format(time.RFC3339)
}
