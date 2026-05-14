package trigger

import (
	"fmt"
	"strings"

	"github.com/sourceplane/orun/internal/model"
)

// ValidateIntent performs static validation of trigger bindings and environment activation references.
func ValidateIntent(intent *model.Intent) []error {
	var errs []error

	if intent.Automation.TriggerBindings == nil {
		return nil
	}

	bindingNames := make(map[string]struct{})
	for name, binding := range intent.Automation.TriggerBindings {
		bindingNames[name] = struct{}{}

		if name == "" {
			errs = append(errs, fmt.Errorf("automation.triggerBindings: binding name must be non-empty"))
		}
		if binding.On.Provider == "" {
			errs = append(errs, fmt.Errorf("trigger binding %q: on.provider is required", name))
		}
		if binding.On.Event == "" {
			errs = append(errs, fmt.Errorf("trigger binding %q: on.event is required", name))
		}
		if binding.Plan.Scope != "" && binding.Plan.Scope != "full" && binding.Plan.Scope != "changed" {
			errs = append(errs, fmt.Errorf("trigger binding %q: plan.scope must be \"full\" or \"changed\", got %q", name, binding.Plan.Scope))
		}
	}

	// Validate environment triggerRefs point to existing bindings
	for envName, env := range intent.Environments {
		seen := make(map[string]struct{})
		for _, ref := range env.Activation.TriggerRefs {
			if _, exists := bindingNames[ref]; !exists {
				errs = append(errs, fmt.Errorf("environment %q references unknown trigger binding %q", envName, ref))
			}
			if _, dup := seen[ref]; dup {
				errs = append(errs, fmt.Errorf("environment %q has duplicate triggerRef %q", envName, ref))
			}
			seen[ref] = struct{}{}
		}
	}

	return errs
}

// ValidateWarnings returns non-fatal warnings (e.g., unreferenced bindings).
func ValidateWarnings(intent *model.Intent) []string {
	var warnings []string

	if intent.Automation.TriggerBindings == nil {
		return nil
	}

	for name := range intent.Automation.TriggerBindings {
		referenced := false
		for _, env := range intent.Environments {
			for _, ref := range env.Activation.TriggerRefs {
				if ref == name {
					referenced = true
					break
				}
			}
			if referenced {
				break
			}
		}
		if !referenced {
			warnings = append(warnings, fmt.Sprintf("trigger binding %q is defined but no environment references it", name))
		}
	}

	return warnings
}

// ValidateTriggerContext performs event-time validation.
func ValidateTriggerContext(intent *model.Intent, ctx model.TriggerContext) error {
	switch ctx.Mode {
	case "named-trigger":
		if intent.Automation.TriggerBindings == nil {
			return fmt.Errorf("trigger binding %q was not found in automation.triggerBindings", ctx.TriggerName)
		}
		if _, exists := intent.Automation.TriggerBindings[ctx.TriggerName]; !exists {
			return fmt.Errorf("trigger binding %q was not found in automation.triggerBindings", ctx.TriggerName)
		}
	case "event-file":
		if ctx.Event == nil {
			return fmt.Errorf("event data is required for event-file mode")
		}
		if ctx.Event.Provider == "" {
			return fmt.Errorf("event provider is required")
		}
	}

	return nil
}

// FormatErrors joins multiple validation errors into a single message.
func FormatErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	msgs := make([]string, len(errs))
	for i, err := range errs {
		msgs[i] = err.Error()
	}
	return fmt.Errorf("trigger validation failed:\n  %s", strings.Join(msgs, "\n  "))
}
