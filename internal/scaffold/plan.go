package scaffold

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
)

// The shared plan (orun-bootstrap-engine BE-O2).
//
// Run and Derive must compute the SAME bytes, or "is this phase done?" is
// answered by a different renderer than the one that placed it and the answer
// means nothing. So both go through buildPlan: resolve sources, order phases,
// render every module. Run then writes; Derive then compares.

type runPlan struct {
	bp       *Blueprint
	values   Values
	phases   []PhasePlan
	sources  map[string]ResolvedSource
	placed   map[string]PlacedFile
	byPhase  map[string]map[string]PlacedFile
	consumed []ConsumedDep
	order    [][]string
	cleanup  func()
}

// buildPlan parses, collects inputs and renders. Callers that already have a
// parsed blueprint and values use buildPlanWith.
func buildPlan(ctx context.Context, opts Options) (*runPlan, error) {
	if opts.Store == nil {
		return nil, fmt.Errorf("scaffold: object store is required")
	}
	bp, err := ParseBlueprint(opts.Blueprint)
	if err != nil {
		return nil, notFoundErr("%v", err)
	}
	values, err := CollectInputs(bp.Inputs, opts.Inputs)
	if err != nil {
		return nil, err
	}
	plan, err := buildPlanWith(ctx, opts, bp, values)
	if err != nil {
		return nil, err
	}
	if plan.cleanup != nil {
		defer plan.cleanup()
	}
	return plan, nil
}

// buildPlanWith renders every module of a parsed blueprint, recording which
// phase each file came from.
func buildPlanWith(ctx context.Context, opts Options, bp *Blueprint, values Values) (*runPlan, error) {
	workDir := opts.WorkDir
	cleanup := func() {}
	if workDir == "" {
		dir, err := os.MkdirTemp("", "orun-scaffold-")
		if err != nil {
			return nil, err
		}
		workDir = dir
		cleanup = func() { _ = os.RemoveAll(dir) }
	}

	sources, err := resolveSources(ctx, opts.Store, bp.Sources, bp.Ignore, opts.SourceBaseDir, workDir)
	if err != nil {
		cleanup()
		return nil, err
	}
	phases, err := planPhases(bp)
	if err != nil {
		cleanup()
		return nil, err
	}
	modulesByName := make(map[string]Module, len(bp.Modules))
	for _, m := range bp.Modules {
		modulesByName[m.Name] = m
	}

	plan := &runPlan{
		bp: bp, values: values, phases: phases, sources: sources,
		placed:  map[string]PlacedFile{},
		byPhase: map[string]map[string]PlacedFile{},
		cleanup: cleanup,
	}

	// Place modules phase by phase, batch by batch, in dependency order. Detect
	// cross-module target collisions (S-10) — a silent last-writer is a failure.
	for _, phase := range phases {
		plan.byPhase[phase.Name] = map[string]PlacedFile{}
		for _, batch := range phase.Batches {
			plan.order = append(plan.order, batch)
			for _, name := range batch {
				m := modulesByName[name]
				var tree FileTree
				if m.Source != "" {
					rs, ok := sources[m.Source]
					if !ok {
						cleanup()
						return nil, notFoundErr("module %q references unresolved source %q", m.Name, m.Source)
					}
					tree = rs.Tree
				}
				out, err := placeModule(m, tree, values)
				if err != nil {
					cleanup()
					return nil, err
				}
				if out.consumed != nil {
					dep := *out.consumed
					if rs, ok := sources[dep.Source]; ok {
						dep.Digest = string(rs.Digest)
					}
					plan.consumed = append(plan.consumed, dep)
				}
				for _, f := range out.files {
					if prev, dup := plan.placed[f.Path]; dup {
						cleanup()
						return nil, gateErr("target collision at %q: modules %q and %q both write it (design §5/§9)", f.Path, prev.Module, f.Module)
					}
					plan.placed[f.Path] = f
					plan.byPhase[phase.Name][f.Path] = f
				}
			}
		}
	}
	return plan, nil
}

