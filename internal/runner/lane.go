package runner

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/executor"
	"github.com/sourceplane/orun/internal/model"
)

// Lane execution (specs/orun-lanes §8): `orun run --lane <id>` runs every job
// of one lane in this process. Upstreams inside the lane are awaited in
// memory; upstreams in other lanes are awaited through the AwaitUpstream hook
// (remote state) without holding a concurrency slot, or must already be
// completed in the run's state (local state).

// laneJobs keeps the jobs of r.LaneJobs, in the order given.
func (r *Runner) laneJobs(jobs []model.PlanJob) []model.PlanJob {
	in := make(map[string]bool, len(r.LaneJobs))
	for _, id := range r.LaneJobs {
		in[id] = true
	}
	out := make([]model.PlanJob, 0, len(r.LaneJobs))
	for _, job := range jobs {
		if in[job.ID] {
			out = append(out, job)
		}
	}
	return out
}

// outsideUpstreams returns, per lane job, the upstreams that are not in the
// lane.
func outsideUpstreams(jobs []model.PlanJob) map[string][]string {
	in := make(map[string]bool, len(jobs))
	for _, job := range jobs {
		in[job.ID] = true
	}
	out := map[string][]string{}
	for _, job := range jobs {
		for _, dep := range job.DependsOn {
			if !in[dep] {
				out[job.ID] = append(out[job.ID], dep)
			}
		}
	}
	return out
}

