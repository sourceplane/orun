package lanes

import (
	"fmt"
	"sort"

	"github.com/sourceplane/orun/internal/model"
)

// autoDrafts lays out the jobs auto mode is left with after annotations
// (specs/orun-lanes §6). fixed is the number of lanes annotations already
// made, which count against maxLanes.
//
//  1. At most maxLanes unplaced jobs are left as job mode lays them out.
//  2. Otherwise jobs are partitioned by (environment, runsOn), and the lane
//     budget is shared in proportion to partition size (at least one each,
//     never more than a partition's jobs).
//  3. Jobs joined by an enforced edge form a cluster that stays in one lane;
//     a cluster bigger than a lane's share is cut by level into consecutive
//     slices.
//  4. Clusters are packed longest-first into the partition's lanes. Every job
//     weighs one: there are no per-job weights.
func autoDrafts(plan *model.Plan, cfg *model.IntentLanes, pending []int, fixed int, g *jobGraph) []*draft {
	maxLanes := cfg.MaxLanes
	if maxLanes <= 0 {
		maxLanes = model.DefaultLaneMaxLanes
	}
	if len(pending) <= maxLanes {
		out := make([]*draft, 0, len(pending))
		for _, i := range pending {
			job := plan.Jobs[i]
			out = append(out, &draft{baseID: job.ID, env: job.Environment, runsOn: job.RunsOn, source: "job", component: job.Component, concurrency: 1, jobs: []int{i}})
		}
		return out
	}

	budget := maxLanes - fixed
	if budget < 1 {
		budget = 1
	}

	// Partitions in order of first appearance.
	type partition struct {
		env, runsOn string
		jobs        []int
		lanes       int
	}
	var parts []*partition
	byKey := map[string]*partition{}
	for _, i := range pending {
		job := plan.Jobs[i]
		key := job.Environment + "\x00" + job.RunsOn
		p, ok := byKey[key]
		if !ok {
			p = &partition{env: job.Environment, runsOn: job.RunsOn}
			byKey[key] = p
			parts = append(parts, p)
		}
		p.jobs = append(p.jobs, i)
	}

	// The budget is shared in proportion to work: job count, or, when the plan
	// carries weights, weight. A partition never gets more lanes than jobs.
	size := func(k int) int { return g.sum(parts[k].jobs) }
	total := 0
	for k := range parts {
		total += size(k)
	}
	allocateLanes(total, budget, len(parts), size, func(k, n int) {
		if n > len(parts[k].jobs) {
			n = len(parts[k].jobs)
		}
		parts[k].lanes = n
	})

	conc := laneConcurrency(cfg)
	var out []*draft
	for _, p := range parts {
		bins := packPartition(g, p.jobs, p.lanes)
		n := len(bins)
		for k, jobs := range bins {
			out = append(out, &draft{
				baseID:      fmt.Sprintf("%s/auto-%d", p.env, k+1),
				env:         p.env,
				runsOn:      p.runsOn,
				source:      "auto",
				concurrency: conc,
				jobs:        jobs,
				autoK:       k + 1,
				autoN:       n,
			})
		}
	}
	return out
}

// allocateLanes shares budget lanes between n partitions in proportion to
// size(k): at least one each, at most size(k), largest remainders first, ties
// to the earlier partition.
func allocateLanes(total, budget, n int, size func(int) int, set func(int, int)) {
	if budget < n {
		budget = n
	}
	alloc := make([]int, n)
	type rem struct {
		k    int
		frac float64
	}
	var rems []rem
	used := 0
	for k := 0; k < n; k++ {
		exact := float64(budget) * float64(size(k)) / float64(total)
		a := int(exact)
		if a < 1 {
			a = 1
		}
		if a > size(k) {
			a = size(k)
		}
		alloc[k] = a
		used += a
		rems = append(rems, rem{k: k, frac: exact - float64(int(exact))})
	}
	sort.SliceStable(rems, func(a, b int) bool { return rems[a].frac > rems[b].frac })
	for used < budget {
		progressed := false
		for _, r := range rems {
			if used >= budget {
				break
			}
			if alloc[r.k] < size(r.k) {
				alloc[r.k]++
				used++
				progressed = true
			}
		}
		if !progressed {
			break
		}
	}
	for k := 0; k < n; k++ {
		set(k, alloc[k])
	}
}