// declOf returns the blueprint's declaration for a planned phase.
func (p *runPlan) declOf(name string) *Phase {
	for i := range p.bp.Phases {
		if p.bp.Phases[i].Name == name {
			return &p.bp.Phases[i]
		}
	}
	return nil
}

// skipped reports whether a phase's `when` condition excludes it. A condition
// that does not evaluate is an ERROR, never a silent skip: "this phase did
// nothing and nobody knows why" is the failure the parse-time compile check
// exists to prevent, and the same reasoning holds at run time.
func (p *runPlan) skipped(name string, inputs map[string]any) (bool, error) {
	decl := p.declOf(name)
	if decl == nil || decl.When == "" {
		return false, nil
	}
	ok, err := evalCondition(decl.When, inputs)
	if err != nil {
		return false, gateErr("phase %q: %v", name, err)
	}
	return !ok, nil
}

// phaseNames lists the plan's phases in order.
func (p *runPlan) phaseNames() []string {
	out := make([]string, 0, len(p.phases))
	for _, ph := range p.phases {
		out = append(out, ph.Name)
	}
	return out
}

// selectPhases narrows the plan per Only/Until/Resume.
//
// The bool reports whether a selection was ASKED FOR, which is not the same as
// whether it selected anything: `--resume` on a finished product selects zero
// phases, and that is the correct answer. Conflating "no selection" with "an
// empty selection" made a completed product re-place its whole tree.
func (p *runPlan) selectPhases(opts Options) ([]PhasePlan, bool, error) {
	set := 0
	for _, on := range []bool{opts.Only != "", opts.Until != "", opts.Resume} {
		if on {
			set++
		}
	}
	// A redo names phases a resume places again. It is a refinement of
	// resume, not a fourth selector: on its own there is nothing to refine.
	redo := map[string]bool{}
	for _, name := range opts.Redo {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !opts.Resume {
			return nil, false, gateErr("scaffold: --redo names a phase for --resume to place again; use it with --resume")
		}
		if !containsPhaseName(p.phases, name) {
			return nil, false, notFoundErr("scaffold: no phase named %q to redo (this blueprint declares: %s)", name, strings.Join(p.phaseNames(), ", "))
		}
		redo[name] = true
	}
	if set == 0 {
		return nil, false, nil
	}
	if set > 1 {
		return nil, false, gateErr("scaffold: --phase, --until and --resume select phases three different ways; use one")
	}

	switch {
	case opts.Only != "":
		for _, ph := range p.phases {
			if ph.Name == opts.Only {
				return []PhasePlan{ph}, true, nil
			}
		}
		return nil, false, notFoundErr("scaffold: no phase named %q (this blueprint declares: %s)", opts.Only, strings.Join(p.phaseNames(), ", "))

	case opts.Until != "":
		out := make([]PhasePlan, 0, len(p.phases))
		for _, ph := range p.phases {
			out = append(out, ph)
			if ph.Name == opts.Until {
				return out, true, nil
			}
		}
		return nil, false, notFoundErr("scaffold: no phase named %q (this blueprint declares: %s)", opts.Until, strings.Join(p.phaseNames(), ", "))

	default: // Resume
		out := make([]PhasePlan, 0, len(p.phases))
		var drifted []string
		for _, ph := range p.phases {
			// A phase the condition excludes is not "not yet done" — it is not
			// wanted. Resuming into it would place a tree the operator asked
			// not to have.
			skip, err := p.skipped(ph.Name, p.values.Fields)
			if err != nil {
				return nil, false, err
			}
			if skip {
				continue
			}
			// A REDO IS PLACED WHATEVER THE TREE SAYS. The tree answers "are
			// the files there", and after a phase landed and then failed to
			// converge they are — merged, even. The operator retrying that
			// build knows something the tree cannot: the phase is not done.
			// Its files re-place as a no-op and its hooks run again, which is
			// what "do the phase again" means for a phase whose work is a
			// landing and a deployment.
			if redo[ph.Name] {
				out = append(out, ph)
				continue
			}
			st := derivePhaseOf(opts.OutDir, ph.Name, p.byPhase[ph.Name], p.declOf(ph.Name))
			// DRIFT IS SKIPPED, and this reverses an earlier decision here.
			//
			// It used to read "re-placing restores the phase to what the
			// blueprint says, which is what a resume is for" — which assumes
			// the product's files are meant to equal the blueprint's
			// rendering. A baseline that BRANDS what it places breaks that
			// assumption on purpose: cirrus's `01-scaffold` rewrites the
			// baseline's identity out of every file it just wrote, so from
			// then on every placed phase is permanently drifted, and drift is
			// the product's normal steady state rather than an anomaly.
			//
			// Under the old rule `--resume` on such a product re-placed those
			// phases and reverted the branding — observed: a product's
			// package.json `name` went from "acme-cloud" back to "cirrus".
			// "Reported by --status so nobody is surprised" does not save it:
			// a resume is what an automated bootstrap runs, not a human who
			// has just read a status table.
			//
			// So drift is treated as PLACED. Every file the phase writes is on
			// disk, which is the question resume asks; whether their contents
			// still match the blueprint is the product's business, and
			// `status.go` already says drift is "reported and never silently
			// resolved". Re-placing a phase whose blueprint genuinely moved is
			// still available, as the deliberate act it should be: `--phase`.
			//
			// PhasePartial is NOT skipped — some files are missing, so the
			// placement really is unfinished and re-running completes it.
			//
			// PhaseUnknown is not skipped either, and that is the safe way
			// round: a hook-only phase's work lives where the tree cannot see
			// it, and its hooks are idempotent by construction (find-or-create,
			// additive apply), so re-running one that was already done costs a
			// few API calls. Skipping one that was NOT done leaves a bootstrap
			// silently incomplete.
			if st.State == PhaseDone || st.State == PhaseDrifted {
				if st.State == PhaseDrifted {
					drifted = append(drifted, ph.Name)
				}
				continue
			}
			out = append(out, ph)
		}
		// Skipping is not silent. A resume that passes over a phase says so and
		// says why, on the run that does it, because the alternative is an
		// operator wondering why their branded product was left alone.
		if len(drifted) > 0 {
			fmt.Fprintf(os.Stderr, "resume: leaving %s as placed — every file is present but differs from the blueprint (branded, or edited). Re-place one deliberately with --phase <name>.\n",
				strings.Join(drifted, ", "))
		}
		return out, true, nil
	}
}

