package lanes

import (
	"github.com/sourceplane/orun/internal/model"
)

// ForIntent builds Options from a loaded intent: execution.lanes, every
// component's spec.lane, and the trigger bindings that fired.
func ForIntent(intent *model.Intent, matchedTriggers []string, strict bool, checkName func(model.PlanJob) string) Options {
	opts := Options{
		MatchedTriggers: matchedTriggers,
		Strict:          strict,
		CheckName:       checkName,
	}
	if intent == nil {
		return opts
	}
	opts.Config = intent.Execution.Lanes
	for _, comp := range intent.Components {
		if comp.Lane == "" {
			continue
		}
		if opts.ComponentLanes == nil {
			opts.ComponentLanes = map[string]string{}
		}
		opts.ComponentLanes[comp.Name] = comp.Lane
	}
	return opts
}
