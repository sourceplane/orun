package remotestate

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A batched append (orun-cloud saas-live-runs LR8b) sends `entries` and never
// `content`, and believes only a `count` it was given.

func appendLogsServer(t *testing.T, data map[string]any, seen *map[string]any) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, seen)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "meta": map[string]any{"requestId": "req_test"}})
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, "test", NewStaticTokenSource("t"))
}

func TestAppendLogsSendsEntriesAndNoContent(t *testing.T) {
	var seen map[string]any
	c := appendLogsServer(t, map[string]any{"seq": 4, "count": 2}, &seen)
	n, err := c.AppendLogs(context.Background(), "01J0000000000000000000ABCD", "j", "runner-1",
		[]AppendLogEntry{{Content: "a\n"}, {Content: "", Step: &AppendLogStep{StepID: "s", Index: 0, Event: "start"}}})
	if err != nil || n != 2 {
		t.Fatalf("AppendLogs = %d, %v; want 2, nil", n, err)
	}
	if _, has := seen["content"]; has {
		t.Errorf("a batch must not carry content: %v", seen)
	}
	if entries, _ := seen["entries"].([]any); len(entries) != 2 || seen["runnerId"] != "runner-1" {
		t.Errorf("body = %v", seen)
	}
}

func TestAppendLogsWithoutACountIsNotABatch(t *testing.T) {
	var seen map[string]any
	c := appendLogsServer(t, map[string]any{"seq": 4}, &seen)
	n, err := c.AppendLogs(context.Background(), "01J0000000000000000000ABCD", "j", "runner-1", []AppendLogEntry{{Content: "a\n"}})
	if n != 0 || !errors.Is(err, ErrBatchedAppendUnsupported) {
		t.Fatalf("AppendLogs = %d, %v; want 0 and ErrBatchedAppendUnsupported", n, err)
	}
}

func TestAppendLogsReportsAPartWayStore(t *testing.T) {
	var seen map[string]any
	c := appendLogsServer(t, map[string]any{"seq": 0, "count": 1}, &seen)
	n, err := c.AppendLogs(context.Background(), "01J0000000000000000000ABCD", "j", "runner-1",
		[]AppendLogEntry{{Content: "a\n"}, {Content: "b\n"}})
	if n != 1 || err == nil || errors.Is(err, ErrBatchedAppendUnsupported) || !strings.Contains(err.Error(), "1 of 2") {
		t.Fatalf("AppendLogs = %d, %v; want 1 and a part-way error", n, err)
	}
}
