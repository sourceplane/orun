package cliauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const browserLoginTimeout = 10 * time.Minute

// APIError is the backend auth error envelope.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"error"`
}

func (e *APIError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return fmt.Sprintf("%s (%s)", e.Message, e.Code)
}

// BackendClient talks to the Orun backend auth/account routes.
type BackendClient struct {
	baseURL    string
	userAgent  string
	httpClient *http.Client
}

// DeviceStartResponse is returned by POST /v1/auth/cli/device/start.
type DeviceStartResponse struct {
	DeviceCode              string `json:"deviceCode"`
	UserCode                string `json:"userCode"`
	VerificationURI         string `json:"verificationUri"`
	VerificationURIComplete string `json:"verificationUriComplete"`
	ExpiresIn               int    `json:"expiresIn"`
	Interval                int    `json:"interval"`
}

// DevicePollPendingResponse is returned while device auth is pending.
type DevicePollPendingResponse struct {
	Status   string `json:"status"`
	Interval int    `json:"interval"`
}

// SessionResponse is returned by the CLI auth routes.
type SessionResponse struct {
	AccessToken         string   `json:"accessToken"`
	ExpiresAt           string   `json:"expiresAt"`
	RefreshToken        string   `json:"refreshToken,omitempty"`
	RefreshExpiresAt    string   `json:"refreshExpiresAt,omitempty"`
	GitHubLogin         string   `json:"githubLogin"`
	AllowedNamespaceIDs []string `json:"allowedNamespaceIds"`
}

// LinkedRepo is returned by GET /v1/accounts/repos.
type LinkedRepo struct {
	NamespaceID   string `json:"namespaceId"`
	NamespaceSlug string `json:"namespaceSlug"`
	LinkedAt      string `json:"linkedAt"`
}

// Account is returned by GET /v1/accounts/me.
type Account struct {
	AccountID   string `json:"accountId"`
	GitHubLogin string `json:"githubLogin"`
	CreatedAt   string `json:"createdAt"`
}