// packPartition clusters a partition's jobs and packs them into at most
// lanes bins, longest-first. Bins come back in a stable order and empty ones
// are dropped.
func packPartition(g *jobGraph, jobs []int, lanes int) [][]int {
	if lanes < 1 {
		lanes = 1
	}
	in := map[int]bool{}
	for _, j := range jobs {
		in[j] = true
	}

	// Union-find over enforced edges inside the partition.
	parent := map[int]int{}
	for _, j := range jobs {
		parent[j] = j
	}
	var find func(int) int
	find = func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	for _, j := range jobs {
		for _, d := range g.deps[j] {
			if in[d] {
				a, b := find(j), find(d)
				if a != b {
					if a < b {
						parent[b] = a
					} else {
						parent[a] = b
					}
				}
			}
		}
	}
	clusters := map[int][]int{}
	var roots []int
	for _, j := range jobs {
		r := find(j)
		if _, ok := clusters[r]; !ok {
			roots = append(roots, r)
		}
		clusters[r] = append(clusters[r], j)
	}

	// A lane's share of the work; a cluster bigger than it is cut by level
	// into consecutive slices within the share. By count, or by weight when
	// the plan carries weights (specs/orun-runner-efficiency §5.4).
	capacity := (g.sum(jobs) + lanes - 1) / lanes
	type item struct {
		jobs     []int
		weight   int
		critical int
		minLevel int
		minJob   int
	}
	var items []item
	for _, r := range roots {
		c := clusters[r]
		sort.Slice(c, func(a, b int) bool {
			if g.level[c[a]] != g.level[c[b]] {
				return g.level[c[a]] < g.level[c[b]]
			}
			return c[a] < c[b]
		})
		for start := 0; start < len(c); {
			end := start
			acc := 0
			for end < len(c) && (end == start || acc+g.weight[c[end]] <= capacity) {
				acc += g.weight[c[end]]
				end++
			}
			slice := append([]int(nil), c[start:end]...)
			minJob := slice[0]
			for _, j := range slice {
				if j < minJob {
					minJob = j
				}
			}
			items = append(items, item{jobs: slice, weight: acc, critical: g.criticalPath(slice), minLevel: g.level[slice[0]], minJob: minJob})
			start = end
		}
	}
	// Longest work first: by the longest chain of work in the slice when
	// weighted (the lane that bounds the wall-clock), else by size; then the
	// slice's weight, its lowest level and its earliest job.
	sort.SliceStable(items, func(a, b int) bool {
		if g.weighted && items[a].critical != items[b].critical {
			return items[a].critical > items[b].critical
		}
		if items[a].weight != items[b].weight {
			return items[a].weight > items[b].weight
		}
		if items[a].minLevel != items[b].minLevel {
			return items[a].minLevel < items[b].minLevel
		}
		return items[a].minJob < items[b].minJob
	})

	bins := make([][]int, lanes)
	load := make([]int, lanes)
	for _, it := range items {
		best := 0
		for b := 1; b < lanes; b++ {
			if load[b] < load[best] {
				best = b
			}
		}
		bins[best] = append(bins[best], it.jobs...)
		load[best] += it.weight
	}
	var out [][]int
	for _, b := range bins {
		if len(b) == 0 {
			continue
		}
		sort.Ints(b)
		out = append(out, b)
	}
	// Stable order: by the lowest level, then the earliest job.
	sort.SliceStable(out, func(a, b int) bool {
		la, lb := minLevelOf(g, out[a]), minLevelOf(g, out[b])
		if la != lb {
			return la < lb
		}
		return out[a][0] < out[b][0]
	})
	return out
}

// sum is the jobs' total weight (their count when the plan has no weights).
func (g *jobGraph) sum(jobs []int) int {
	n := 0
	for _, j := range jobs {
		n += g.weight[j]
	}
	return n
}

// criticalPath is the heaviest chain of enforced dependencies inside jobs:
// the least time the slice can take however wide the lane is.
func (g *jobGraph) criticalPath(jobs []int) int {
	in := make(map[int]bool, len(jobs))
	for _, j := range jobs {
		in[j] = true
	}
	memo := map[int]int{}
	var longest func(j int) int
	longest = func(j int) int {
		if v, ok := memo[j]; ok {
			return v
		}
		best := 0
		for _, d := range g.deps[j] {
			if in[d] {
				if v := longest(d); v > best {
					best = v
				}
			}
		}
		memo[j] = best + g.weight[j]
		return memo[j]
	}
	m := 0
	for _, j := range jobs {
		if v := longest(j); v > m {
			m = v
		}
	}
	return m
}

func minLevelOf(g *jobGraph, jobs []int) int {
	m := g.level[jobs[0]]
	for _, j := range jobs {
		if g.level[j] < m {
			m = g.level[j]
		}
	}
	return m
}

// renumberAuto refreshes the k/n numbering and ids of auto lanes after a
// merge, per partition, keeping their relative order.
func renumberAuto(ds []*draft) {
	type key struct{ env, runsOn string }
	count := map[key]int{}
	for _, d := range ds {
		if d.source == "auto" {
			count[key{d.env, d.runsOn}]++
		}
	}
	seen := map[key]int{}
	sorted := make([]*draft, 0, len(ds))
	for _, d := range ds {
		if d.source == "auto" {
			sorted = append(sorted, d)
		}
	}
	sort.SliceStable(sorted, func(a, b int) bool { return sorted[a].autoK < sorted[b].autoK })
	for _, d := range sorted {
		k := key{d.env, d.runsOn}
		seen[k]++
		d.autoK = seen[k]
		d.autoN = count[k]
		d.baseID = fmt.Sprintf("%s/auto-%d", d.env, d.autoK)
	}
}
