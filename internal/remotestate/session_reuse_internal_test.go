package remotestate

import (
	"context"
	"testing"
	"time"
)

// A token read from the session store serves requests for sessionReuseFor
// without the store being consulted again (specs/orun-sync-efficiency F10):
// with no store at all, a remembered token is still returned until it ages,
// then the store is asked and its absence surfaces.
func TestSessionTokenSource_ReusesTheTokenItRead(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ORUN_CREDENTIAL_STORE", "file")
	s := &SessionTokenSource{BackendURL: "https://example.invalid"}
	s.remember("tok", time.Now().Add(time.Hour))
	got, err := s.Token(context.Background())
	if err != nil || got != "tok" {
		t.Fatalf("the remembered token serves: %q %v", got, err)
	}
	s.mu.Lock()
	s.readAt = time.Now().Add(-2 * sessionReuseFor)
	s.mu.Unlock()
	if _, err := s.Token(context.Background()); err == nil {
		t.Fatal("an aged token must be re-read from the store, which does not exist here")
	}
}