// LinkRepoFromSessionResponse is returned by POST /v1/accounts/repos/link.
// The backend always returns namespaceKind: "local" for CLI session links.
type LinkRepoFromSessionResponse struct {
	NamespaceKind string `json:"namespaceKind"`
	NamespaceID   string `json:"namespaceId"`
	NamespaceSlug string `json:"namespaceSlug"`
	RepoID        string `json:"repoId"`
	RepoFullName  string `json:"repoFullName"`
	LinkedAt      string `json:"linkedAt"`
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

// BrowserLoginURL returns the CLI browser-login URL.
func (c *BackendClient) BrowserLoginURL(returnTo string) string {
	v := url.Values{}
	v.Set("client", "cli")
	v.Set("returnTo", returnTo)
	return c.baseURL + "/v1/auth/github?" + v.Encode()
}

// StartDeviceFlow starts the backend-mediated GitHub device flow.
func (c *BackendClient) StartDeviceFlow(ctx context.Context) (*DeviceStartResponse, error) {
	var resp DeviceStartResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v1/auth/cli/device/start", nil, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// PollDeviceFlow polls the backend device-flow endpoint.
func (c *BackendClient) PollDeviceFlow(ctx context.Context, deviceCode string) (*SessionResponse, *DevicePollPendingResponse, error) {
	body := map[string]string{"deviceCode": deviceCode}
	data, status, err := c.do(ctx, http.MethodPost, "/v1/auth/cli/device/poll", nil, body)
	if err != nil {
		return nil, nil, err
	}
	if status == http.StatusAccepted {
		var pending DevicePollPendingResponse
		if err := json.Unmarshal(data, &pending); err != nil {
			return nil, nil, err
		}
		return nil, &pending, nil
	}
	var session SessionResponse
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, nil, err
	}
	return &session, nil, nil
}

// Refresh exchanges a refresh token for a new access token.
func (c *BackendClient) Refresh(ctx context.Context, refreshToken string) (*SessionResponse, error) {
	var resp SessionResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v1/auth/cli/token", nil, map[string]string{"refreshToken": refreshToken}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Logout revokes the refresh token.
func (c *BackendClient) Logout(ctx context.Context, refreshToken string) error {
	return c.doJSON(ctx, http.MethodPost, "/v1/auth/cli/logout", nil, map[string]string{"refreshToken": refreshToken}, nil)
}

// GetAccount returns the current backend account.
func (c *BackendClient) GetAccount(ctx context.Context, accessToken string) (*Account, error) {
	var resp Account
	if err := c.doJSON(ctx, http.MethodGet, "/v1/accounts/me", map[string]string{"Authorization": "Bearer " + accessToken}, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListLinkedRepos returns repos linked to the current account.
func (c *BackendClient) ListLinkedRepos(ctx context.Context, accessToken string) ([]LinkedRepo, error) {
	var resp struct {
		Repos []LinkedRepo `json:"repos"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/v1/accounts/repos", map[string]string{"Authorization": "Bearer " + accessToken}, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Repos, nil
}

// LinkRepoFromSession calls POST /v1/accounts/repos/link with a CLI session token.
// Returns an error if the response does not contain namespaceKind: "local" — this
// guards against older backend versions that may return canonical repo namespaces.
func (c *BackendClient) LinkRepoFromSession(ctx context.Context, accessToken, repoFullName string) (*LinkRepoFromSessionResponse, error) {
	var resp LinkRepoFromSessionResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v1/accounts/repos/link",
		map[string]string{"Authorization": "Bearer " + accessToken},
		map[string]string{"repoFullName": repoFullName},
		&resp,
	); err != nil {
		return nil, err
	}
	if resp.NamespaceKind != "local" {
		return nil, &APIError{
			Code:    "INVALID_RESPONSE",
			Message: fmt.Sprintf("link endpoint returned namespaceKind %q, expected \"local\"; ensure the backend includes Task 0012.2.1", resp.NamespaceKind),
		}
	}
	return &resp, nil
}

// BrowserLogin performs the CLI loopback OAuth flow and stores the resulting session.
func BrowserLogin(ctx context.Context, backendURL, version string, out io.Writer, openBrowser BrowserOpener) (*Credentials, error) {
	client := NewBackendClient(backendURL, version)
	if openBrowser == nil {
		openBrowser = OpenBrowser
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start loopback listener: %w", err)
	}
	defer ln.Close()

	nonce, err := randomNonce(12)
	if err != nil {
		return nil, err
	}
	callbackPath := "/callback/" + nonce
	callbackURL := "http://" + ln.Addr().String() + callbackPath
	loginURL := client.BrowserLoginURL(callbackURL)

	type loginResult struct {
		creds *Credentials
		err   error
	}
	resultCh := make(chan loginResult, 1)

	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><html><body>Completing Orun login...<script>
fetch(location.pathname + "/complete", {method:"POST", headers:{"Content-Type":"text/plain"}, body:location.hash.replace(/^#/,"")})
  .then(() => { document.body.textContent = "Orun login complete. You can close this window."; })
  .catch(() => { document.body.textContent = "Orun login failed. Return to the terminal."; });
</script></body></html>`)
	})
	mux.HandleFunc(callbackPath+"/complete", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		creds, parseErr := credentialsFromFragment(string(body), backendURL)
		if parseErr == nil {
			parseErr = SaveSession(creds)
		}
		select {
		case resultCh <- loginResult{creds: creds, err: parseErr}:
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	})

	server := &http.Server{Handler: mux}
	go func() {
		_ = server.Serve(ln)
	}()
	defer server.Shutdown(context.Background())

	if out != nil {
		fmt.Fprintf(out, "Open the browser to authenticate with Orun:\n%s\n\n", loginURL)
	}
	if err := openBrowser(loginURL); err != nil && out != nil {
		fmt.Fprintf(out, "Browser open failed: %v\nContinue with the URL above.\n\n", err)
	}

	loginCtx, cancel := context.WithTimeout(ctx, browserLoginTimeout)
	defer cancel()
	select {
	case <-loginCtx.Done():
		return nil, fmt.Errorf("browser login timed out")
	case result := <-resultCh:
		if result.err != nil {
			return nil, result.err
		}
		return result.creds, nil
	}
}

// DeviceLogin performs the backend-mediated device flow and stores the session.
func DeviceLogin(ctx context.Context, backendURL, version string, out io.Writer) (*Credentials, error) {
	client := NewBackendClient(backendURL, version)
	start, err := client.StartDeviceFlow(ctx)
	if err != nil {
		return nil, err
	}
	if out != nil {
		fmt.Fprintf(out, "Complete GitHub device login:\n")
		fmt.Fprintf(out, "Code: %s\n", start.UserCode)
		fmt.Fprintf(out, "Verify: %s\n\n", start.VerificationURIComplete)
	}
	interval := start.Interval
	if interval <= 0 {
		interval = 5
	}
	deadline := time.Now().Add(time.Duration(start.ExpiresIn) * time.Second)
	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("device login expired")
		}
		session, pending, err := client.PollDeviceFlow(ctx, start.DeviceCode)
		if err != nil {
			var apiErr *APIError
			if errors.As(err, &apiErr) && apiErr.Code == "RATE_LIMITED" {
				interval += 5
				time.Sleep(time.Duration(interval) * time.Second)
				continue
			}
			return nil, err
		}
		if pending != nil {
			if pending.Interval > 0 {
				interval = pending.Interval
			}
			time.Sleep(time.Duration(interval) * time.Second)
			continue
		}
		creds := sessionResponseToCredentials(session, backendURL)
		if err := SaveSession(creds); err != nil {
			return nil, err
		}
		return creds, nil
	}
}

