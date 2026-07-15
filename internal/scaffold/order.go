package scaffold

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/planner"
)

// orderModules lowers a blueprint's modules into orun's own dependency graph
// and returns the placement order as a sequence of batches (design §6). Edges
// are DECLARED (dependsOn + wiring) — never sniffed from framework files. A
// single component is a one-node graph (a single one-element batch); a repo is
// the batched order the planner's graph implies.
//
// It REUSES internal/planner's JobGraph for the authoritative cycle check
// (DetectCycles). The order itself is computed with a deterministic Kahn pass
// (a stable tiebreak by module name) because planner.TopologicalSort is
// JobInstance-bound and nondeterministic in tie order, and scaffolding requires
// byte-reproducible output (design §9). When a cycle exists, Tarjan SCC
// condensation groups the cluster into one atomic batch, which is only allowed
// if the blueprint declares those feedback edges in cycleBreak.
func orderModules(bp *Blueprint) ([][]string, error) {
	return orderModuleSet(bp.Modules, bp.CycleBreak)
}

// PhasePlan is one ordered phase ready to place: its name, DAG-ordered batches,
// and the hooks to run after it (design §6, phases overlay).
type PhasePlan struct {
	Name    string
	Batches [][]string
	Hooks   []Hook
}

// planPhases lowers a blueprint into ordered phases. With no declared phases it
// returns a single implicit phase carrying every batch (today's behavior). With
// phases it orders each phase's modules by the DAG restricted to that phase
// (cross-phase edges are already satisfied by the barrier law, validated in
// blueprint.validatePhases), preserving the authored phase order.
func planPhases(bp *Blueprint) ([]PhasePlan, error) {
	if len(bp.Phases) == 0 {
		batches, err := orderModules(bp)
		if err != nil {
			return nil, err
		}
		return []PhasePlan{{Name: "", Batches: batches, Hooks: nil}}, nil
	}

	byName := make(map[string]Module, len(bp.Modules))
	for _, m := range bp.Modules {
		byName[m.Name] = m
	}
	plans := make([]PhasePlan, 0, len(bp.Phases))
	for _, ph := range bp.Phases {
		inPhase := make(map[string]struct{}, len(ph.Modules))
		for _, name := range ph.Modules {
			inPhase[name] = struct{}{}
		}
		// Restrict each module's edges to same-phase prerequisites; edges to
		// earlier phases are already placed (barrier law).
		mods := make([]Module, 0, len(ph.Modules))
		for _, name := range ph.Modules {
			m := byName[name]
			m.DependsOn = filterToSet(m.DependsOn, inPhase)
			m.Wiring = filterToSet(m.Wiring, inPhase)
			mods = append(mods, m)
		}
		batches, err := orderModuleSet(mods, bp.CycleBreak)
		if err != nil {
			return nil, fmt.Errorf("phase %q: %w", ph.Name, err)
		}
		plans = append(plans, PhasePlan{Name: ph.Name, Batches: batches, Hooks: ph.Hooks})
	}
	return plans, nil
}

func filterToSet(items []string, set map[string]struct{}) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		if _, ok := set[it]; ok {
			out = append(out, it)
		}
	}
	return out
}

// orderModuleSet computes the deterministic, DAG-ordered batches for a set of
// modules (design §6). It REUSES internal/planner's JobGraph for the
// authoritative cycle check, then produces a byte-reproducible order with a
// stable Kahn pass (planner.TopologicalSort is JobInstance-bound and tie-
// nondeterministic). A cycle is condensed into SCC batches, permitted only when
// every feedback edge is declared in cycleBreak.
func orderModuleSet(modules []Module, cycleBreak []string) ([][]string, error) {
	deps := make(map[string][]string, len(modules))
	names := make([]string, 0, len(modules))
	for _, m := range modules {
		names = append(names, m.Name)
		edges := append(append([]string{}, m.DependsOn...), m.Wiring...)
		sort.Strings(edges)
		deps[m.Name] = edges
	}
	sort.Strings(names)

	// Reuse the planner's DAG for the authoritative cycle check.
	jobs := make(map[string]*model.JobInstance, len(modules))
	for _, name := range names {
		jobs[name] = &model.JobInstance{ID: name, DependsOn: deps[name]}
	}
	graph := planner.NewJobGraph(jobs)

	if err := graph.DetectCycles(); err != nil {
		// A cycle exists: condense into SCC batches and require every
		// feedback edge to be declared in cycleBreak (design §6).
		return orderWithCycles(names, deps, cycleBreakSet(cycleBreak))
	}

	// Acyclic: deterministic Kahn, each module its own batch.
	order, err := deterministicTopo(names, deps)
	if err != nil {
		return nil, err
	}
	batches := make([][]string, len(order))
	for i, n := range order {
		batches[i] = []string{n}
	}
	return batches, nil
}

