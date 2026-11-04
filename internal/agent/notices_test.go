package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/agent/driver"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
)

// Body-reported events (preview_changed) land in the session log in order,
// and what BeforeEnd reports lands before the terminal state.
func TestRunLogsNoticesAndBeforeEnd(t *testing.T) {
	ctx := context.Background()
	store := objectstore.NewMemStore(objectstore.AlgoSHA256)
	brief, err := AssembleBrief(ctx, store, BriefInput{RunKind: nodes.RunKindInteractive})
	if err != nil {
		t.Fatal(err)
	}
	inputs := NewInputQueue()
	notices := NewNotices()
	var mu sync.Mutex
	var seen []SessionEvent
	logged := make(chan struct{}, 16)
	notices.Post(nodes.SessionEventPreviewChanged, map[string]any{"state": "starting", "port": 4173})

	done := make(chan RunResult, 1)
	go func() {
		res, err := Run(ctx, store, RunOptions{
			SessionID: "as_notices",
			Driver:    &driver.Stub{Interactive: true},
			Brief:     brief,
			Inputs:    inputs,
			Notices:   notices,
			BeforeEnd: func() {
				notices.Post(nodes.SessionEventPreviewChanged, map[string]any{"state": "stopped"})
			},
			Observe: func(e SessionEvent) {
				mu.Lock()
				seen = append(seen, e)
				mu.Unlock()
				if e.Kind == nodes.SessionEventPreviewChanged {
					logged <- struct{}{}
				}
			},
		})
		if err != nil {
			t.Errorf("run: %v", err)
		}
		done <- res
	}()
	waitLogged := func() {
		select {
		case <-logged:
		case <-time.After(5 * time.Second):
			t.Fatal("notice never logged")
		}
	}
	waitLogged()
	notices.Post(nodes.SessionEventPreviewChanged, map[string]any{"state": "ready", "port": 4173})
	waitLogged()
	if err := inputs.Steer("/done", "u_1"); err != nil {
		t.Fatal(err)
	}
	res := <-done
	if res.Outcome.Status != "completed" {
		t.Fatalf("outcome = %+v", res.Outcome)
	}

	mu.Lock()
	defer mu.Unlock()
	var previews []string
	for _, e := range seen {
		if e.Kind == nodes.SessionEventPreviewChanged {
			previews = append(previews, e.Payload["state"].(string))
		}
	}
	if got := len(previews); got != 3 || previews[0] != "starting" || previews[1] != "ready" || previews[2] != "stopped" {
		t.Fatalf("preview events = %v", previews)
	}
	last, beforeLast := seen[len(seen)-1], seen[len(seen)-2]
	if last.Kind != nodes.SessionEventStateChanged || beforeLast.Kind != nodes.SessionEventPreviewChanged {
		t.Fatalf("tail = %s, %s; want the stop before the terminal state", beforeLast.Kind, last.Kind)
	}
	for i, e := range seen {
		if e.Seq != i {
			t.Fatalf("seq %d at %d: notices must take the log's seq", e.Seq, i)
		}
	}
}

func TestNoticesPostNeverBlocks(t *testing.T) {
	n := NewNotices()
	for i := 0; i < 1000; i++ {
		n.Post(nodes.SessionEventPreviewChanged, map[string]any{"i": i})
	}
	if got := len(n.take()); got != 1000 {
		t.Fatalf("queued %d, want 1000", got)
	}
	var nilQueue *Notices
	if nilQueue.readyCh() != nil {
		t.Fatal("a nil queue must never be ready")
	}
}