// RefreshSession refreshes the local CLI session if needed.
func RefreshSession(ctx context.Context, backendURL, version string, creds *Credentials) (*Credentials, error) {
	if creds == nil {
		return nil, os.ErrNotExist
	}
	if strings.TrimSpace(creds.BackendURL) != "" && !sameURL(creds.BackendURL, backendURL) {
		return nil, fmt.Errorf("stored login is for %s; run `orun auth login --backend-url %s`", creds.BackendURL, backendURL)
	}
	client := NewBackendClient(backendURL, version)
	resp, err := client.Refresh(ctx, creds.RefreshToken)
	if err != nil {
		return nil, err
	}
	updated := sessionResponseToCredentials(resp, backendURL)
	updated.RefreshToken = creds.RefreshToken
	updated.RefreshTokenExpiry = creds.RefreshTokenExpiry
	if err := SaveSession(updated); err != nil {
		return nil, err
	}
	return updated, nil
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

func (c *BackendClient) doJSON(ctx context.Context, method, path string, headers map[string]string, body interface{}, out interface{}) error {
	data, _, err := c.do(ctx, method, path, headers, body)
	if err != nil {
		return err
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode backend response: %w", err)
	}
	return nil
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
		var apiErr APIError
		if json.Unmarshal(data, &apiErr) == nil && apiErr.Message != "" {
			return nil, resp.StatusCode, &apiErr
		}
		return nil, resp.StatusCode, &APIError{Code: fmt.Sprintf("HTTP_%d", resp.StatusCode), Message: strings.TrimSpace(string(data))}
	}
	return data, resp.StatusCode, nil
}

func sessionResponseToCredentials(resp *SessionResponse, backendURL string) *Credentials {
	return &Credentials{
		AccessToken:         resp.AccessToken,
		AccessTokenExpiry:   resp.ExpiresAt,
		RefreshToken:        resp.RefreshToken,
		RefreshTokenExpiry:  resp.RefreshExpiresAt,
		GitHubLogin:         resp.GitHubLogin,
		AllowedNamespaceIDs: append([]string(nil), resp.AllowedNamespaceIDs...),
		BackendURL:          backendURL,
	}
}

func credentialsFromFragment(fragment, backendURL string) (*Credentials, error) {
	values, err := url.ParseQuery(strings.TrimPrefix(fragment, "#"))
	if err != nil {
		return nil, fmt.Errorf("parse loopback callback: %w", err)
	}
	allowedRaw := values.Get("allowedNamespaceIds")
	allowed := []string{}
	if strings.TrimSpace(allowedRaw) != "" {
		if err := json.Unmarshal([]byte(allowedRaw), &allowed); err != nil {
			return nil, fmt.Errorf("parse allowed namespace IDs: %w", err)
		}
	}
	accessToken := values.Get("sessionToken")
	if accessToken == "" {
		return nil, fmt.Errorf("missing session token in loopback callback")
	}
	return &Credentials{
		AccessToken:         accessToken,
		AccessTokenExpiry:   jwtExpiry(accessToken),
		RefreshToken:        values.Get("refreshToken"),
		RefreshTokenExpiry:  values.Get("refreshExpiresAt"),
		GitHubLogin:         values.Get("githubLogin"),
		AllowedNamespaceIDs: allowed,
		BackendURL:          backendURL,
	}, nil
}

func randomNonce(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	return hex.EncodeToString(b), nil
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
