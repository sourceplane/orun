package lanes

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/model"
)

// jobGraph is the plan's enforced dependency graph by job index, and each
// job's level: the length of the longest enforced chain that ends at it
// (§7.1). Advisory edges are not in DependsOn, so they are not here.
type jobGraph struct {
	index map[string]int
	deps  [][]int
	level []int
	// weight is each job's packing weight: its estimatedSeconds, or, when
	// some jobs carry one and this one does not, the median of those that
	// do. weighted is false when no job carries one: every job then weighs
	// one and auto mode packs by count, exactly as before weights existed.
	weight   []int
	weighted bool
}

func newJobGraph(jobs []model.PlanJob) *jobGraph {
	g := &jobGraph{index: make(map[string]int, len(jobs)), deps: make([][]int, len(jobs)), level: make([]int, len(jobs)), weight: make([]int, len(jobs))}
	for i, job := range jobs {
		g.index[job.ID] = i
	}
	var declared []int
	for _, job := range jobs {
		if job.EstimatedSeconds > 0 {
			declared = append(declared, job.EstimatedSeconds)
		}
	}
	def := 1
	if len(declared) > 0 {
		g.weighted = true
		sort.Ints(declared)
		def = declared[len(declared)/2]
	}
	for i, job := range jobs {
		g.weight[i] = def
		if job.EstimatedSeconds > 0 {
			g.weight[i] = job.EstimatedSeconds
		}
	}
	for i, job := range jobs {
		for _, dep := range job.DependsOn {
			if d, ok := g.index[dep]; ok {
				g.deps[i] = append(g.deps[i], d)
			}
		}
		sort.Ints(g.deps[i])
	}
	done := make([]bool, len(jobs))
	var visit func(i int) int
	visit = func(i int) int {
		if done[i] {
			return g.level[i]
		}
		done[i] = true // the job graph is acyclic (the planner refuses cycles)
		l := 0
		for _, d := range g.deps[i] {
			if v := visit(d) + 1; v > l {
				l = v
			}
		}
		g.level[i] = l
		return l
	}
	for i := range jobs {
		visit(i)
	}
	return g
}

// laneEdges contracts the job graph by lane: an edge a→b means a job in lane
// b waits on a job in lane a. witness keeps one job edge per lane edge.
func laneEdges(g *jobGraph, ds []*draft) (adj [][]int, witness map[[2]int][2]int) {
	laneOf := make([]int, len(g.level))
	for li, d := range ds {
		for _, j := range d.jobs {
			laneOf[j] = li
		}
	}
	adj = make([][]int, len(ds))
	witness = map[[2]int][2]int{}
	for j := range g.deps {
		for _, dep := range g.deps[j] {
			a, b := laneOf[dep], laneOf[j]
			if a == b {
				continue
			}
			key := [2]int{a, b}
			if _, ok := witness[key]; !ok {
				witness[key] = [2]int{dep, j}
				adj[a] = append(adj[a], b)
			}
		}
	}
	for i := range adj {
		sort.Ints(adj[i])
	}
	return adj, witness
}

// sccs returns the strongly connected components with more than one lane,
// each sorted, in a deterministic order (Tarjan).
func sccs(adj [][]int) [][]int {
	n := len(adj)
	index := make([]int, n)
	low := make([]int, n)
	onStack := make([]bool, n)
	for i := range index {
		index[i] = -1
	}
	var stack []int
	var out [][]int
	next := 0
	var strong func(v int)
	strong = func(v int) {
		index[v], low[v] = next, next
		next++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range adj[v] {
			if index[w] < 0 {
				strong(w)
				if low[w] < low[v] {
					low[v] = low[w]
				}
			} else if onStack[w] && index[w] < low[v] {
				low[v] = index[w]
			}
		}
		if low[v] == index[v] {
			var comp []int
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				comp = append(comp, w)
				if w == v {
					break
				}
			}
			if len(comp) > 1 {
				sort.Ints(comp)
				out = append(out, comp)
			}
		}
	}
	for v := 0; v < n; v++ {
		if index[v] < 0 {
			strong(v)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a][0] < out[b][0] })
	return out
}

func acyclic(g *jobGraph, ds []*draft) bool {
	adj, _ := laneEdges(g, ds)
	return len(sccs(adj)) == 0
}

// describeCycle names one cycle through an SCC, lane ids joined by arrows and
// the job edge that forces each hop.
func describeCycle(plan *model.Plan, g *jobGraph, ds []*draft, comp []int) string {
	adj, witness := laneEdges(g, ds)
	in := map[int]bool{}
	for _, v := range comp {
		in[v] = true
	}
	start := comp[0]
	path := []int{start}
	seen := map[int]int{start: 0}
	cur := start
	for {
		nxt := -1
		for _, w := range adj[cur] {
			if in[w] {
				nxt = w
				break
			}
		}
		if nxt < 0 {
			break
		}
		if at, ok := seen[nxt]; ok {
			path = append(path[at:], nxt)
			break
		}
		seen[nxt] = len(path)
		path = append(path, nxt)
		cur = nxt
	}
	var lanesPart, jobsPart []string
	for i, v := range path {
		lanesPart = append(lanesPart, ds[v].laneID())
		if i+1 < len(path) {
			w := witness[[2]int{v, path[i+1]}]
			jobsPart = append(jobsPart, plan.Jobs[w[0]].ID+" → "+plan.Jobs[w[1]].ID)
		}
	}
	return strings.Join(lanesPart, " → ") + " via " + strings.Join(jobsPart, ", ")
}

