package statebackend

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sourceplane/orun/internal/remotestate"
)

// The held tail read (`logs --follow`) carries the cursor and the wait; the
// plain one carries no wait (specs/orun-sync-efficiency P6).
func TestRemoteStateBackend_TailJobLogWait_AsksTheServerToHold(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"content":"line\n","nextSeq":4,"complete":false}}`))
	}))
	t.Cleanup(srv.Close)
	b := NewRemoteStateBackend(
		remotestate.NewClientWithScope(srv.URL, "test", stubToken{}, remotestate.Scope{OrgID: "o", ProjectID: "p"}), "r1")

	content, next, complete, err := b.TailJobLogWait(context.Background(), "run-1", "job", 3, 15)
	if err != nil || content != "line\n" || next != 4 || complete {
		t.Fatalf("held read: %q %d %v %v", content, next, complete, err)
	}
	if _, _, _, err := b.TailJobLog(context.Background(), "run-1", "job", 0); err != nil {
		t.Fatal(err)
	}
	if len(queries) != 2 || queries[0] != "fromSeq=3&wait=15" || queries[1] != "" {
		t.Fatalf("queries: %q", queries)
	}
}
