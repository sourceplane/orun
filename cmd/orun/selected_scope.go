package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/model"
)

// planAutoPushDisabled records that a binding which resolved this plan sets
// plan.autopush: false; `orun plan` then skips the best-effort catalog push
// (orun-manual-triggers §5). generatePlan resets it on every call.
var planAutoPushDisabled bool

// autoPushCatalog is the best-effort plan-time catalog push; a variable so a
// test can observe whether a plan reached it.
var autoPushCatalog = maybeAutoPushCatalog

// autoPushAfterPlan runs the config-driven catalog push unless the binding
// that resolved the plan said plan.autopush: false.
func autoPushAfterPlan(ctx context.Context) {
	if planAutoPushDisabled {
		autopushVerbosef("catalog auto-sync: skipped (the trigger sets plan.autopush: false)")
		return
	}
	autoPushCatalog(ctx)
}

// selectedComponentsFor returns a selected plan's components: the event's
// selection, narrowed by --component when both are given, or --component
// alone under a named trigger (which has no event to read).
func selectedComponentsFor(res *model.TriggerResolution, flags []string) ([]string, error) {
	fromEvent := res.SelectedComponents
	switch {
	case len(fromEvent) > 0 && len(flags) > 0:
		want := make(map[string]struct{}, len(flags))
		for _, f := range flags {
			want[f] = struct{}{}
		}
		var both []string
		for _, c := range fromEvent {
			if _, ok := want[c]; ok {
				both = append(both, c)
			}
		}
		if len(both) == 0 {
			return nil, fmt.Errorf("--component %s selects none of the trigger's components (%s)", strings.Join(flags, ", "), strings.Join(fromEvent, ", "))
		}
		return both, nil
	case len(fromEvent) > 0:
		return fromEvent, nil
	case len(flags) > 0:
		return flags, nil
	default:
		return nil, fmt.Errorf("trigger %s plans a selected scope: no components were selected (name them with --component)", strings.Join(res.MatchedTriggerNames, ", "))
	}
}

// selectInstances narrows the expansion to a selected plan's lanes: exactly
// the named components in the named environments. Names are exact. It fails,
// naming every cause, on a name with a wildcard, a component the intent does
// not have, or a component that does not subscribe to a selected environment
// — a selected plan is never wider or narrower than what was asked for.
func selectInstances(instances map[string][]*model.ComponentInstance, components, envs []string) (map[string][]*model.ComponentInstance, error) {
	var problems []string
	for _, c := range components {
		if strings.ContainsAny(c, "*?[]") {
			problems = append(problems, fmt.Sprintf("component name %q is not exact (a selected plan takes no wildcards)", c))
		}
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("selected plan: %s", strings.Join(problems, "; "))
	}

	known := make(map[string]struct{})
	byEnv := make(map[string]map[string]*model.ComponentInstance, len(instances))
	for env, insts := range instances {
		byEnv[env] = make(map[string]*model.ComponentInstance, len(insts))
		for _, inst := range insts {
			known[inst.ComponentName] = struct{}{}
			byEnv[env][inst.ComponentName] = inst
		}
	}

	var unknown []string
	for _, c := range components {
		if _, ok := known[c]; !ok {
			unknown = append(unknown, c)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("selected plan: unknown component %s", strings.Join(unknown, ", "))
	}

	selected := make(map[string][]*model.ComponentInstance, len(envs))
	for _, env := range envs {
		for _, c := range components {
			inst, ok := byEnv[env][c]
			if !ok {
				problems = append(problems, fmt.Sprintf("%s does not subscribe to %s", c, env))
				continue
			}
			selected[env] = append(selected[env], inst)
		}
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("selected plan: %s", strings.Join(problems, "; "))
	}
	return selected, nil
}
