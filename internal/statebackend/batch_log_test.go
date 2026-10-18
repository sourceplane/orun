package statebackend

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sourceplane/orun/internal/remotestate"
)

// A batched log append (orun-cloud saas-live-runs LR8b). A streaming step sends
// a start, a chunk every couple of seconds and an end; one request each fell
// behind the job on production. The claims: a job's queue goes in as few
// requests as the server's bounds allow, each chunk keeps its own coordinate,
// a part-way failure says how far it got, and a server from before LR8b gets
// single appends.

type batchServer struct {
	mu sync.Mutex
	// old answers a body without `content` the way a server before LR8b does.
	old bool
	// storeAtMost caps what one batch stores (0: all of it).
	storeAtMost int
	batches     [][]capturedAppend
	singles     []capturedAppend
}

func (s *batchServer) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Content *string                    `json:"content"`
			Step    *remotestate.AppendLogStep `json:"step"`
			Entries []capturedAppend           `json:"entries"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("decode append body: %v", err)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		reply := func(data map[string]any) {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "meta": map[string]any{"requestId": "req_test"}})
		}
		if req.Content == nil {
			if s.old || req.Entries == nil {
				w.Header().Set("content-type", "application/json")
				w.WriteHeader(http.StatusUnprocessableEntity)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
					"code": "validation_failed", "message": "Validation failed",
					"details": map[string]any{"fields": map[string]any{"content": []string{"Required; string chunk"}}},
				}})
				return
			}
			n := len(req.Entries)
			if s.storeAtMost > 0 && n > s.storeAtMost {
				n = s.storeAtMost
			}
			s.batches = append(s.batches, req.Entries[:n])
			reply(map[string]any{"seq": 0, "count": n})
			return
		}
		s.singles = append(s.singles, capturedAppend{Content: *req.Content, Step: req.Step})
		reply(map[string]any{"seq": len(s.singles) - 1})
	}
}

func newBatchBackend(t *testing.T, s *batchServer) *RemoteStateBackend {
	t.Helper()
	srv := httptest.NewServer(s.handler(t))
	t.Cleanup(srv.Close)
	client := remotestate.NewClient(srv.URL, "test", remotestate.NewStaticTokenSource("t"))
	return NewRemoteStateBackend(client, "runner-1")
}

const batchRun = "01J0000000000000000000ABCD"

func stepChunks() []LogChunk {
	return []LogChunk{
		{Content: "", Step: &LogStep{StepID: "build", Index: 2, Event: LogStepStart}},
		{Content: "compiling…\n", Step: &LogStep{StepID: "build", Index: 2, Event: LogStepChunk}},
		{Content: "done\n", Step: &LogStep{StepID: "build", Index: 2, Event: LogStepEnd, Status: "succeeded", ExitCode: exit(0)}},
	}
}

func TestAppendStepLogsSendsAJobsQueueInOneRequest(t *testing.T) {
	s := &batchServer{}
	n, err := newBatchBackend(t, s).AppendStepLogs(context.Background(), batchRun, "j", stepChunks())
	if err != nil || n != 3 {
		t.Fatalf("AppendStepLogs = %d, %v; want 3, nil", n, err)
	}
	if len(s.batches) != 1 || len(s.singles) != 0 {
		t.Fatalf("requests: %d batch(es), %d single(s); want one batch", len(s.batches), len(s.singles))
	}
	got := s.batches[0]
	if len(got) != 3 || got[1].Content != "compiling…\n" {
		t.Fatalf("entries = %+v", got)
	}
	for i, ev := range []string{"start", "chunk", "end"} {
		if got[i].Step == nil || got[i].Step.Event != ev || got[i].Step.StepID != "build" || got[i].Step.Index != 2 {
			t.Errorf("entry %d step = %+v, want build/2/%s", i, got[i].Step, ev)
		}
	}
	if got[2].Step.Status != "succeeded" || got[2].Step.ExitCode == nil || *got[2].Step.ExitCode != 0 {
		t.Errorf("the end lost its outcome: %+v", got[2].Step)
	}
}

func TestAppendStepLogsSplitsAtTheServersBounds(t *testing.T) {
	s := &batchServer{}
	many := make([]LogChunk, remotestate.MaxAppendLogEntries+6)
	for i := range many {
		many[i] = LogChunk{Content: "line\n"}
	}
	n, err := newBatchBackend(t, s).AppendStepLogs(context.Background(), batchRun, "j", many)
	if err != nil || n != len(many) {
		t.Fatalf("AppendStepLogs = %d, %v", n, err)
	}
	if len(s.batches) != 2 || len(s.batches[0]) != remotestate.MaxAppendLogEntries || len(s.batches[1]) != 6 {
		t.Fatalf("batches = %d (%d, …); want 64 then 6", len(s.batches), len(s.batches[0]))
	}

	s = &batchServer{}
	big := strings.Repeat("x", 600*1024)
	n, err = newBatchBackend(t, s).AppendStepLogs(context.Background(), batchRun, "j", []LogChunk{{Content: big}, {Content: big}})
	if err != nil || n != 2 || len(s.batches) != 2 {
		t.Fatalf("two 600 KiB chunks: %d, %v, %d batch(es); want two requests", n, err, len(s.batches))
	}
}

func TestAppendStepLogsSplitsAnOversizedEndAndCountsItOnce(t *testing.T) {
	s := &batchServer{}
	big := strings.Repeat("x", logChunkSafeBytes+1024) + "\n"
	n, err := newBatchBackend(t, s).AppendStepLogs(context.Background(), batchRun, "j",
		[]LogChunk{{Content: big, Step: &LogStep{StepID: "build", Index: 1, Event: LogStepEnd, Status: "failed", ExitCode: exit(1)}}})
	if err != nil || n != 1 {
		t.Fatalf("AppendStepLogs = %d, %v; want 1, nil", n, err)
	}
	var pieces []capturedAppend
	for _, b := range s.batches {
		pieces = append(pieces, b...)
	}
	if len(pieces) != 2 {
		t.Fatalf("pieces = %d, want 2", len(pieces))
	}
	if pieces[0].Step.Event != "chunk" || pieces[1].Step.Event != "end" || pieces[1].Step.Status != "failed" {
		t.Errorf("only the last piece may carry the end: %+v, %+v", pieces[0].Step, pieces[1].Step)
	}
}

func TestAppendStepLogsSaysHowFarAPartWayFailureGot(t *testing.T) {
	s := &batchServer{storeAtMost: 2}
	n, err := newBatchBackend(t, s).AppendStepLogs(context.Background(), batchRun, "j", stepChunks())
	if n != 2 || err == nil {
		t.Fatalf("AppendStepLogs = %d, %v; want 2 and an error", n, err)
	}
	if errors.Is(err, remotestate.ErrBatchedAppendUnsupported) {
		t.Fatalf("a part-way store is not an unsupported batch: %v", err)
	}
}

func TestAppendStepLogsCountsAnEmptyChunkItHasNothingToSendFor(t *testing.T) {
	s := &batchServer{}
	chunks := []LogChunk{{Content: "a\n"}, {Content: ""}, {Content: "b\n"}, {Content: ""}}
	n, err := newBatchBackend(t, s).AppendStepLogs(context.Background(), batchRun, "j", chunks)
	if err != nil || n != 4 {
		t.Fatalf("AppendStepLogs = %d, %v; want 4, nil", n, err)
	}
	if len(s.batches) != 1 || len(s.batches[0]) != 2 {
		t.Fatalf("batches = %+v; want one with the two non-empty chunks", s.batches)
	}
}

func TestAppendStepLogsAnOldServerGetsSingleAppends(t *testing.T) {
	s := &batchServer{old: true}
	backend := newBatchBackend(t, s)
	n, err := backend.AppendStepLogs(context.Background(), batchRun, "j", stepChunks())
	if err != nil || n != 3 {
		t.Fatalf("AppendStepLogs = %d, %v; want 3, nil", n, err)
	}
	if len(s.singles) != 3 || s.singles[1].Content != "compiling…\n" || s.singles[2].Step.Event != "end" {
		t.Fatalf("singles = %+v; want the three chunks in order", s.singles)
	}

	// Remembered: the next call does not ask again.
	s.mu.Lock()
	s.old = false
	s.mu.Unlock()
	if _, err := backend.AppendStepLogs(context.Background(), batchRun, "j", stepChunks()); err != nil {
		t.Fatal(err)
	}
	if len(s.batches) != 0 || len(s.singles) != 6 {
		t.Fatalf("after a refusal: %d batch(es), %d single(s); want singles only", len(s.batches), len(s.singles))
	}
}

func TestCoordBackendAppendStepLogsIsTheInnerBackends(t *testing.T) {
	s := &batchServer{}
	coord := &CoordBackend{inner: newBatchBackend(t, s)}
	var _ BatchLogAppender = coord
	if n, err := coord.AppendStepLogs(context.Background(), batchRun, "j", stepChunks()); err != nil || n != 3 {
		t.Fatalf("AppendStepLogs = %d, %v", n, err)
	}
	if len(s.batches) != 1 {
		t.Fatalf("batches = %d, want 1", len(s.batches))
	}
}
