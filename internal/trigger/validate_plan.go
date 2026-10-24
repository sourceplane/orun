package trigger

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/model"
)

// ValidatePlanOptions checks each trigger binding's plan options
// (orun-manual-triggers §7): the scope is known; a selected scope names where
// its components and environments are in the event, and no other scope does;
// an event path has no empty segment.
func ValidatePlanOptions(intent *model.Intent) []error {
	var errs []error
	names := make([]string, 0, len(intent.Automation.TriggerBindings))
	for name := range intent.Automation.TriggerBindings {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		plan := intent.Automation.TriggerBindings[name].Plan
		switch plan.Scope {
		case "", "full", "changed", model.PlanScopeSelected:
		default:
			errs = append(errs, fmt.Errorf("trigger binding %q: plan.scope %q is invalid (want full, changed or selected)", name, plan.Scope))
		}

		selected := plan.Scope == model.PlanScopeSelected
		for _, f := range []struct{ field, path string }{
			{"plan.components", plan.Components},
			{"plan.environments", plan.Environments},
		} {
			switch {
			case selected && f.path == "":
				errs = append(errs, fmt.Errorf("trigger binding %q: %s is required with scope: selected", name, f.field))
			case !selected && f.path != "":
				errs = append(errs, fmt.Errorf("trigger binding %q: %s is only valid with scope: selected", name, f.field))
			case f.path != "" && !validEventPath(f.path):
				errs = append(errs, fmt.Errorf("trigger binding %q: %s %q is not a dot path (e.g. inputs.component)", name, f.field, f.path))
			}
		}
	}
	return errs
}

func validEventPath(path string) bool {
	for _, part := range strings.Split(path, ".") {
		if strings.TrimSpace(part) == "" || part != strings.TrimSpace(part) {
			return false
		}
	}
	return true
}
