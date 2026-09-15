package scaffold

import (
	"context"
	"log"
	"sync"
	"time"
)

// The platform sink (orun-bootstrap-engine BE-O13): the engine's own account of
// a build, delivered to the platform that is showing it to somebody.
//
// # Why this exists at all
//
// `events.go` says why the engine reports itself rather than letting a model
// relay a script's output. orun-cloud then built the other half — a store
// keyed (org, run, seq), an ingest door, a feed, and a `blocked` row state the
// console had never been able to draw — and every one of those has been sitting
// dark, because nothing sends. This is the sender.
//
// # THE ONE RULE THE DOOR IMPOSES, and the whole reason this is not a loop
//
// The door is idempotent by (org, run, seq), so re-sending is harmless and
// expected. What it REFUSES is a batch that starts above the next expected seq,
// because a console resuming from the last seq it holds steps straight over a
// hole and never comes back for it.
//
// That makes a dropped event permanent. Not "one missing line" — every batch
// after it is refused forever, because each one starts above what the platform
// holds. So this sink may never skip: it retries from its own high-water mark,
// and when it truly cannot keep up it STOPS and says so, rather than sending
// batches the door will refuse until the build ends.
//
// # It must never fail a build
//
// The build is the point; the feed is the report. A platform that is down, a
// token that expired, a queue that filled — none of them is a reason to stop
// writing somebody's product. Every failure here degrades to "the build page is
// behind, or incomplete", logged once, and the bootstrap runs on.

// buildEventClient is the platform call this sink needs, and nothing else.
// An interface rather than *remotestate.Client so `internal/scaffold` keeps no
// dependency on the platform client — the engine does not know it is hosted.
type buildEventClient interface {
	AppendBuildEvents(ctx context.Context, org, runID string, events []PlatformBuildEvent) (highestHeld int, err error)
}

// PlatformBuildEvent is the wire shape, mirrored here so this package exports
// what a caller must adapt to without importing the platform client.
type PlatformBuildEvent struct {
	Seq       int
	At        string
	Phase     string
	Step      string
	State     string
	Narration string
	Detail    string
	Meta      map[string]string
}

// Tunables. Not configurable: every one of them is a consequence of the door's
// own limits, and a caller that could raise them could only break the stream.
const (
	// The door caps a batch at 500. 100 keeps a flush small enough to retry
	// cheaply and large enough that an hour's build is not a thousand requests.
	platformBatchSize = 100
	// A build is watched live. Two seconds is under the console's own ten, so
	// the page is never waiting on this.
	platformFlushEvery = 2 * time.Second
	// How much unsent backlog to hold before giving up. A bootstrap emits a few
	// hundred events in an hour; 5000 is far past any real run and is here to
	// bound memory in a pathological one, not to be reached.
	platformMaxBacklog = 5000
	// Attempts per batch before backing off to the next flush. The events are
	// still held, so this is not a drop — it is how long one flush tries.
	platformAttempts = 3
)

// PlatformSink delivers the engine's events to the platform's build stream.
//
// Construct with NewPlatformSink and always `defer sink.Close(ctx)`: Close is
// the flush that gets the LAST events out, and a build whose final `done` never
// arrived leaves a build page that looks stuck forever.
type PlatformSink struct {
	client buildEventClient
	org    string
	runID  string
	logf   func(string, ...any)

	mu      sync.Mutex
	pending []PlatformBuildEvent
	// held is the highest seq the platform has acknowledged. The next batch
	// must start at or below held+1, which is why this is the sender's state
	// and not the transport's.
	held int
	// broken latches. Once the stream has a hole in it nothing can close, every
	// further send would be refused, so the sink stops and says so once.
	broken bool

	stop chan struct{}
	done chan struct{}
	once sync.Once
}

