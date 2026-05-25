package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

const (
	defaultBaseURL = "https://api.github.com"
	userAgent      = "orun"
)

// Client is a GitHub API client for artifact operations.
type Client struct {
	repo        string // "owner/repo"
	token       string
	baseURL     string
	http        *http.Client
	retryConfig RetryConfig
}

// NewClient creates a new GitHub API client for the given repository.
// Token resolution order:
//  1. GITHUB_TOKEN env var
//  2. GH_TOKEN env var
//  3. gh auth token via subprocess
//  4. Provided explicitToken
func NewClient(ctx context.Context, repo string, opts ...ClientOption) (*Client, error) {
	if repo == "" {
		return nil, fmt.Errorf("repository is required (format: owner/repo)")
	}
	if !strings.Contains(repo, "/") {
		return nil, fmt.Errorf("invalid repository format: %q (expected owner/repo)", repo)
	}

	c := &Client{
		repo:    repo,
		baseURL: defaultBaseURL,
		http:    http.DefaultClient,
	}

	// Apply options
	for _, opt := range opts {
		opt(c)
	}

	// Resolve token if not already set via option
	if c.token == "" {
		token, err := resolveToken(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve GitHub token: %w", err)
		}
		c.token = token
	}

	return c, nil
}

// ClientOption configures the GitHub client.
type ClientOption func(*Client)

// WithToken sets an explicit GitHub token.
func WithToken(token string) ClientOption {
	return func(c *Client) {
		c.token = token
	}
}

// WithBaseURL sets a custom API base URL (for GitHub Enterprise).
func WithBaseURL(baseURL string) ClientOption {
	return func(c *Client) {
		c.baseURL = strings.TrimRight(baseURL, "/")
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		c.http = httpClient
	}
}

// WithRetryConfig sets a custom retry configuration for API calls.
func WithRetryConfig(cfg RetryConfig) ClientOption {
	return func(c *Client) {
		c.retryConfig = cfg
	}
}

// resolveToken resolves a GitHub token from environment or gh CLI.
func resolveToken(ctx context.Context) (string, error) {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token, nil
	}
	if token := os.Getenv("GH_TOKEN"); token != "" {
		return token, nil
	}

	// Try gh auth token
	cmd := exec.CommandContext(ctx, "gh", "auth", "token")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("no token found in GITHUB_TOKEN, GH_TOKEN, or gh auth token")
	}
	return strings.TrimSpace(string(output)), nil
}

// apiURL returns the full URL for a given API path.
func (c *Client) apiURL(path string) string {
	return fmt.Sprintf("%s%s", c.baseURL, path)
}

// newRequest creates an authenticated GET request.
func (c *Client) newRequest(ctx context.Context, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	// Needed for artifact download
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	return req, nil
}

// doRequest performs an HTTP request and returns the response (body already checked).
func (c *Client) doRequest(req *http.Request) (*http.Response, error) {
	resp, err := c.retryDo(req.Context(), req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected status %d for %s", resp.StatusCode, req.URL)
	}
	return resp, nil
}

// newPostRequest creates an authenticated POST request with a JSON body.
func (c *Client) newPostRequest(ctx context.Context, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	return req, nil
}

// newPutRequest creates an authenticated PUT request with a byte body.
func (c *Client) newPutRequest(ctx context.Context, url, contentType string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", userAgent)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	return req, nil
}