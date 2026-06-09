package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/ui"
)

// computePlanSelection records which environments and components ended up in
// the compiled plan, whether the selection was an explicit --all-envs, and
// whether any narrowing was applied. The result is deterministic (sorted) and
// is stamped into plan.Metadata.Selection — see
// specs/orun-env-scoping/data-model.md §2.
func computePlanSelection(instances map[string][]*model.ComponentInstance, scoped, allEnvs bool) *model.PlanSelection {
	envSet := make(map[string]struct{}, len(instances))
	compSet := make(map[string]struct{})
	for env, insts := range instances {
		envSet[env] = struct{}{}
		for _, inst := range insts {
			if inst != nil {
				compSet[inst.ComponentName] = struct{}{}
			}
		}
	}
	mode := "full"
	if scoped {
		mode = "scoped"
	}
	return &model.PlanSelection{
		Envs:       selectionSortedKeys(envSet),
		Components: selectionSortedKeys(compSet),
		Mode:       mode,
		AllEnvs:    allEnvs,
	}
}

// computePrunedEdges records dependency edges dropped because an endpoint is
// not in the expanded plan (env-scoping ES2). It mirrors the silent drops the
// planner and promotion resolver already perform on a scoped plan, so the CLI
// can surface them (warn + metadata.selection.prunedEdges). Deterministic:
// deduplicated and sorted by (kind, from, to). See
// specs/orun-env-scoping/data-model.md §3.
func computePrunedEdges(instances map[string][]*model.ComponentInstance, environments map[string]model.Environment) []model.PrunedEdge {
	const sep = "\x00"
	envSet := make(map[string]struct{}, len(instances))
	present := make(map[string]struct{}) // "component\x00env"
	for env, insts := range instances {
		envSet[env] = struct{}{}
		for _, inst := range insts {
			if inst != nil {
				present[inst.ComponentName+sep+env] = struct{}{}
			}
		}
	}

	seen := make(map[string]struct{})
	var pruned []model.PrunedEdge
	add := func(e model.PrunedEdge) {
		k := e.Kind + sep + e.From + sep + e.To + sep + e.Reason
		if _, ok := seen[k]; ok {
			return
		}
		seen[k] = struct{}{}
		pruned = append(pruned, e)
	}

	// Component edges: a component dependsOn whose target (component, env) is
	// absent from the plan and whose include policy is not "always" (the
	// planner silently order-drops these; "always" is a hard error instead).
	for env, insts := range instances {
		for _, inst := range insts {
			if inst == nil {
				continue
			}
			for _, dep := range inst.DependsOn {
				if dep.Include == model.IncludeAlways {
					continue
				}
				targetEnv := dep.Environment
				if targetEnv == "" {
					targetEnv = env
				}
				if _, ok := present[dep.ComponentName+sep+targetEnv]; ok {
					continue
				}
				reason := "component-not-selected"
				if _, ok := envSet[targetEnv]; !ok {
					reason = "env-not-selected"
				}
				add(model.PrunedEdge{Kind: "component", From: inst.ComponentName, To: dep.ComponentName, Reason: reason})
			}
		}
	}

	// Promotion edges: a same-plan promotion dependency whose prerequisite
	// environment is not in this plan (promotion.go prunes it; we record it).
	for envName := range envSet {
		env, ok := environments[envName]
		if !ok {
			continue
		}
		for _, dep := range env.Promotion.DependsOn {
			if dep.Satisfy != "same-plan" {
				continue
			}
			if _, active := envSet[dep.Environment]; active {
				continue
			}
			add(model.PrunedEdge{Kind: "promotion", From: envName, To: dep.Environment, Reason: "env-not-selected"})
		}
	}

	sort.Slice(pruned, func(i, j int) bool {
		if pruned[i].Kind != pruned[j].Kind {
			return pruned[i].Kind < pruned[j].Kind
		}
		if pruned[i].From != pruned[j].From {
			return pruned[i].From < pruned[j].From
		}
		return pruned[i].To < pruned[j].To
	})
	return pruned
}

// warnPrunedEdges prints a human-readable warning block to stderr listing the
// dropped edges. No-op when nothing was pruned (a full plan).
func warnPrunedEdges(edges []model.PrunedEdge) {
	if len(edges) == 0 {
		return
	}
	color := ui.ColorEnabledForWriter(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s dropped %d dependency edge(s) not in this plan's selection:\n",
		ui.Yellow(color, "warning:"), len(edges))
	for _, e := range edges {
		fmt.Fprintf(os.Stderr, "    %-9s %s → %s  (%s)\n",
			e.Kind, e.From, e.To, strings.ReplaceAll(e.Reason, "-", " "))
	}
}

func selectionSortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