// checkLocalUpstreams refuses a lane whose outside upstreams are not
// completed in local state, naming the lanes to run first.
func (r *Runner) checkLocalUpstreams(plan *model.Plan, jobs []model.PlanJob, execState *execmodel.ExecState) error {
	laneOf := make(map[string]string, len(plan.Jobs))
	for _, job := range plan.Jobs {
		laneOf[job.ID] = job.Lane
		if laneOf[job.ID] == "" {
			laneOf[job.ID] = job.ID
		}
	}
	var missing []string
	lanes := map[string]bool{}
	for _, deps := range outsideUpstreams(jobs) {
		for _, dep := range deps {
			if js := execState.Jobs[dep]; js != nil && js.Status == "completed" {
				continue
			}
			missing = append(missing, dep)
			lanes[laneOf[dep]] = true
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	names := make([]string, 0, len(lanes))
	for l := range lanes {
		names = append(names, l)
	}
	sort.Strings(names)
	return fmt.Errorf("cannot run lane %s: upstream jobs in other lanes are not completed (%s); run lane %s first with the same --exec-id, or use remote state",
		r.LaneID, strings.Join(dedupe(missing), ", "), strings.Join(names, ", "))
}

func dedupe(s []string) []string {
	out := s[:0]
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}

// runScheduled runs jobs each as soon as its upstreams finish, at most
// concurrency at a time, a free slot going to the ready job with the most
// work behind it in the plan (lane_gate.go). A failed job stops the jobs that
// depend on it and nothing else (specs/orun-lanes §8). Because a job is only
// ever left unrun below a failed upstream, a dependent in another lane sees
// it as blocked and stops waiting (awaitRemoteUpstreams), rather than waiting
// out the upstream timeout for a job nobody will run (orun#737).
//
// It is the one scheduler for a lane (`orun run --lane`, jobs = the lane's)
// and for a whole plan at concurrency > 1 (jobs = every selected job;
// specs/orun-runner-efficiency RE1). The two differ in how an upstream
// outside jobs is treated — a lane awaits it through remote state or has
// already been refused by checkLocalUpstreams; a plan selection (a
// --component or --env filter, or --job) requires it completed in this run's
// state, exactly as the sequential path does — and in failFast: a lane
// passes false, since every rendered plan sets execution.failFast and
// honouring it cancelled every unstarted job of the lane, dependent or not
// (orun#735); a whole plan keeps honouring it, as the sequential path does.
//
// A lane of more than one job also reports each job on its own (§9): a check
// run per job through r.Checks, and a row per job in the step summary.
func (r *Runner) runScheduled(planJobs, jobs []model.PlanJob, execState *execmodel.ExecState, baseExecContext executor.ExecContext, persistState, failFast bool, summary *runSummary, concurrency int) error {
	if r.DryRun || concurrency < 1 {
		concurrency = 1
	}
	laneMode := r.LaneJobs != nil
	outside := outsideUpstreams(jobs)
	multi := len(jobs) > 1
	checks := r.Checks
	if !multi || r.DryRun || !laneMode {
		checks = nil
	}
	// With several jobs in flight, hold each job's step groups until it ends
	// so its output reads as one block in the GitHub log.
	r.ghaHoldSteps = multi && concurrency > 1

	type outcome struct {
		done chan struct{}
		ok   bool
	}
	outcomes := make(map[string]*outcome, len(jobs))
	for _, job := range jobs {
		outcomes[job.ID] = &outcome{done: make(chan struct{})}
	}
	records := make([]*laneRecord, len(jobs))

	gate := newSlotGate(concurrency, len(jobs))
	components := newComponentGate(r.ComponentConcurrency)
	prio := criticalPath(planJobs)
	var mu sync.Mutex
	var firstErr error
	// stopped is a whole-plan run's fail-fast: never true for a lane (see above).
	stopped := func() bool {
		mu.Lock()
		defer mu.Unlock()
		return failFast && firstErr != nil
	}
	fail := func(err error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		mu.Unlock()
	}

	var wg sync.WaitGroup
	for i, job := range jobs {
		wg.Add(1)
		rec := &laneRecord{job: job, status: laneNotRun, waited: outside[job.ID]}
		records[i] = rec
		ticket := gate.ticket(prio[job.ID], i)
		go func(j model.PlanJob, rec *laneRecord, ticket *slotTicket) {
			defer wg.Done()
			defer ticket.settle()
			o := outcomes[j.ID]
			defer func() {
				o.ok = rec.status == laneSucceeded || rec.status == laneResumed
				if checks != nil {
					checks.Finish(j.ID, laneCheckName(j), rec.conclusion(), rec.note)
				}
				close(o.done)
			}()

			// Upstreams in this run.
			for _, dep := range j.DependsOn {
				up, ok := outcomes[dep]
				if !ok {
					continue
				}
				ticket.settle()
				<-up.done
				if !up.ok {
					summary.addWaiting(1)
					rec.status, rec.note = laneBlocked, "upstream "+dep+" did not succeed"
					r.printLaneBlocked(j, dep)
					return
				}
			}

			r.stateMu.Lock()
			jobState := ensureJobState(execState, j)
			completed := jobState.Status == "completed"
			r.stateMu.Unlock()
			if completed {
				rec.status = laneResumed
				summary.addResumed()
				r.printJobResumed(j)
				return
			}

			// Upstreams outside this run's jobs.
			if deps := outside[j.ID]; len(deps) > 0 && !r.DryRun {
				switch {
				case laneMode && r.Hooks != nil && r.Hooks.AwaitUpstream != nil:
					// Another lane's job: waited for without holding a slot.
					ticket.settle()
					if err := r.Hooks.AwaitUpstream(j.ID, deps); err != nil {
						summary.addWaiting(1)
						rec.status, rec.note = laneBlocked, err.Error()
						fail(err)
						fmt.Fprintf(r.Stderr, "  %s\n", err)
						return
					}
				case laneMode, r.JobID != "" && r.SkipLocalDepsForJob:
					// Verified already: by checkLocalUpstreams, or by the remote
					// claim that ordered this --job.
				default:
					// A job the selection left out must have completed in this
					// run's state; otherwise this job waits, as the sequential
					// path reports it, and a --job is refused.
					r.stateMu.Lock()
					unmet := unresolvedDependencies(j, execState)
					r.stateMu.Unlock()
					if len(unmet) > 0 {
						summary.addWaiting(1)
						rec.status, rec.note = laneNotRun, "waiting on "+strings.Join(unmet, ", ")
						if r.JobID != "" {
							fail(fmt.Errorf("cannot run %s: dependencies not completed (%s)", j.ID, strings.Join(unmet, ", ")))
						} else if !r.inGHA() && r.Verbose {
							r.printWaiting(j, unmet, execState)
						}
						return
					}
				}
			}

			if stopped() {
				return
			}
			// --component-concurrency: a job of a component not yet in flight
			// waits here, holding no slot, until a component drains. It settles
			// its ticket only if it has to wait: settling before joining the slot
			// queue would let a lower-priority job that is already queued take
			// the slot this one should get.
			components.enter(j.Component, ticket.settle)
			defer components.leave(j.Component)
			ticket.acquire()
			defer ticket.release()
			if stopped() {
				return
			}

			if r.Hooks != nil && r.Hooks.BeforeJob != nil {
				skip, err := r.Hooks.BeforeJob(j.ID)
				if err != nil {
					rec.status, rec.note = laneFailed, err.Error()
					fail(err)
					return
				}
				if skip {
					r.stateMu.Lock()
					jobState.Status = "completed"
					r.stateMu.Unlock()
					rec.status = laneResumed
					summary.addResumed()
					r.printJobResumed(j)
					return
				}
			}

			if checks != nil {
				checks.Start(j.ID, laneCheckName(j))
			}
			started := time.Now()
			failed := r.executeJob(j, jobState, execState, baseExecContext, persistState, false, summary)
			rec.duration = time.Since(started)
			if failed {
				rec.status = laneFailed
				r.stateMu.Lock()
				rec.note = jobState.LastError
				r.stateMu.Unlock()
				fail(fmt.Errorf("job %s failed", j.ID))
				return
			}
			rec.status = laneSucceeded
		}(job, rec, ticket)
	}
	wg.Wait()
	r.ghaHoldSteps = false

	if laneMode && multi && r.inGHA() && !r.DryRun {
		writeLaneStepSummary(r.LaneID, records)
	}

	snap := summary.snapshot()
	finalStatus := "completed"
	if snap.failed > 0 || firstErr != nil {
		finalStatus = "failed"
	}
	r.printRunSummary(summary, finalStatus)
	return firstErr
}

func (r *Runner) printLaneBlocked(job model.PlanJob, dep string) {
	r.withPrintLock(func() {
		fmt.Fprintf(r.Stderr, "  ⊘ %s not run: upstream %s did not succeed\n", job.ID, dep)
	})
}

// componentGate caps how many distinct components have jobs in flight
// (`orun run --component-concurrency`). It keeps a run's output grouped by
// component on a terminal; it is not a scheduling constraint otherwise, and
// a cap of 0 admits everything.
type componentGate struct {
	mu     sync.Mutex
	cond   *sync.Cond
	cap    int
	active map[string]int
}

func newComponentGate(cap int) *componentGate {
	g := &componentGate{cap: cap, active: map[string]int{}}
	g.cond = sync.NewCond(&g.mu)
	return g
}

// enter blocks until the component is in flight or there is room for one
// more. A job waiting here holds no slot; onWait is called once before the
// first wait (the job has reached its first wait, for the slot gate).
func (g *componentGate) enter(component string, onWait func()) {
	if g.cap <= 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	waited := false
	for g.active[component] == 0 && len(g.active) >= g.cap {
		if !waited && onWait != nil {
			waited = true
			onWait()
		}
		g.cond.Wait()
	}
	g.active[component]++
}

// leave releases the component once its last in-flight job ends.
func (g *componentGate) leave(component string) {
	if g.cap <= 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.active[component]--
	if g.active[component] <= 0 {
		delete(g.active, component)
	}
	g.cond.Broadcast()
}
