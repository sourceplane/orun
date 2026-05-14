package trigger

import (
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func TestValidateIntent_Valid(t *testing.T) {
	intent := testIntent()
	errs := ValidateIntent(intent)
	if len(errs) > 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidateIntent_NoAutomation(t *testing.T) {
	intent := &model.Intent{}
	errs := ValidateIntent(intent)
	if len(errs) != 0 {
		t.Errorf("expected no errors for intent without automation, got %v", errs)
	}
}

func TestValidateIntent_MissingProvider(t *testing.T) {
	intent := &model.Intent{
		Automation: model.AutomationConfig{
			TriggerBindings: map[string]model.TriggerBinding{
				"bad-trigger": {
					On: model.TriggerMatch{
						Event: "push",
					},
				},
			},
		},
	}

	errs := ValidateIntent(intent)
	if len(errs) == 0 {
		t.Fatal("expected validation error for missing provider")
	}
	found := false
	for _, err := range errs {
		if err.Error() == `trigger binding "bad-trigger": on.provider is required` {
			found = true
		}
	}
	if !found {
		t.Errorf("expected provider error, got %v", errs)
	}
}

func TestValidateIntent_MissingEvent(t *testing.T) {
	intent := &model.Intent{
		Automation: model.AutomationConfig{
			TriggerBindings: map[string]model.TriggerBinding{
				"bad-trigger": {
					On: model.TriggerMatch{
						Provider: "github",
					},
				},
			},
		},
	}

	errs := ValidateIntent(intent)
	if len(errs) == 0 {
		t.Fatal("expected validation error for missing event")
	}
}

func TestValidateIntent_InvalidScope(t *testing.T) {
	intent := &model.Intent{
		Automation: model.AutomationConfig{
			TriggerBindings: map[string]model.TriggerBinding{
				"bad-scope": {
					On: model.TriggerMatch{
						Provider: "github",
						Event:    "push",
					},
					Plan: model.TriggerPlanOptions{
						Scope: "invalid",
					},
				},
			},
		},
	}

	errs := ValidateIntent(intent)
	if len(errs) == 0 {
		t.Fatal("expected validation error for invalid scope")
	}
}

func TestValidateIntent_UnknownTriggerRef(t *testing.T) {
	intent := &model.Intent{
		Automation: model.AutomationConfig{
			TriggerBindings: map[string]model.TriggerBinding{
				"valid-trigger": {
					On: model.TriggerMatch{Provider: "github", Event: "push"},
				},
			},
		},
		Environments: map[string]model.Environment{
			"dev": {
				Activation: model.EnvironmentActivation{
					TriggerRefs: []string{"nonexistent-trigger"},
				},
			},
		},
	}

	errs := ValidateIntent(intent)
	if len(errs) == 0 {
		t.Fatal("expected validation error for unknown trigger ref")
	}
}

func TestValidateIntent_DuplicateTriggerRef(t *testing.T) {
	intent := &model.Intent{
		Automation: model.AutomationConfig{
			TriggerBindings: map[string]model.TriggerBinding{
				"my-trigger": {
					On: model.TriggerMatch{Provider: "github", Event: "push"},
				},
			},
		},
		Environments: map[string]model.Environment{
			"dev": {
				Activation: model.EnvironmentActivation{
					TriggerRefs: []string{"my-trigger", "my-trigger"},
				},
			},
		},
	}

	errs := ValidateIntent(intent)
	if len(errs) == 0 {
		t.Fatal("expected validation error for duplicate trigger ref")
	}
}

func TestValidateWarnings_UnreferencedBinding(t *testing.T) {
	intent := &model.Intent{
		Automation: model.AutomationConfig{
			TriggerBindings: map[string]model.TriggerBinding{
				"orphan-trigger": {
					On: model.TriggerMatch{Provider: "github", Event: "push"},
				},
			},
		},
		Environments: map[string]model.Environment{
			"dev": {},
		},
	}

	warnings := ValidateWarnings(intent)
	if len(warnings) == 0 {
		t.Fatal("expected warning for unreferenced trigger binding")
	}
}

func TestValidateTriggerContext_NamedTrigger(t *testing.T) {
	intent := testIntent()

	err := ValidateTriggerContext(intent, model.TriggerContext{
		Mode:        "named-trigger",
		TriggerName: "github-pull-request",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = ValidateTriggerContext(intent, model.TriggerContext{
		Mode:        "named-trigger",
		TriggerName: "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent trigger")
	}
}

func TestValidateTriggerContext_EventFile(t *testing.T) {
	intent := testIntent()

	err := ValidateTriggerContext(intent, model.TriggerContext{
		Mode:  "event-file",
		Event: &model.NormalizedEvent{Provider: "github"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = ValidateTriggerContext(intent, model.TriggerContext{
		Mode:  "event-file",
		Event: nil,
	})
	if err == nil {
		t.Fatal("expected error for nil event")
	}

	err = ValidateTriggerContext(intent, model.TriggerContext{
		Mode:  "event-file",
		Event: &model.NormalizedEvent{},
	})
	if err == nil {
		t.Fatal("expected error for missing provider")
	}
}

func TestFormatErrors(t *testing.T) {
	err := FormatErrors(nil)
	if err != nil {
		t.Fatal("expected nil for no errors")
	}

	err = FormatErrors([]error{})
	if err != nil {
		t.Fatal("expected nil for empty errors")
	}
}
