package runner

import (
	"sync"

	"github.com/sourceplane/orun/internal/model"
)

// A lane runs more jobs than it has slots (auto mode packs a whole
// environment's fleet into one lane), so which ready job gets the next slot
// decides how long the lane's critical path waits. A plain semaphore leaves
// that to the goroutine scheduler: on cirrus-baseline's edge phase the API
// edge, the only job the console waits on, drew the last slot of its lane
// behind ten workers nothing waited on, and the console went live four
// minutes late.
//
// slotGate hands each free slot to the waiting job with the longest chain of
// work behind it in the whole plan (criticalPath), the plan order breaking
// ties. It hands out nothing until every job of the lane has reached its
// first wait — for a slot, for an upstream, or the end — so the jobs that are
// ready at the start compete by priority rather than by who reached the
// gate first.
type slotGate struct {
	mu        sync.Mutex
	free      int
	unsettled int
	waiting   []*slotTicket
}

// slotTicket is one job's place at the gate.
type slotTicket struct {
	gate    *slotGate
	prio    int
	order   int
	settled bool
	granted chan struct{}
}

func newSlotGate(slots, jobs int) *slotGate {
	return &slotGate{free: slots, unsettled: jobs}
}

func (g *slotGate) ticket(prio, order int) *slotTicket {
	return &slotTicket{gate: g, prio: prio, order: order, granted: make(chan struct{})}
}

// settle records that the job reached its first wait. Idempotent.
func (t *slotTicket) settle() {
	g := t.gate
	g.mu.Lock()
	defer g.mu.Unlock()
	t.settleLocked()
	g.dispatchLocked()
}

func (t *slotTicket) settleLocked() {
	if !t.settled {
		t.settled = true
		t.gate.unsettled--
	}
}

// acquire blocks until the gate hands this job a slot.
func (t *slotTicket) acquire() {
	g := t.gate
	g.mu.Lock()
	t.settleLocked()
	g.waiting = append(g.waiting, t)
	g.dispatchLocked()
	g.mu.Unlock()
	<-t.granted
}

// release returns a slot taken by acquire.
func (t *slotTicket) release() {
	g := t.gate
	g.mu.Lock()
	defer g.mu.Unlock()
	g.free++
	g.dispatchLocked()
}

func (g *slotGate) dispatchLocked() {
	if g.unsettled > 0 {
		return
	}
	for g.free > 0 && len(g.waiting) > 0 {
		best := 0
		for i, w := range g.waiting[1:] {
			b := g.waiting[best]
			if w.prio > b.prio || (w.prio == b.prio && w.order < b.order) {
				best = i + 1
			}
		}
		w := g.waiting[best]
		g.waiting = append(g.waiting[:best], g.waiting[best+1:]...)
		g.free--
		close(w.granted)
	}
}

// criticalPath returns, per job of the plan, the number of jobs on the
// longest chain that starts at it and follows dependents: 1 for a job
// nothing waits on. Dependents in other lanes count — the console that waits
// on the API edge runs in a lane of its own.
func criticalPath(jobs []model.PlanJob) map[string]int {
	dependents := make(map[string][]string, len(jobs))
	for _, job := range jobs {
		for _, dep := range job.DependsOn {
			dependents[dep] = append(dependents[dep], job.ID)
		}
	}
	memo := make(map[string]int, len(jobs))
	visiting := map[string]bool{}
	var walk func(id string) int
	walk = func(id string) int {
		if n, ok := memo[id]; ok {
			return n
		}
		if visiting[id] { // a cycle is the planner's to reject; don't loop on one
			return 1
		}
		visiting[id] = true
		longest := 0
		for _, d := range dependents[id] {
			if n := walk(d); n > longest {
				longest = n
			}
		}
		delete(visiting, id)
		memo[id] = longest + 1
		return longest + 1
	}
	for _, job := range jobs {
		walk(job.ID)
	}
	return memo
}
