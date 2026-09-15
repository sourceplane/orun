package scaffold

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// The platform sink (orun-bootstrap-engine BE-O13).
//
// One property matters more than the rest, and it is not "events arrive": the
// door refuses a batch that starts ABOVE what it holds, because a console
// resuming from its last seq steps over a hole and never comes back. So a
// dropped event is not one missing line — it is every line after it, refused
// forever, in silence.
//
// These therefore drive the sink through the failures that would produce a
// hole: a batch that is not acknowledged, a partial success, a platform that is
// down for a while, and a backlog that cannot be held. In each case the
// question is the same — does the stream stay contiguous, or does it wedge?

type fakeDoor struct {
	mu sync.Mutex
	// held is what the door has: seq -> present. It enforces the real rule.
	held        map[int]bool
	highest     int
	batches     [][]PlatformBuildEvent
	failNext    int
	failWith    error
	gapRefusals int
}

func newFakeDoor() *fakeDoor { return &fakeDoor{held: map[int]bool{}} }

// AppendBuildEvents mirrors the shipped door: idempotent by seq, and refusing
// a batch that would leave a gap.
func (d *fakeDoor) AppendBuildEvents(
	_ context.Context, _ string, _ string, events []PlatformBuildEvent,
) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.failNext > 0 {
		d.failNext--
		err := d.failWith
		if err == nil {
			err = errors.New("platform unavailable")
		}
		return 0, err
	}
	if len(events) == 0 {
		return d.highest, nil
	}
	if events[0].Seq > d.highest+1 {
		d.gapRefusals++
		return 0, errors.New("seq would leave a gap")
	}
	d.batches = append(d.batches, append([]PlatformBuildEvent(nil), events...))
	for _, e := range events {
		d.held[e.Seq] = true
		if e.Seq > d.highest {
			d.highest = e.Seq
		}
	}
	return d.highest, nil
}

func (d *fakeDoor) seqs() []int {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]int, 0, len(d.held))
	for s := 1; s <= d.highest; s++ {
		if d.held[s] {
			out = append(out, s)
		}
	}
	return out
}

func (d *fakeDoor) contiguous(upTo int) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for s := 1; s <= upTo; s++ {
		if !d.held[s] {
			return false
		}
	}
	return true
}

func ev(seq int) Event {
	return Event{
		Seq:       seq,
		At:        "2026-09-15T10:00:00Z",
		Phase:     "03-infrastructure",
		State:     EventRunning,
		Narration: "Planning the data plane.",
	}
}

func quiet(string, ...any) {}

// emitN queues seq 1..n and closes, which forces the final flush.
func emitN(t *testing.T, s *PlatformSink, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		s.Emit(context.Background(), ev(i))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.Close(ctx)
}

func TestPlatformSinkDeliversEveryEventInOrder(t *testing.T) {
	d := newFakeDoor()
	s := NewPlatformSink(d, "org_1", "run_1", quiet)
	emitN(t, s, 250)

	if !d.contiguous(250) {
		t.Fatalf("stream is not contiguous: got %v", d.seqs())
	}
	if d.gapRefusals != 0 {
		t.Fatalf("door refused %d batches for gaps; the sender must never skip", d.gapRefusals)
	}
	// Batched, not one request per event: an hour's build is not a thousand
	// round trips.
	if len(d.batches) > 4 {
		t.Fatalf("250 events took %d batches, expected at most 4", len(d.batches))
	}
}

// THE PROPERTY THIS FILE EXISTS FOR. A batch that is not acknowledged must be
// re-sent, not skipped — skipping it wedges every batch after it.
func TestPlatformSinkResendsAnUnacknowledgedBatchRatherThanSkipping(t *testing.T) {
	d := newFakeDoor()
	d.failNext = 6 // more than one flush's worth of attempts
	s := NewPlatformSink(d, "org_1", "run_1", quiet)

	for i := 1; i <= 120; i++ {
		s.Emit(context.Background(), ev(i))
	}
	// Let the timer flush into the failures at least once.
	time.Sleep(3 * platformFlushEvery)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.Close(ctx)

	if !d.contiguous(120) {
		t.Fatalf("a failed send left a hole: got %v", d.seqs())
	}
	if d.gapRefusals != 0 {
		t.Fatalf("door refused %d batches for gaps after a failure", d.gapRefusals)
	}
}

// A retry that raced a partial success re-sends events the door already holds.
// That is allowed — the door is idempotent — and must not confuse the sender
// into starting the next batch above what was acknowledged.
func TestPlatformSinkToleratesReDeliveryWithoutLosingItsPlace(t *testing.T) {
	d := newFakeDoor()
	s := NewPlatformSink(d, "org_1", "run_1", quiet)
	emitN(t, s, 50)

	// Replay the whole run through a second sink onto the same door.
	s2 := NewPlatformSink(d, "org_1", "run_1", quiet)
	emitN(t, s2, 50)

	if !d.contiguous(50) {
		t.Fatalf("re-delivery broke the stream: got %v", d.seqs())
	}
	if d.gapRefusals != 0 {
		t.Fatalf("a replay was refused for a gap %d time(s)", d.gapRefusals)
	}
}