// repairCycles makes the lane graph acyclic (§7): auto lanes in one partition
// merge; any other lane in a cycle is split by level, cutting as little as it
// can. With onCycle: reject or Strict, a cycle that needs a split is an error.
func repairCycles(plan *model.Plan, g *jobGraph, dsp *[]*draft, cfg *model.IntentLanes, strict bool) ([]string, error) {
	var repairs []string
	reject := strict || cfg.OnCycle == model.LaneOnCycleReject
	for round := 0; round < len(plan.Jobs)+1; round++ {
		ds := *dsp
		adj, _ := laneEdges(g, ds)
		comps := sccs(adj)
		if len(comps) == 0 {
			return repairs, nil
		}
		comp := comps[0]

		// All-auto cycle within one partition: auto's own doing, merge it.
		if merged, ok := mergeAuto(ds, comp); ok {
			*dsp = merged
			continue
		}
		if reject {
			return nil, fmt.Errorf("lanes: %s (execution.lanes.onCycle is reject)", describeCycle(plan, g, ds, comp))
		}
		desc := describeCycle(plan, g, ds, comp)
		next, note := splitByLevel(g, ds, comp)
		if note == "" {
			// Nothing left to split: cannot happen for an acyclic job graph
			// (§7.1), but never loop forever on a broken invariant.
			return nil, fmt.Errorf("lanes: cannot break %s", desc)
		}
		*dsp = next
		repairs = append(repairs, fmt.Sprintf("%s — %s", desc, note))
	}
	return nil, fmt.Errorf("lanes: cycle repair did not converge")
}

// mergeAuto folds an SCC made only of auto lanes of one partition into one.
func mergeAuto(ds []*draft, comp []int) ([]*draft, bool) {
	first := ds[comp[0]]
	for _, v := range comp {
		d := ds[v]
		if d.source != "auto" || d.env != first.env || d.runsOn != first.runsOn {
			return nil, false
		}
	}
	keep := comp[0]
	drop := map[int]bool{}
	for _, v := range comp[1:] {
		ds[keep].jobs = append(ds[keep].jobs, ds[v].jobs...)
		drop[v] = true
	}
	sort.Ints(ds[keep].jobs)
	out := make([]*draft, 0, len(ds)-len(drop))
	for i, d := range ds {
		if !drop[i] {
			out = append(out, d)
		}
	}
	renumberAuto(out)
	return out, true
}

// splitByLevel splits every multi-level lane of the SCC into one slice per
// level, then joins neighbouring slices back wherever the graph stays acyclic.
func splitByLevel(g *jobGraph, ds []*draft, comp []int) ([]*draft, string) {
	inComp := map[int]bool{}
	for _, v := range comp {
		inComp[v] = true
	}
	var out []*draft
	type split struct {
		base   string
		slices []*draft
	}
	var splits []split
	for i, d := range ds {
		if !inComp[i] {
			out = append(out, d)
			continue
		}
		byLevel := map[int][]int{}
		var levels []int
		for _, j := range d.jobs {
			l := g.level[j]
			if _, ok := byLevel[l]; !ok {
				levels = append(levels, l)
			}
			byLevel[l] = append(byLevel[l], j)
		}
		if len(levels) < 2 {
			out = append(out, d)
			continue
		}
		sort.Ints(levels)
		s := split{base: d.laneID()}
		for _, l := range levels {
			c := *d
			c.jobs = append([]int(nil), byLevel[l]...)
			c.splitFrom = d.laneID()
			s.slices = append(s.slices, &c)
			out = append(out, &c)
		}
		splits = append(splits, s)
	}
	if len(splits) == 0 {
		return ds, ""
	}

	// Join neighbouring slices back while the lane graph stays acyclic.
	var notes []string
	for _, s := range splits {
		kept := []*draft{s.slices[0]}
		for _, nxt := range s.slices[1:] {
			last := kept[len(kept)-1]
			trial := make([]*draft, 0, len(out))
			joined := *last
			joined.jobs = append(append([]int(nil), last.jobs...), nxt.jobs...)
			for _, d := range out {
				switch d {
				case last:
					trial = append(trial, &joined)
				case nxt:
				default:
					trial = append(trial, d)
				}
			}
			if acyclic(g, trial) {
				out = trial
				kept[len(kept)-1] = &joined
			} else {
				kept = append(kept, nxt)
			}
		}
		if len(kept) == 1 {
			kept[0].splitFrom = ""
			kept[0].part = 0
			continue
		}
		var parts []string
		for k, d := range kept {
			d.part = k + 1
			parts = append(parts, d.laneID())
		}
		notes = append(notes, fmt.Sprintf("split %s into %s", s.base, strings.Join(parts, ", ")))
	}
	if len(notes) == 0 {
		return out, "joined back without a cut"
	}
	return out, strings.Join(notes, "; ")
}
