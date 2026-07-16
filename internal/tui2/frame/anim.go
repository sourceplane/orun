package frame

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// TickInterval is the one animation cadence in the cockpit. Everything that
// moves — spinner, live dots, progress — advances on this beat or not at all.
const TickInterval = 100 * time.Millisecond

// TickMsg is the scheduler's beat. Exactly one tick chain exists at a time,
// and none exists while no animator is registered: an idle cockpit receives
// no messages and renders no frames (design §13.3).
type TickMsg struct{ At time.Time }

// Scheduler owns the single animation ticker. Anything that needs motion
// registers an animator id while its work is live and removes it when done;
// the ticker arms while the set is non-empty and stops — not pauses, stops —
// when it empties.
//
// Motion means work: the scheduler is deliberately the only source of
// time-based re-render in the program.
type Scheduler struct {
	animators map[string]struct{}
	armed     bool
	interval  time.Duration
}

// NewScheduler returns a scheduler beating at TickInterval.
func NewScheduler() *Scheduler {
	return &Scheduler{animators: make(map[string]struct{}), interval: TickInterval}
}

// Add registers an animator. The caller must follow with Arm() (or return
// its command) so the chain starts if this was the first animator.
func (s *Scheduler) Add(id string) { s.animators[id] = struct{}{} }

// Remove deregisters an animator. When the last one leaves, the in-flight
// tick (if any) arrives, finds the set empty, and the chain ends.
func (s *Scheduler) Remove(id string) { delete(s.animators, id) }

// Active reports whether any animator is registered.
func (s *Scheduler) Active() bool { return len(s.animators) > 0 }

// Armed reports whether a tick is in flight — the test hook behind the
// "idle cockpit holds zero tickers" invariant.
func (s *Scheduler) Armed() bool { return s.armed }

// Arm starts the tick chain if animators exist and no tick is in flight.
// It returns nil otherwise, so callers can pass its result straight through
// as their command.
func (s *Scheduler) Arm() tea.Cmd {
	if !s.Active() || s.armed {
		return nil
	}
	s.armed = true
	return tea.Tick(s.interval, func(t time.Time) tea.Msg { return TickMsg{At: t} })
}

// OnTick folds a received tick: the in-flight marker clears, and the chain
// re-arms only while animators remain.
func (s *Scheduler) OnTick() tea.Cmd {
	s.armed = false
	return s.Arm()
}
