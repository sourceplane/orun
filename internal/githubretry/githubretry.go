// Package githubretry is when a GitHub API READ is worth asking again.
//
// A build polls GitHub for an hour at a time — a landing waiting on CI, a
// convergence watch — and one dropped connection in that hour ended it. The
// first console build of a product to get through two phases died in phase
// 02's watch on
//
//	GET …/actions/runs?…: read tcp …->140.82.114.5:443: read: connection
//	reset by peer
//
// with its pull request already merged and its convergence running fine.
//
// READS ONLY. A write that failed in transit may have happened: a merge
// retried after its response was lost answers "not mergeable", and a created
// repository answers "already exists". Those callers decide for themselves.
package githubretry

import (
	"context"
	"net/http"
	"time"
)

// Backoff is the wait before each retry; its length is how many retries a
// read gets. A variable so tests do not sleep.
var Backoff = []time.Duration{1 * time.Second, 3 * time.Second, 8 * time.Second}

// Transient reports whether a read that ended this way should be asked again:
// the connection failed (reset, timeout, EOF — anything below HTTP), or GitHub
// answered 429 or a 5xx. Never once the caller's own context is done: that is
// the caller giving up, not the network.
func Transient(ctx context.Context, status int, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	if err != nil && status == 0 {
		return true
	}
	switch status {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// Do runs one request through attempt, asking again while it is a GET and
// Transient says so, up to len(Backoff) retries.
func Do(ctx context.Context, method string, attempt func() (int, error)) (int, error) {
	for i := 0; ; i++ {
		status, err := attempt()
		if err == nil || method != http.MethodGet || i >= len(Backoff) || !Transient(ctx, status, err) {
			return status, err
		}
		select {
		case <-ctx.Done():
			return status, err
		case <-time.After(Backoff[i]):
		}
	}
}
