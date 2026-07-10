package attach

import (
	"errors"
	"sync"
	"testing"

	"github.com/sourceplane/orun/internal/agent"
)

// TestVerdictRaceThreeHeads is the AL5 adversarial case: three heads answer
// one approval simultaneously; exactly one verdict wins and the rest get the
// not-pending ack. The winner's attribution is what lands in the log
// (design §2.2, risk R3).
func TestVerdictRaceThreeHeads(t *testing.T) {
	srv, _, resCh, store := startInteractive(t, "as_race1")
	defer func() { <-resCh }()

	h0, _ := srv.Attach(-1, "tui", "usr_a")
	r0 := newHeadReader(h0)
	r0.waitFor(t, "live", func(f Frame) bool { return f.T == TLive })

	// Ask for a gated tool.
	if err := h0.Steer("/ask contract_propose"); err != nil {
		t.Fatal(err)
	}
	ask := r0.waitFor(t, "ask", eventKind("approval_requested"))
	reqID, _ := ask.Payload["requestId"].(string)

	// Three heads, each with a distinct principal, answer at once.
	heads := []*HeadConn{h0}
	for _, p := range []string{"usr_b", "usr_c"} {
		h, err := srv.Attach(-1, "console", p)
		if err != nil {
			t.Fatal(err)
		}
		heads = append(heads, h)
	}
	var wg sync.WaitGroup
	results := make([]error, len(heads))
	for i, h := range heads {
		wg.Add(1)
		go func(i int, h *HeadConn) {
			defer wg.Done()
			results[i] = h.Verdict(reqID, true, "ok")
		}(i, h)
	}
	wg.Wait()

	wins, notPending := 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, agent.ErrNotPending):
			notPending++
		default:
			t.Fatalf("unexpected verdict error: %v", err)
		}
	}
	if wins != 1 || notPending != len(heads)-1 {
		t.Fatalf("verdict race: wins=%d notPending=%d (want 1 / %d)", wins, notPending, len(heads)-1)
	}

	// Exactly one approval_resolved is sealed, attributed to whoever won.
	res := r0.waitFor(t, "resolution", eventKind("approval_resolved"))
	winner, _ := res.Payload["principal"].(string)
	if winner == "" {
		t.Fatalf("resolution not attributed: %+v", res.Payload)
	}
	_ = store

	if err := h0.Steer("/done"); err != nil {
		t.Fatal(err)
	}
}

// TestManyHeadsAttachDetachSoak stresses the multi-head machinery: heads
// churn in and out while the session streams, and the survivor still sees a
// coherent conversation to terminal — the frame/queue invariants hold under
// attach/detach load (risk R4/R5, the runtime half).
func TestManyHeadsAttachDetachSoak(t *testing.T) {
	srv, _, resCh, _ := startInteractive(t, "as_soak1")

	survivor, _ := srv.Attach(-1, "tui", "usr_keep")
	sr := newHeadReader(survivor)
	sr.waitFor(t, "live", func(f Frame) bool { return f.T == TLive })

	// Churn: attach a head, read a few frames, detach — repeatedly, while
	// steering the session forward.
	for i := 0; i < 20; i++ {
		h, err := srv.Attach(-1, "console", "usr_churn")
		if err != nil {
			t.Fatalf("attach %d: %v", i, err)
		}
		// Drain a couple frames without blocking on a slow producer.
		for j := 0; j < 3; j++ {
			if _, ok := h.TryRecv(); !ok {
				break
			}
		}
		h.Detach()
		if err := survivor.Steer("tick"); err != nil {
			t.Fatalf("steer %d: %v", i, err)
		}
		sr.waitFor(t, "echo", func(f Frame) bool {
			return f.T == TEvent && f.Kind == "message_agent"
		})
	}

	if err := survivor.Steer("/done"); err != nil {
		t.Fatal(err)
	}
	res := <-resCh
	if res.Outcome.Status != "completed" {
		t.Fatalf("soak outcome = %+v", res.Outcome)
	}
	sr.waitFor(t, "bye", func(f Frame) bool { return f.T == TBye })
}