// deterministicTopo applies Kahn's algorithm (the planner's algorithm) with a
// stable, name-sorted ready queue so the order is byte-reproducible.
func deterministicTopo(names []string, deps map[string][]string) ([]string, error) {
	indeg := make(map[string]int, len(names))
	dependents := make(map[string][]string, len(names))
	for _, n := range names {
		indeg[n] = 0
	}
	for _, n := range names {
		for _, d := range deps[n] {
			indeg[n]++
			dependents[d] = append(dependents[d], n)
		}
	}
	ready := make([]string, 0, len(names))
	for _, n := range names {
		if indeg[n] == 0 {
			ready = append(ready, n)
		}
	}
	sort.Strings(ready)

	order := make([]string, 0, len(names))
	for len(ready) > 0 {
		n := ready[0]
		ready = ready[1:]
		order = append(order, n)
		sort.Strings(dependents[n])
		for _, dep := range dependents[n] {
			indeg[dep]--
			if indeg[dep] == 0 {
				ready = insertSorted(ready, dep)
			}
		}
	}
	if len(order) != len(names) {
		return nil, gateErr("module dependency cycle detected (design §6)")
	}
	return order, nil
}

func insertSorted(s []string, v string) []string {
	i := sort.SearchStrings(s, v)
	s = append(s, "")
	copy(s[i+1:], s[i:])
	s[i] = v
	return s
}

// orderWithCycles condenses the module graph into SCCs (Tarjan), orders the
// condensation topologically, and emits each SCC as one batch. A multi-node SCC
// is a declared binding cluster: it is only permitted if every internal
// (feedback) edge is declared in cycleBreak; otherwise the cycle is an error.
func orderWithCycles(names []string, deps map[string][]string, allowed map[string]bool) ([][]string, error) {
	sccs := tarjanSCC(names, deps)

	// Validate every multi-node SCC is declared.
	for _, comp := range sccs {
		if len(comp) < 2 {
			continue
		}
		member := make(map[string]bool, len(comp))
		for _, m := range comp {
			member[m] = true
		}
		for _, from := range comp {
			for _, to := range deps[from] {
				if member[to] && !allowed[edgeKey(from, to)] && !allowed[edgeKey(to, from)] {
					return nil, gateErr("undeclared dependency cycle among [%s]: edge %s->%s must be listed in cycleBreak (design §6)",
						strings.Join(sortedCopy(comp), ", "), from, to)
				}
			}
		}
	}

	// Order the condensation: build super-node edges, deterministic topo.
	comp := make(map[string]int, len(names))
	for i, c := range sccs {
		for _, n := range c {
			comp[n] = i
		}
	}
	superNames := make([]string, len(sccs))
	superDeps := make(map[string][]string, len(sccs))
	for i := range sccs {
		superNames[i] = fmt.Sprintf("scc-%d", i)
	}
	seen := make(map[string]bool)
	for _, n := range names {
		for _, d := range deps[n] {
			if comp[n] != comp[d] {
				key := fmt.Sprintf("%d->%d", comp[n], comp[d])
				if !seen[key] {
					seen[key] = true
					// n depends on d ⇒ super(n) depends on super(d).
					superDeps[superNames[comp[n]]] = append(superDeps[superNames[comp[n]]], superNames[comp[d]])
				}
			}
		}
	}
	sortedSuper, err := deterministicTopo(superNames, superDeps)
	if err != nil {
		return nil, err
	}
	batches := make([][]string, 0, len(sortedSuper))
	for _, s := range sortedSuper {
		var idx int
		fmt.Sscanf(s, "scc-%d", &idx)
		batches = append(batches, sortedCopy(sccs[idx]))
	}
	return batches, nil
}

// tarjanSCC returns the strongly-connected components of the module graph, in
// reverse-topological order of the condensation (dependencies first once
// reversed by the caller's edge direction). Deterministic via sorted inputs.
func tarjanSCC(names []string, deps map[string][]string) [][]string {
	index := 0
	idx := make(map[string]int)
	low := make(map[string]int)
	onStack := make(map[string]bool)
	var stack []string
	var out [][]string

	var strongconnect func(v string)
	strongconnect = func(v string) {
		idx[v] = index
		low[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true
		succ := append([]string{}, deps[v]...)
		sort.Strings(succ)
		for _, w := range succ {
			if _, ok := idx[w]; !ok {
				strongconnect(w)
				if low[w] < low[v] {
					low[v] = low[w]
				}
			} else if onStack[w] {
				if idx[w] < low[v] {
					low[v] = idx[w]
				}
			}
		}
		if low[v] == idx[v] {
			var comp []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				comp = append(comp, w)
				if w == v {
					break
				}
			}
			out = append(out, comp)
		}
	}

	for _, n := range names {
		if _, ok := idx[n]; !ok {
			strongconnect(n)
		}
	}
	return out
}

func cycleBreakSet(entries []string) map[string]bool {
	set := make(map[string]bool, len(entries))
	for _, e := range entries {
		parts := strings.SplitN(e, "->", 2)
		if len(parts) == 2 {
			set[edgeKey(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))] = true
		}
	}
	return set
}

func edgeKey(from, to string) string { return from + "->" + to }

func sortedCopy(s []string) []string {
	out := append([]string{}, s...)
	sort.Strings(out)
	return out
}