// A build that cannot report must still be a build. Nothing here may block or
// fail the thing it is reporting on.
func TestPlatformSinkNeverBlocksTheBuild(t *testing.T) {
	d := newFakeDoor()
	d.failNext = 1 << 30 // permanently down
	s := NewPlatformSink(d, "org_1", "run_1", quiet)

	done := make(chan struct{})
	go func() {
		for i := 1; i <= 2000; i++ {
			s.Emit(context.Background(), ev(i))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Emit blocked the build while the platform was down")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	s.Close(ctx)
}

// Past the backlog bound the sink STOPS rather than dropping from the middle.
// Dropping would leave a hole the door refuses forever, in silence; stopping is
// visible and leaves the build untouched.
func TestPlatformSinkStopsRatherThanLeavingAHole(t *testing.T) {
	d := newFakeDoor()
	// Down long enough to build a backlog past the bound...
	d.failNext = 1 << 30
	var logged int
	s := NewPlatformSink(d, "org_1", "run_1", func(string, ...any) { logged++ })

	for i := 1; i <= platformMaxBacklog+200; i++ {
		s.Emit(context.Background(), ev(i))
	}
	if logged == 0 {
		t.Fatal("the sink gave up silently; a feed that stops must say so")
	}

	// ...and then BACK UP. This is what the first version of this test missed:
	// with the door down for the whole run there is nothing to refuse, so a
	// sink that dropped its oldest events instead of stopping looked identical
	// to one that stopped. It is not identical — it is the hole this whole
	// file exists to forbid — and it only shows when somebody is listening.
	d.mu.Lock()
	d.failNext = 0
	d.mu.Unlock()
	for i := platformMaxBacklog + 201; i <= platformMaxBacklog+260; i++ {
		s.Emit(context.Background(), ev(i))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.Close(ctx)

	d.mu.Lock()
	refusals, highest := d.gapRefusals, d.highest
	d.mu.Unlock()
	if refusals != 0 {
		t.Fatalf("the sink sent past a hole %d time(s); the door refuses those forever", refusals)
	}
	// A stopped sink sends nothing further. A dropping one would resume from
	// seq %d-ish, which is exactly the batch the door refuses.
	if highest != 0 {
		t.Fatalf("a sink that gave up still sent events (door holds up to %d)", highest)
	}
}

// Close is the flush that gets the LAST events out. A build page whose stream
// ends at "running" cannot tell a finished build from a hung one.
func TestPlatformSinkCloseFlushesTheEnding(t *testing.T) {
	d := newFakeDoor()
	s := NewPlatformSink(d, "org_1", "run_1", quiet)
	s.Emit(context.Background(), ev(1))
	s.Emit(context.Background(), Event{Seq: 2, At: "2026-09-15T11:00:00Z", State: EventDone, Narration: "Built."})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.Close(ctx) // no sleep: Close must not depend on the timer having ticked

	if !d.contiguous(2) {
		t.Fatalf("Close did not flush the ending: got %v", d.seqs())
	}
}

func TestPlatformSinkCloseIsIdempotent(t *testing.T) {
	d := newFakeDoor()
	s := NewPlatformSink(d, "org_1", "run_1", quiet)
	s.Emit(context.Background(), ev(1))
	ctx := context.Background()
	s.Close(ctx)
	s.Close(ctx) // must not panic on a closed channel
}

// A local `orun new` has no platform and must not be made to care. `events.go`
// already says a nil sink is valid and means nobody is listening.
func TestPlatformSinkIsNilWithoutAPlatform(t *testing.T) {
	for _, c := range []struct {
		name   string
		client buildEventClient
		org    string
		run    string
	}{
		{"no client", nil, "org_1", "run_1"},
		{"no org", newFakeDoor(), "", "run_1"},
		{"no run", newFakeDoor(), "org_1", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := NewPlatformSink(c.client, c.org, c.run, quiet)
			if s != nil {
				t.Fatal("expected a nil sink")
			}
			// And every method is safe on it, because that is what "nil is
			// valid" has to mean at the call site.
			s.Emit(context.Background(), ev(1))
			s.Close(context.Background())
		})
	}
}

// The engine's Event and the wire shape are two types on purpose; this is the
// translation, asserted so a field added to one is a deliberate change to both.
func TestPlatformSinkCarriesEveryFieldTheDoorTakes(t *testing.T) {
	d := newFakeDoor()
	s := NewPlatformSink(d, "org_1", "run_1", quiet)
	s.Emit(context.Background(), Event{
		Seq:       1,
		At:        "2026-09-15T10:24:02Z",
		Phase:     "03-infrastructure",
		Step:      "apply",
		State:     EventWaiting,
		Narration: "Point acme.com's nameservers at Cloudflare.",
		Detail:    "terraform apply -auto-approve",
		Meta:      map[string]string{"elapsed": "21m"},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.Close(ctx)

	if len(d.batches) != 1 || len(d.batches[0]) != 1 {
		t.Fatalf("expected one event, got %v", d.batches)
	}
	got := d.batches[0][0]
	if got.Phase != "03-infrastructure" || got.Step != "apply" {
		t.Fatalf("phase/step lost: %+v", got)
	}
	if got.State != string(EventWaiting) {
		t.Fatalf("state lost: %q — `waiting` is the row state the console could never draw", got.State)
	}
	if got.Narration == "" || got.Detail == "" {
		t.Fatalf("narration or detail lost: %+v", got)
	}
	if got.Meta["elapsed"] != "21m" {
		t.Fatalf("meta lost: %+v", got.Meta)
	}
}
