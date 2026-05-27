package github

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

// DefaultRetryConfig is the default retry configuration for API calls.
var DefaultRetryConfig = RetryConfig{
	MaxRetries:      3,
	InitialDelay:    500 * time.Millisecond,
	MaxDelay:        10 * time.Second,
	JitterFactor:    0.25,
	RetryableStatus: []int{429, 500, 502, 503, 504},
}

// RetryConfig controls retry behaviour for HTTP requests.
type RetryConfig struct {
	// MaxRetries is the maximum number of retry attempts (0 = no retry).
	MaxRetries int
	// InitialDelay is the base delay before the first retry.
	InitialDelay time.Duration
	// MaxDelay clamps the total delay per attempt.
	MaxDelay time.Duration
	// JitterFactor adds randomness: actualDelay = delay * (1 ± jitterFactor).
	// Set to 0 for pure exponential backoff without jitter.
	JitterFactor float64
	// RetryableStatus lists HTTP status codes that should trigger a retry.
	// 429 and 5xx are the sensible defaults.
	RetryableStatus []int
}

// retryDo wraps an HTTP handler with retry logic.
// It retries on:
//   - Network errors (connection refused, DNS failures, TLS errors)
//   - HTTP status codes listed in RetryableStatus
//   - Respects context cancellation
func (c *Client) retryDo(ctx context.Context, req *http.Request) (*http.Response, error) {
	cfg := c.retryConfig
	if cfg.MaxRetries == 0 {
		cfg = DefaultRetryConfig
	}

	var (
		lastErr   error
		lastResp  *http.Response
		attempt   int
	)

	for attempt = 0; attempt <= cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			// Check context before retry
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("request cancelled before retry %d: %w", attempt, err)
			}

			delay := backoffDuration(attempt, cfg)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, fmt.Errorf("request cancelled during retry delay %d: %w", attempt, ctx.Err())
			}
		}

		// Clone the request body for each retry attempt
		// (the body is consumed after the first request)
		var bodyClone *http.Request
		if req.Body != nil && req.GetBody != nil {
			clonedReq, _ := http.NewRequestWithContext(ctx, req.Method, req.URL.String(), nil)
			if clonedReq != nil {
				body, _ := req.GetBody()
				clonedReq.Body = body
				clonedReq.Header = req.Header.Clone()
				clonedReq.ContentLength = req.ContentLength
				bodyClone = clonedReq
			}
		}

		retryReq := req
		if bodyClone != nil {
			retryReq = bodyClone
		}

		resp, err := c.http.Do(retryReq)

		if err != nil {
			// Network-level error — always retryable
			lastErr = err
			lastResp = nil
			if attempt < cfg.MaxRetries {
				continue
			}
			break
		}

		// Check if the status is retryable
		if cfg.isRetryableStatus(resp.StatusCode) {
			resp.Body.Close()
			lastErr = fmt.Errorf("unexpected status %d", resp.StatusCode)
			lastResp = nil
			if attempt < cfg.MaxRetries {
				continue
			}
			break
		}

		// Success (2xx or 3xx)
		return resp, nil
	}

	if lastResp != nil {
		lastResp.Body.Close()
	}
	if lastErr != nil {
		return nil, fmt.Errorf("request failed after %d retries: %w", attempt, lastErr)
	}
	return nil, fmt.Errorf("request failed after %d retries", attempt)
}

// isRetryableStatus checks if the HTTP status code should trigger a retry.
func (rc RetryConfig) isRetryableStatus(status int) bool {
	for _, s := range rc.RetryableStatus {
		if s == status {
			return true
		}
	}
	return false
}

// backoffDuration computes the delay before a retry attempt using
// exponential backoff with optional jitter.
//
//	delay = min(initialDelay * 2^(attempt-1), maxDelay)
//	with jitter: delay *= (1 ± jitterFactor)
func backoffDuration(attempt int, cfg RetryConfig) time.Duration {
	if cfg.InitialDelay <= 0 {
		cfg.InitialDelay = DefaultRetryConfig.InitialDelay
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = DefaultRetryConfig.MaxDelay
	}

	expDelay := float64(cfg.InitialDelay) * math.Pow(2, float64(attempt-1))
	delay := time.Duration(math.Min(expDelay, float64(cfg.MaxDelay)))

	if cfg.JitterFactor > 0 {
		jitterRange := delay
		jitter := time.Duration(rand.Float64() * 2 * float64(jitterRange) * cfg.JitterFactor)
		jitter -= time.Duration(float64(jitterRange) * cfg.JitterFactor)
		delay += jitter
		if delay < time.Millisecond {
			delay = time.Millisecond
		}
	}

	return delay
}

// IsRetryableError returns true if the error message looks like a transient
// network error that could succeed on retry.
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	retryablePhrases := []string{
		"connection refused",
		"connection reset",
		"connection timed out",
		"no such host",
		"tls handshake timeout",
		"i/o timeout",
		"read: connection reset",
		"write: broken pipe",
		"use of closed network connection",
		"server closed connection",
		"timeout awaiting response headers",
	}
	for _, phrase := range retryablePhrases {
		if strings.Contains(msg, phrase) {
			return true
		}
	}
	return false
}