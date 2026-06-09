package main

import (
	"sort"

	"github.com/sourceplane/orun/internal/model"
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

func selectionSortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
