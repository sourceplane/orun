package remotestate

import (
	"net/http"
	"testing"
)

// TestClientTransportTuned asserts the state client's transport opts into HTTP/2
// and a pool sized for the parallel object uploader, and that both the default
// and log clients share one transport so the connection pool is shared.
func TestClientTransportTuned(t *testing.T) {
	c := NewClient("https://example.test", "test", NewStaticTokenSource("tok"))

	tr, ok := c.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("httpClient.Transport = %T, want *http.Transport", c.httpClient.Transport)
	}
	if !tr.ForceAttemptHTTP2 {
		t.Errorf("ForceAttemptHTTP2 = false, want true (a custom DialContext otherwise disables HTTP/2)")
	}
	if tr.MaxIdleConnsPerHost != maxIdleConnsPerHost {
		t.Errorf("MaxIdleConnsPerHost = %d, want %d", tr.MaxIdleConnsPerHost, maxIdleConnsPerHost)
	}
	if tr.Proxy == nil {
		t.Errorf("Proxy = nil, want http.ProxyFromEnvironment (else HTTPS_PROXY is bypassed)")
	}
	if tr.IdleConnTimeout == 0 {
		t.Errorf("IdleConnTimeout = 0, want a bounded idle timeout")
	}

	// The log client must reuse the same transport so uploads share the pool.
	logTr, ok := c.logClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("logClient.Transport = %T, want *http.Transport", c.logClient.Transport)
	}
	if tr != logTr {
		t.Errorf("httpClient and logClient use different transports; want a shared pool")
	}
}
