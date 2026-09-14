package statebackend

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sourceplane/orun/internal/remotestate"
)

// The step coordinate on a remote log append (orun-cloud saas-step-logs SL-R1).
//
// The claim that needed a test is the one about SIZE. A step's output is handed
// over as one block, but a block over ~900 KiB is split to stay under the
// server's per-chunk cap — and if every piece carried the step's terminal
// event, the server would see one `end` per megabyte instead of one per step.

type capturedAppend struct {
	Content string                     `json:"content"`
	Step    *remotestate.AppendLogStep `json:"step"`
}

func captureAppends(t *testing.T) (*RemoteStateBackend, *[]capturedAppend, func()) {
	t.Helper()
	var mu sync.Mutex
	got := make([]capturedAppend, 0, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/logs/") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var c capturedAppend
		if err := json.Unmarshal(body, &c); err != nil {
			t.Errorf("decode append body: %v", err)
		}
		mu.Lock()
		got = append(got, c)
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"seq": len(got) - 1},
			"meta": map[string]any{"requestId": "req_test"},
		})
	}))
	client := remotestate.NewClient(srv.URL, "test", remotestate.NewStaticTokenSource("t"))
	return NewRemoteStateBackend(client, "runner-1"), &got, srv.Close
}

func exit(n int) *int { return &n }

func TestAppendStepLogSendsTheCoordinate(t *testing.T) {
	backend, got, done := captureAppends(t)
	defer done()

	err := backend.AppendStepLog(context.Background(), "01J0000000000000000000ABCD", "api.production.deploy", "boom\n",
		&LogStep{StepID: "deploy", Index: 4, Event: LogStepEnd, Status: "failed", ExitCode: exit(1)})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if len(*got) != 1 {
		t.Fatalf("appends = %d, want 1", len(*got))
	}
	step := (*got)[0].Step
	if step == nil {
		t.Fatal("no step on the wire")
	}
	if step.StepID != "deploy" || step.Index != 4 || step.Event != "end" {
		t.Errorf("step = %+v, want deploy/4/end", step)
	}
	if step.Status != "failed" || step.ExitCode == nil || *step.ExitCode != 1 {
		t.Errorf("outcome = %q/%v, want failed/1", step.Status, step.ExitCode)
	}
}

// A runner that says nothing writes exactly the request it wrote before the
// coordinate existed — the server then serves that job as one whole-job step.
func TestAppendStepLogWithoutAStepOmitsTheKey(t *testing.T) {
	backend, got, done := captureAppends(t)
	defer done()

	if err := backend.AppendStepLog(context.Background(), "01J0000000000000000000ABCD", "j", "plain\n", nil); err != nil {
		t.Fatalf("append: %v", err)
	}
	if len(*got) != 1 || (*got)[0].Step != nil {
		t.Fatalf("want one unattributed append, got %+v", *got)
	}
}

func TestOnlyTheLastChunkOfAStepCarriesItsEnd(t *testing.T) {
	backend, got, done := captureAppends(t)
	defer done()

	// Two chunks' worth: the split is what this test exists for.
	big := strings.Repeat("x", logChunkSafeBytes+1024) + "\n"
	err := backend.AppendStepLog(context.Background(), "01J0000000000000000000ABCD", "j", big,
		&LogStep{StepID: "build", Index: 1, Event: LogStepEnd, Status: "succeeded", ExitCode: exit(0)})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if len(*got) < 2 {
		t.Fatalf("want the content split across chunks, got %d append(s)", len(*got))
	}

	for i, c := range (*got)[:len(*got)-1] {
		if c.Step == nil || c.Step.Event != "chunk" {
			t.Errorf("append %d: event = %v, want chunk", i, c.Step)
		}
		// An interior piece reports no outcome — the step has not ended.
		if c.Step != nil && (c.Step.Status != "" || c.Step.ExitCode != nil) {
			t.Errorf("append %d carried an outcome before the step ended: %+v", i, c.Step)
		}
	}

	last := (*got)[len(*got)-1].Step
	if last == nil || last.Event != "end" {
		t.Fatalf("last append = %+v, want the step's end", last)
	}
	if last.Status != "succeeded" || last.ExitCode == nil || *last.ExitCode != 0 {
		t.Errorf("terminal outcome = %q/%v, want succeeded/0", last.Status, last.ExitCode)
	}
	// Every piece is the same step, or the server would draw two.
	for i, c := range *got {
		if c.Step == nil || c.Step.StepID != "build" || c.Step.Index != 1 {
			t.Errorf("append %d: identity = %+v, want build/1", i, c.Step)
		}
	}
}
