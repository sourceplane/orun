package remotestate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// tokenServer answers GitHub's token endpoint with the given handler per
// attempt (1-based) and counts the attempts.
func tokenServer(t *testing.T, answer func(attempt int32, w http.ResponseWriter)) (*OIDCTokenSource, *atomic.Int32) {
	t.Helper()
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		answer(attempts.Add(1), w)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", srv.URL+"/token")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "req-token")
	src := NewOIDCTokenSource("orun")
	src.tokenRetryWaits = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	return src, &attempts
}

func writeToken(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"value": "test-oidc-token"})
}

func TestOIDCToken_RetriesATimeout(t *testing.T) {
	src, attempts := tokenServer(t, func(attempt int32, w http.ResponseWriter) {
		if attempt == 1 {
			time.Sleep(200 * time.Millisecond) // past the client's timeout below
		}
		writeToken(w)
	})
	src.httpClient = &http.Client{Timeout: 50 * time.Millisecond}

	token, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token != "test-oidc-token" || attempts.Load() != 2 {
		t.Fatalf("token %q after %d attempts, want test-oidc-token after 2", token, attempts.Load())
	}
}

func TestOIDCToken_Retries5xxAnd429(t *testing.T) {
	src, attempts := tokenServer(t, func(attempt int32, w http.ResponseWriter) {
		switch attempt {
		case 1:
			w.WriteHeader(http.StatusServiceUnavailable)
		case 2:
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			writeToken(w)
		}
	})
	token, err := src.Token(context.Background())
	if err != nil || token != "test-oidc-token" || attempts.Load() != 3 {
		t.Fatalf("token %q err %v after %d attempts, want the token after 3", token, err, attempts.Load())
	}
}

func TestOIDCToken_GivesUpAfterTheBoundedAttempts(t *testing.T) {
	src, attempts := tokenServer(t, func(_ int32, w http.ResponseWriter) {
		w.WriteHeader(http.StatusBadGateway)
	})
	_, err := src.Token(context.Background())
	if err == nil {
		t.Fatal("Token succeeded against an endpoint that always answers 502")
	}
	if attempts.Load() != 4 {
		t.Fatalf("%d attempts, want 4", attempts.Load())
	}
	if !strings.Contains(err.Error(), "status 502") || !strings.Contains(err.Error(), "after 4 attempts") {
		t.Fatalf("error %q should name the status and the attempts", err)
	}
}

func TestOIDCToken_DoesNotRetryARefusal(t *testing.T) {
	src, attempts := tokenServer(t, func(_ int32, w http.ResponseWriter) {
		w.WriteHeader(http.StatusForbidden)
	})
	_, err := src.Token(context.Background())
	if err == nil || attempts.Load() != 1 {
		t.Fatalf("err %v after %d attempts, want a failure after 1", err, attempts.Load())
	}
	if strings.Contains(err.Error(), "attempts") {
		t.Fatalf("a single attempt should not report a count: %q", err)
	}
}

func TestOIDCToken_StopsWhenTheContextEnds(t *testing.T) {
	src, attempts := tokenServer(t, func(_ int32, w http.ResponseWriter) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	src.tokenRetryWaits = []time.Duration{time.Hour, time.Hour, time.Hour}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := src.Token(ctx)
	if err == nil {
		t.Fatal("Token succeeded against an endpoint that always answers 503")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Token waited %s after its context ended", elapsed)
	}
	if attempts.Load() != 1 {
		t.Fatalf("%d attempts, want 1 before the context ended", attempts.Load())
	}
}

// exchangeServer answers GitHub's token endpoint at once and Orunbase's
// exchange with the given handler per attempt (1-based), counting exchanges.
func exchangeServer(t *testing.T, answer func(attempt int32, w http.ResponseWriter)) (*OIDCTokenSource, *atomic.Int32) {
	t.Helper()
	var exchanges atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			writeToken(w)
			return
		}
		answer(exchanges.Add(1), w)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", srv.URL+"/token")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "req-token")
	src := NewOIDCExchangeSource("", srv.URL, "")
	src.tokenRetryWaits = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	return src, &exchanges
}

func writeExchanged(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{
		"accessToken": "sk_workflow_token",
		"tokenType":   "Bearer",
		"expiresAt":   "2099-01-01T00:00:00Z",
		"projectId":   "prj_resolved",
	}})
}

// The exchange with Orunbase times out too: main's identity-worker and
// web-console-next lanes failed there on 2026-10-23 (v2.86.3).
func TestOIDCExchange_RetriesATimeoutAndA5xx(t *testing.T) {
	src, exchanges := exchangeServer(t, func(attempt int32, w http.ResponseWriter) {
		switch attempt {
		case 1:
			time.Sleep(200 * time.Millisecond) // past the client's timeout below
			writeExchanged(w)
		case 2:
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			writeExchanged(w)
		}
	})
	src.httpClient = &http.Client{Timeout: 50 * time.Millisecond}

	token, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token != "sk_workflow_token" || exchanges.Load() != 3 {
		t.Fatalf("token %q after %d exchanges, want the workflow token after 3", token, exchanges.Load())
	}
}

func TestOIDCExchange_DoesNotRetryARefusal(t *testing.T) {
	src, exchanges := exchangeServer(t, func(_ int32, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "forbidden", "message": "repository not linked"}})
	})
	_, err := src.Token(context.Background())
	if err == nil || exchanges.Load() != 1 {
		t.Fatalf("err %v after %d exchanges, want a refusal after 1", err, exchanges.Load())
	}
	if !strings.Contains(err.Error(), "OIDC exchange rejected") {
		t.Fatalf("error %q should say the exchange was rejected", err)
	}
}

func TestOIDCExchange_GivesUpAfterTheBoundedAttempts(t *testing.T) {
	src, exchanges := exchangeServer(t, func(_ int32, w http.ResponseWriter) {
		w.WriteHeader(http.StatusBadGateway)
	})
	_, err := src.Token(context.Background())
	if err == nil || exchanges.Load() != 4 {
		t.Fatalf("err %v after %d exchanges, want a failure after 4", err, exchanges.Load())
	}
	if !strings.Contains(err.Error(), "after 4 attempts") {
		t.Fatalf("error %q should name the attempts", err)
	}
}
