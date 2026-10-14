package trigger

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/model"
)

var laneGroupNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ValidateLanes checks execution.lanes and every component's spec.lane
// (specs/orun-lanes §3): modes are known, numbers are positive, group names
// are well-formed and not reserved, rules name existing trigger bindings, and
// a component's lane is `isolated` or a declared group. An intent that
// declares no lanes and annotates no component is valid by construction.
func ValidateLanes(intent *model.Intent) []error {
	var errs []error
	lanes := intent.Execution.Lanes

	// execution.defaults rides along: every place lanes are validated is a
	// place the intent's run-time bounds must hold too.
	if d := intent.Execution.Defaults; d != nil && strings.TrimSpace(d.Timeout) != "" {
		if v, err := time.ParseDuration(strings.TrimSpace(d.Timeout)); err != nil || v <= 0 {
			errs = append(errs, fmt.Errorf("execution.defaults.timeout %q is not a positive duration (e.g. 45m)", d.Timeout))
		}
	}
	if lanes != nil && strings.TrimSpace(lanes.UpstreamWait) != "" {
		if v, err := time.ParseDuration(strings.TrimSpace(lanes.UpstreamWait)); err != nil || v <= 0 {
			errs = append(errs, fmt.Errorf("execution.lanes.upstreamWait %q is not a positive duration (e.g. 90m)", lanes.UpstreamWait))
		}
	}

	groups := map[string]struct{}{}
	if lanes != nil {
		if !model.IsValidLaneMode(lanes.Mode) {
			errs = append(errs, fmt.Errorf(
				"execution.lanes.mode %q is not one of job|component|env|auto", lanes.Mode))
		}
		if lanes.MaxLanes < 0 {
			errs = append(errs, fmt.Errorf("execution.lanes.maxLanes must be at least 1, got %d", lanes.MaxLanes))
		}
		if lanes.Concurrency < 0 && !lanes.Concurrency.IsAuto() {
			errs = append(errs, fmt.Errorf("execution.lanes.concurrency must be at least 1 or auto, got %d", lanes.Concurrency))
		}
		switch lanes.OnCycle {
		case "", model.LaneOnCycleSplit, model.LaneOnCycleReject:
		default:
			errs = append(errs, fmt.Errorf(
				"execution.lanes.onCycle %q is not one of split|reject", lanes.OnCycle))
		}

		names := make([]string, 0, len(lanes.Groups))
		for name := range lanes.Groups {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			switch {
			case model.IsReservedLaneName(name):
				errs = append(errs, fmt.Errorf(
					"execution.lanes.groups.%s: %q is reserved (job, auto and isolated cannot name a group)", name, name))
			case !laneGroupNameRE.MatchString(name):
				errs = append(errs, fmt.Errorf(
					"execution.lanes.groups.%s: a group name is lowercase letters, digits and dashes", name))
			default:
				groups[name] = struct{}{}
			}
			if g := lanes.Groups[name]; g.Concurrency < 0 && !g.Concurrency.IsAuto() {
				errs = append(errs, fmt.Errorf(
					"execution.lanes.groups.%s.concurrency must be at least 1 or auto, got %d", name, g.Concurrency))
			}
		}

		for i, rule := range lanes.Rules {
			if rule.Mode == "" {
				errs = append(errs, fmt.Errorf("execution.lanes.rules[%d].mode is required", i))
			} else if !model.IsValidLaneMode(rule.Mode) {
				errs = append(errs, fmt.Errorf(
					"execution.lanes.rules[%d].mode %q is not one of job|component|env|auto", i, rule.Mode))
			}
			if rule.When.TriggerRef == "" {
				errs = append(errs, fmt.Errorf("execution.lanes.rules[%d].when.triggerRef is required", i))
			} else if _, ok := intent.Automation.TriggerBindings[rule.When.TriggerRef]; !ok {
				errs = append(errs, fmt.Errorf(
					"execution.lanes.rules[%d].when.triggerRef %q does not exist in automation.triggerBindings",
					i, rule.When.TriggerRef))
			}
		}
	}

	for _, comp := range intent.Components {
		if comp.Lane == "" || comp.Lane == model.LaneIsolated {
			continue
		}
		if _, ok := groups[comp.Lane]; !ok {
			errs = append(errs, fmt.Errorf(
				"component %q: lane %q is not a group in execution.lanes.groups (or %q)",
				comp.Name, comp.Lane, model.LaneIsolated))
		}
	}

	return errs
}