// NewPlatformSink starts a sink that flushes on a timer until Close.
//
// A nil client, an empty org or an empty run id returns nil — which is a valid
// EventSink meaning "nobody is listening", exactly as `events.go` says. A local
// `orun new` has no platform and must not be made to care.
func NewPlatformSink(client buildEventClient, org, runID string, logf func(string, ...any)) *PlatformSink {
	if client == nil || org == "" || runID == "" {
		return nil
	}
	if logf == nil {
		logf = log.Printf
	}
	s := &PlatformSink{
		client: client,
		org:    org,
		runID:  runID,
		logf:   logf,
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	go s.loop()
	return s
}

// Emit queues an event. Never blocks the build, never returns an error, and
// never panics on a nil sink — the three things a reporter owes the thing it
// is reporting on.
func (s *PlatformSink) Emit(_ context.Context, e Event) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.broken {
		return
	}
	if len(s.pending) >= platformMaxBacklog {
		// Dropping the oldest would leave a hole the door refuses forever;
		// dropping the newest would do the same to everything after it. So the
		// sink stops. The build page is short an ending, which is visible and
		// honest — unlike a stream that keeps being refused in silence.
		s.broken = true
		s.logf(
			"bootstrap events: backlog of %d unsent, giving up on the build feed for run %s "+
				"(the build itself is unaffected)", len(s.pending), s.runID,
		)
		return
	}
	s.pending = append(s.pending, PlatformBuildEvent{
		Seq:       e.Seq,
		At:        e.At,
		Phase:     e.Phase,
		Step:      e.Step,
		State:     string(e.State),
		Narration: e.Narration,
		Detail:    e.Detail,
		Meta:      e.Meta,
	})
}

func (s *PlatformSink) loop() {
	defer close(s.done)
	t := time.NewTicker(platformFlushEvery)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			s.flush(context.Background())
		case <-s.stop:
			return
		}
	}
}

// Close stops the timer and makes a final, blocking flush.
//
// The last events are the ones that matter most: a build page whose stream ends
// at "04-workers running" cannot tell a finished build from a hung one. The
// context bounds how long the build waits to report its own ending.
func (s *PlatformSink) Close(ctx context.Context) {
	if s == nil {
		return
	}
	s.once.Do(func() {
		close(s.stop)
		<-s.done
		s.flush(ctx)
	})
}

// flush sends what it can, in seq order, from the high-water mark.
func (s *PlatformSink) flush(ctx context.Context) {
	for {
		batch, ok := s.take()
		if !ok {
			return
		}
		if !s.send(ctx, batch) {
			// Put it back, in front of whatever arrived while we were sending.
			// The next flush starts here again — which is the whole point: a
			// batch that is not acknowledged is not behind us.
			s.unshift(batch)
			return
		}
	}
}

// take removes the next batch, skipping anything the platform already holds.
func (s *PlatformSink) take() ([]PlatformBuildEvent, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.broken {
		return nil, false
	}
	// Already acknowledged: a retry that overlapped a partial success leaves
	// these behind, and re-sending them is allowed but pointless.
	for len(s.pending) > 0 && s.pending[0].Seq <= s.held {
		s.pending = s.pending[1:]
	}
	if len(s.pending) == 0 {
		return nil, false
	}
	n := min(len(s.pending), platformBatchSize)
	batch := make([]PlatformBuildEvent, n)
	copy(batch, s.pending[:n])
	s.pending = s.pending[n:]
	return batch, true
}

func (s *PlatformSink) unshift(batch []PlatformBuildEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = append(batch, s.pending...)
}

// send delivers one batch, retrying a few times before leaving it for the next
// flush. Returns false when the batch was not acknowledged.
func (s *PlatformSink) send(ctx context.Context, batch []PlatformBuildEvent) bool {
	var lastErr error
	for attempt := range platformAttempts {
		if attempt > 0 {
			select {
			case <-time.After(time.Duration(attempt) * 250 * time.Millisecond):
			case <-ctx.Done():
				return false
			}
		}
		held, err := s.client.AppendBuildEvents(ctx, s.org, s.runID, batch)
		if err == nil {
			s.mu.Lock()
			if held > s.held {
				s.held = held
			}
			s.mu.Unlock()
			return true
		}
		lastErr = err
	}
	// Logged at most once per flush rather than per event: a platform that is
	// down for a minute would otherwise write thirty identical lines into the
	// middle of a build's own output.
	s.logf("bootstrap events: could not deliver seq %d-%d for run %s (%v); will retry",
		batch[0].Seq, batch[len(batch)-1].Seq, s.runID, lastErr)
	return false
}