// filesFor is the union of the selected phases' files.
func (p *runPlan) filesFor(phases []PhasePlan) map[string]PlacedFile {
	out := make(map[string]PlacedFile)
	for _, ph := range phases {
		for path, f := range p.byPhase[ph.Name] {
			out[path] = f
		}
	}
	return out
}

// mergeModuleRecords folds a previous lock's module records into this run's, so
// a partial run does not erase the record of the phases it did not touch.
//
// Without this, `--phase 05-edge` would write a provenance.lock naming five
// files and nothing else — and the product would have lost the record of
// everything phases 01 to 04 placed.
func mergeModuleRecords(prev, current []ProvModule) []ProvModule {
	byName := make(map[string]ProvModule, len(prev)+len(current))
	var order []string
	for _, m := range prev {
		if _, seen := byName[m.Name]; !seen {
			order = append(order, m.Name)
		}
		byName[m.Name] = m
	}
	for _, m := range current {
		if _, seen := byName[m.Name]; !seen {
			order = append(order, m.Name)
		}
		byName[m.Name] = m // this run's record wins for a module it placed
	}
	sort.Strings(order)
	out := make([]ProvModule, 0, len(order))
	for _, name := range order {
		out = append(out, byName[name])
	}
	return out
}

func containsPhaseName(phases []PhasePlan, name string) bool {
	for _, ph := range phases {
		if ph.Name == name {
			return true
		}
	}
	return false
}
