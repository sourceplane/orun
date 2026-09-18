package scaffold

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/sourceplane/orun/internal/actions"
	"github.com/sourceplane/orun/internal/objectstore"
)

// A WAIT NOBODY CAN SEE IS A HANG. cirrus asks for the GitHub connection with
// `waitSeconds: 600`, so an operator can fix it while the build holds. The
// preconditions sweep asked it — and waited the full ten minutes — before the
// run's first event, then phase 03's probe waited ten more: twenty minutes of a
// build page that said nothing, which read as a build that never started.
//
// The sweep now asks once. The waiting happens at the phase, AFTER the build
// has said what it is waiting on.
const consentBlueprint = `apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: bp
modules:
  - name: first
    mode: template
    files:
      one.md: "1"
phases:
  - name: 01-scaffold
    modules: [first]
    requires:
      probe:
        - id: github
          uses: orun.doctor/check@v1
          with:
            providers: [github]
            githubOwner: orunbase-demo
            waitSeconds: 600
`

const reason = "waiting for github for orunbase-demo (connected: adampullely) to be connected in workspace ws_1"

// consent answers the GitHub probe the way doctor/check does: at once when
// asked not to wait, and — when allowed to wait — with whatever the operator
// did in the meantime. It records what each call was allowed, and what the
// run had already said when the call was made.
type consent struct {
	mu       sync.Mutex
	sink     *CollectingSink
	arrives  bool // the operator fixes it while the build waits
	waits    []int
	saidThen [][]Event
}

func (c *consent) Run(_ context.Context, id string, in ActionInput) (map[string]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if id != "orun.doctor/check@v1" {
		return nil, nil
	}
	wait := 0
	if v, ok := in.Params["waitSeconds"]; ok {
		fmt.Sscan(fmt.Sprint(v), &wait)
	}
	c.waits = append(c.waits, wait)
	c.saidThen = append(c.saidThen, snapshot(c.sink))
	if wait > 0 && c.arrives {
		return map[string]string{"connected": "github"}, nil
	}
	return nil, &actions.PendingError{ID: "github", Pending: actions.Pending{Reason: reason}}
}

func runConsent(t *testing.T, arrives bool, narrate string) (*consent, []Event, error) {
	t.Helper()
	sink := &CollectingSink{}
	c := &consent{sink: sink, arrives: arrives}
	bp := consentBlueprint
	if narrate != "" {
		bp = strings.Replace(bp, "    modules: [first]\n", "    modules: [first]\n    narrate:\n      await: \""+narrate+"\"\n", 1)
	}
	_, err := Run(context.Background(), Options{
		Blueprint: []byte(bp), OutDir: t.TempDir(),
		Store: objectstore.NewMemStore(objectstore.AlgoSHA256), Actions: c,
		RunHooks: true, Events: sink, RunID: "run_test",
	})
	return c, snapshot(sink), err
}

// snapshot is what the sink holds right now.
func snapshot(s *CollectingSink) []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Event(nil), s.Events...)
}

func states(evs []Event) []string {
	var out []string
	for _, e := range evs {
		if e.Phase == "01-scaffold" {
			out = append(out, string(e.State))
		}
	}
	return out
}

func TestTheSweepAsksAProbeOnceAndDoesNotWait(t *testing.T) {
	c, _, _ := runConsent(t, false, "")
	if len(c.waits) == 0 || c.waits[0] != 0 {
		t.Fatalf("the sweep before the first event waited %v — a wait nobody can see", c.waits)
	}
	if len(c.saidThen[0]) != 0 {
		t.Fatalf("the sweep is meant to run before anything is said, but %d event(s) preceded it", len(c.saidThen[0]))
	}
}

// Before the probe is allowed to wait, the run has said it is waiting and on
// what — so the operator who has to click something is told while the build
// is still holding for them.
func TestTheBuildSaysWhatItIsWaitingOnBeforeItWaits(t *testing.T) {
	c, _, _ := runConsent(t, false, "")
	waited := -1
	for i, w := range c.waits {
		if w > 0 {
			waited = i
			break
		}
	}
	if waited < 0 {
		t.Fatalf("the phase never let the probe wait its declared 600s: %v", c.waits)
	}
	if c.waits[waited] != 600 {
		t.Fatalf("the phase-time wait must be the probe's own, got %d", c.waits[waited])
	}
	said := c.saidThen[waited]
	if got := states(said); len(got) < 2 || got[len(got)-1] != "waiting" {
		t.Fatalf("before waiting, the run had said %v — it must already be `waiting`", got)
	}
	last := said[len(said)-1]
	if last.Detail != reason {
		t.Errorf("the waiting event's detail = %q, want the probe's reason", last.Detail)
	}
	if last.Narration != "01-scaffold: "+reason {
		t.Errorf("an unauthored waiting line must name what it waits on, got %q", last.Narration)
	}
	if last.Meta["waitingOn"] != reason {
		t.Errorf("meta.waitingOn = %q, want the reason", last.Meta["waitingOn"])
	}
}

// Fixed while the build held: the page drew the phase as waiting on somebody,
// and nothing else the phase emits arrives until its hooks have finished — so
// the build says it has moved on.
func TestAProbeThatComesGoodWhileWaitingRetiresTheWait(t *testing.T) {
	_, evs, err := runConsent(t, true, "")
	if err != nil {
		t.Fatalf("the consent arrived during the wait; the run should go on: %v", err)
	}
	got := strings.Join(states(evs), ",")
	if got != "started,waiting,running,done" {
		t.Fatalf("01-scaffold said %s, want started,waiting,running,done", got)
	}
}

func TestAProbeStillPendingAfterTheWaitParksNamingIt(t *testing.T) {
	_, evs, err := runConsent(t, false, "")
	var parked *ParkedError
	if !errors.As(err, &parked) {
		t.Fatalf("want the run parked, got %v", err)
	}
	if parked.Reason != reason {
		t.Errorf("parked on %q, want the probe's reason", parked.Reason)
	}
	last := evs[len(evs)-1]
	if last.State != EventWaiting || last.Detail != reason {
		t.Fatalf("the last word must be the park, naming it; got %s %q", last.State, last.Detail)
	}
}

// An authored line can name it too.
func TestAnAuthoredWaitingLineCanNameWhatItWaitsOn(t *testing.T) {
	_, evs, _ := runConsent(t, false, "Connect GitHub: {{ .meta.waitingOn }}")
	for _, e := range evs {
		if e.State == EventWaiting {
			if e.Narration != "Connect GitHub: "+reason {
				t.Fatalf("authored waiting line = %q", e.Narration)
			}
			return
		}
	}
	t.Fatal("no waiting event")
}

// Asking without waiting must not change the probe the phase will ask later.
func TestWithoutWaitDoesNotTouchTheBlueprintsProbe(t *testing.T) {
	h := Hook{ID: "github", Uses: "orun.doctor/check@v1", With: map[string]any{"waitSeconds": 600, "providers": []any{"github"}}}
	once := withoutWait(h)
	if once.With["waitSeconds"] != 0 {
		t.Fatalf("withoutWait left waitSeconds = %v", once.With["waitSeconds"])
	}
	if h.With["waitSeconds"] != 600 {
		t.Fatalf("withoutWait changed the original: waitSeconds = %v", h.With["waitSeconds"])
	}
	plain := Hook{ID: "p", With: map[string]any{"keys": []any{"K"}}}
	if _, added := withoutWait(plain).With["waitSeconds"]; added {
		t.Fatal("a probe with no waitSeconds must not be given one")
	}
}
