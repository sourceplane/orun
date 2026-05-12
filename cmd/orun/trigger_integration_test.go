package main

import (
	"testing"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/trigger"
)

func TestResolveTriggerAndProfile_ProfileDirect(t *testing.T) {
	normalized := &model.NormalizedIntent{
		Execution: model.IntentExecution{
			Profiles: map[string]model.ExecutionProfile{
				"verify": {Controls: map[string]map[string]interface{}{}},
			},
		},
	}

	// Save and restore globals
	oldProfile := profileName
	oldTrigger := triggerName
	oldFromCI := fromCI
	oldEventFile := eventFile
	defer func() {
		profileName = oldProfile
		triggerName = oldTrigger
		fromCI = oldFromCI
		eventFile = oldEventFile
	}()

	profileName = "verify"
	triggerName = ""
	fromCI = ""
	eventFile = ""

	profile, trigName, err := resolveTriggerAndProfile(normalized)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile != "verify" {
		t.Fatalf("profile = %q, want verify", profile)
	}
	if trigName != "" {
		t.Fatalf("triggerName = %q, want empty", trigName)
	}
}

func TestResolveTriggerAndProfile_ProfileNotFound(t *testing.T) {
	normalized := &model.NormalizedIntent{
		Execution: model.IntentExecution{
			Profiles: map[string]model.ExecutionProfile{},
		},
	}

	oldProfile := profileName
	defer func() { profileName = oldProfile }()

	profileName = "nonexistent"
	triggerName = ""
	fromCI = ""
	eventFile = ""

	_, _, err := resolveTriggerAndProfile(normalized)
	if err == nil {
		t.Fatal("expected error for unknown profile")
	}
}

func TestResolveTriggerAndProfile_TriggerByName(t *testing.T) {
	normalized := &model.NormalizedIntent{
		Execution: model.IntentExecution{
			Profiles: map[string]model.ExecutionProfile{
				"dry-run": {Controls: map[string]map[string]interface{}{}},
			},
		},
		Automation: model.IntentAutomation{
			Triggers: []model.AutomationTrigger{
				{
					Name: "pr",
					On:   model.TriggerOn{Event: "pull_request"},
					Plan: model.TriggerPlan{Profile: "dry-run"},
				},
			},
		},
	}

	oldProfile := profileName
	oldTrigger := triggerName
	defer func() {
		profileName = oldProfile
		triggerName = oldTrigger
	}()

	profileName = ""
	triggerName = "pr"
	fromCI = ""
	eventFile = ""

	profile, trigName, err := resolveTriggerAndProfile(normalized)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile != "dry-run" {
		t.Fatalf("profile = %q, want dry-run", profile)
	}
	if trigName != "pr" {
		t.Fatalf("triggerName = %q, want pr", trigName)
	}
}

func TestResolveTriggerAndProfile_TriggerNotFound(t *testing.T) {
	normalized := &model.NormalizedIntent{
		Automation: model.IntentAutomation{},
	}

	oldTrigger := triggerName
	defer func() { triggerName = oldTrigger }()

	profileName = ""
	triggerName = "nonexistent"
	fromCI = ""
	eventFile = ""

	_, _, err := resolveTriggerAndProfile(normalized)
	if err == nil {
		t.Fatal("expected error for unknown trigger")
	}
}

func TestResolveTriggerAndProfile_MutuallyExclusive(t *testing.T) {
	normalized := &model.NormalizedIntent{}

	oldProfile := profileName
	oldTrigger := triggerName
	defer func() {
		profileName = oldProfile
		triggerName = oldTrigger
	}()

	profileName = "verify"
	triggerName = "pr"
	fromCI = ""
	eventFile = ""

	_, _, err := resolveTriggerAndProfile(normalized)
	if err == nil {
		t.Fatal("expected error for mutually exclusive flags")
	}
}

func TestResolveTriggerAndProfile_NoFlags(t *testing.T) {
	normalized := &model.NormalizedIntent{}

	oldProfile := profileName
	oldTrigger := triggerName
	defer func() {
		profileName = oldProfile
		triggerName = oldTrigger
	}()

	profileName = ""
	triggerName = ""
	fromCI = ""
	eventFile = ""

	profile, trigName, err := resolveTriggerAndProfile(normalized)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile != "" || trigName != "" {
		t.Fatalf("expected empty profile/trigger, got %q/%q", profile, trigName)
	}
}

func TestTriggerMatchSelectsCorrectProfile(t *testing.T) {
	triggers := []model.AutomationTrigger{
		{
			Name: "pr",
			On:   model.TriggerOn{Provider: "github", Event: "pull_request"},
			Plan: model.TriggerPlan{Profile: "dry-run"},
		},
		{
			Name: "push-main",
			On:   model.TriggerOn{Provider: "github", Event: "push", Branches: []string{"main"}},
			Plan: model.TriggerPlan{Profile: "verify"},
		},
		{
			Name: "tag-release",
			On:   model.TriggerOn{Provider: "github", Event: "push", Tags: []string{"v*"}},
			Plan: model.TriggerPlan{Profile: "release"},
		},
	}

	cases := []struct {
		name        string
		event       *model.EventContext
		wantProfile string
		wantTrigger string
	}{
		{
			name:        "PR event",
			event:       &model.EventContext{Provider: "github", Event: "pull_request", Action: "opened"},
			wantProfile: "dry-run",
			wantTrigger: "pr",
		},
		{
			name:        "push to main",
			event:       &model.EventContext{Provider: "github", Event: "push", Branch: "main", Ref: "refs/heads/main"},
			wantProfile: "verify",
			wantTrigger: "push-main",
		},
		{
			name:        "tag push",
			event:       &model.EventContext{Provider: "github", Event: "push", Ref: "refs/tags/v1.0.0"},
			wantProfile: "release",
			wantTrigger: "tag-release",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := trigger.Match(triggers, tc.event)
			if result == nil {
				t.Fatal("expected match")
			}
			if result.Profile != tc.wantProfile {
				t.Fatalf("profile = %q, want %q", result.Profile, tc.wantProfile)
			}
			if result.Trigger.Name != tc.wantTrigger {
				t.Fatalf("trigger = %q, want %q", result.Trigger.Name, tc.wantTrigger)
			}
		})
	}
}

func TestProfileFilterEnvironments(t *testing.T) {
	envs := map[string]model.Environment{
		"dev": {
			Execution: model.EnvironmentExecution{Profile: "dry-run"},
		},
		"staging": {
			Execution: model.EnvironmentExecution{Profile: "verify"},
		},
		"production": {
			Execution: model.EnvironmentExecution{Profile: "release"},
		},
		"preview": {
			Execution: model.EnvironmentExecution{Profile: "dry-run"},
		},
	}

	cases := []struct {
		profile  string
		wantEnvs []string
	}{
		{"dry-run", []string{"dev", "preview"}},
		{"verify", []string{"staging"}},
		{"release", []string{"production"}},
	}

	for _, tc := range cases {
		t.Run(tc.profile, func(t *testing.T) {
			var matched []string
			for envName, env := range envs {
				if env.Execution.Profile == tc.profile {
					matched = append(matched, envName)
				}
			}
			if len(matched) != len(tc.wantEnvs) {
				t.Fatalf("matched %d envs, want %d", len(matched), len(tc.wantEnvs))
			}
			for _, want := range tc.wantEnvs {
				found := false
				for _, got := range matched {
					if got == want {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected env %q in matched set", want)
				}
			}
		})
	}
}

func TestProfilePlanScopeImpliesChanged(t *testing.T) {
	profiles := map[string]model.ExecutionProfile{
		"dry-run": {
			Plan: model.ProfilePlan{Scope: "changed"},
		},
		"release": {
			Plan: model.ProfilePlan{Scope: "full"},
		},
		"default": {
			Plan: model.ProfilePlan{},
		},
	}

	cases := []struct {
		profile     string
		wantChanged bool
	}{
		{"dry-run", true},
		{"release", false},
		{"default", false},
	}

	for _, tc := range cases {
		t.Run(tc.profile, func(t *testing.T) {
			p := profiles[tc.profile]
			impliesChanged := p.Plan.Scope == "changed"
			if impliesChanged != tc.wantChanged {
				t.Fatalf("scope=%q implies changed=%v, want %v", p.Plan.Scope, impliesChanged, tc.wantChanged)
			}
		})
	}
}

func TestResolveTriggerAndProfile_AutoDetect_PREvent(t *testing.T) {
	normalized := &model.NormalizedIntent{
		Execution: model.IntentExecution{
			Profiles: map[string]model.ExecutionProfile{
				"dry-run": {Controls: map[string]map[string]interface{}{}},
				"verify":  {Controls: map[string]map[string]interface{}{}},
			},
		},
		Automation: model.IntentAutomation{
			Triggers: []model.AutomationTrigger{
				{
					Name: "github-pull-request",
					On: model.TriggerOn{
						Provider: "github",
						Event:    "pull_request",
						Actions:  []string{"opened", "synchronize", "reopened"},
					},
					Plan: model.TriggerPlan{Profile: "dry-run"},
				},
				{
					Name: "github-push-main",
					On: model.TriggerOn{
						Provider: "github",
						Event:    "push",
						Branches: []string{"main"},
					},
					Plan: model.TriggerPlan{Profile: "verify"},
				},
			},
		},
	}

	oldProfile := profileName
	oldTrigger := triggerName
	oldFromCI := fromCI
	oldEventFile := eventFile
	oldBuildEventCtx := buildEventCtx
	defer func() {
		profileName = oldProfile
		triggerName = oldTrigger
		fromCI = oldFromCI
		eventFile = oldEventFile
		buildEventCtx = oldBuildEventCtx
	}()

	profileName = ""
	triggerName = ""
	fromCI = ""
	eventFile = ""

	buildEventCtx = func(_ func(string) string, _ func(string) ([]byte, error)) *model.EventContext {
		return &model.EventContext{
			Provider: "github",
			Event:    "pull_request",
			Action:   "opened",
			Branch:   "main",
		}
	}

	profile, trigName, err := resolveTriggerAndProfile(normalized)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile != "dry-run" {
		t.Fatalf("profile = %q, want dry-run", profile)
	}
	if trigName != "github-pull-request" {
		t.Fatalf("triggerName = %q, want github-pull-request", trigName)
	}
}

func TestResolveTriggerAndProfile_AutoDetect_PushToMain(t *testing.T) {
	normalized := &model.NormalizedIntent{
		Execution: model.IntentExecution{
			Profiles: map[string]model.ExecutionProfile{
				"dry-run": {Controls: map[string]map[string]interface{}{}},
				"verify":  {Controls: map[string]map[string]interface{}{}},
			},
		},
		Automation: model.IntentAutomation{
			Triggers: []model.AutomationTrigger{
				{
					Name: "github-pull-request",
					On:   model.TriggerOn{Provider: "github", Event: "pull_request"},
					Plan: model.TriggerPlan{Profile: "dry-run"},
				},
				{
					Name: "github-push-main",
					On:   model.TriggerOn{Provider: "github", Event: "push", Branches: []string{"main"}},
					Plan: model.TriggerPlan{Profile: "verify"},
				},
			},
		},
	}

	oldProfile := profileName
	oldTrigger := triggerName
	oldFromCI := fromCI
	oldEventFile := eventFile
	oldBuildEventCtx := buildEventCtx
	defer func() {
		profileName = oldProfile
		triggerName = oldTrigger
		fromCI = oldFromCI
		eventFile = oldEventFile
		buildEventCtx = oldBuildEventCtx
	}()

	profileName = ""
	triggerName = ""
	fromCI = ""
	eventFile = ""

	buildEventCtx = func(_ func(string) string, _ func(string) ([]byte, error)) *model.EventContext {
		return &model.EventContext{
			Provider: "github",
			Event:    "push",
			Branch:   "main",
			Ref:      "refs/heads/main",
		}
	}

	profile, trigName, err := resolveTriggerAndProfile(normalized)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile != "verify" {
		t.Fatalf("profile = %q, want verify", profile)
	}
	if trigName != "github-push-main" {
		t.Fatalf("triggerName = %q, want github-push-main", trigName)
	}
}

func TestResolveTriggerAndProfile_AutoDetect_NoCI(t *testing.T) {
	normalized := &model.NormalizedIntent{
		Execution: model.IntentExecution{
			Profiles: map[string]model.ExecutionProfile{
				"dry-run": {Controls: map[string]map[string]interface{}{}},
			},
		},
		Automation: model.IntentAutomation{
			Triggers: []model.AutomationTrigger{
				{
					Name: "github-pull-request",
					On:   model.TriggerOn{Provider: "github", Event: "pull_request"},
					Plan: model.TriggerPlan{Profile: "dry-run"},
				},
			},
		},
	}

	oldProfile := profileName
	oldTrigger := triggerName
	oldFromCI := fromCI
	oldEventFile := eventFile
	oldBuildEventCtx := buildEventCtx
	defer func() {
		profileName = oldProfile
		triggerName = oldTrigger
		fromCI = oldFromCI
		eventFile = oldEventFile
		buildEventCtx = oldBuildEventCtx
	}()

	profileName = ""
	triggerName = ""
	fromCI = ""
	eventFile = ""

	buildEventCtx = func(_ func(string) string, _ func(string) ([]byte, error)) *model.EventContext {
		return nil
	}

	profile, trigName, err := resolveTriggerAndProfile(normalized)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile != "" || trigName != "" {
		t.Fatalf("expected empty profile/trigger for non-CI env, got %q/%q", profile, trigName)
	}
}

func TestResolveTriggerAndProfile_AutoDetect_NoTriggerMatch(t *testing.T) {
	normalized := &model.NormalizedIntent{
		Execution: model.IntentExecution{
			Profiles: map[string]model.ExecutionProfile{
				"dry-run": {Controls: map[string]map[string]interface{}{}},
			},
		},
		Automation: model.IntentAutomation{
			Triggers: []model.AutomationTrigger{
				{
					Name: "github-pull-request",
					On:   model.TriggerOn{Provider: "github", Event: "pull_request"},
					Plan: model.TriggerPlan{Profile: "dry-run"},
				},
			},
		},
	}

	oldProfile := profileName
	oldTrigger := triggerName
	oldFromCI := fromCI
	oldEventFile := eventFile
	oldBuildEventCtx := buildEventCtx
	defer func() {
		profileName = oldProfile
		triggerName = oldTrigger
		fromCI = oldFromCI
		eventFile = oldEventFile
		buildEventCtx = oldBuildEventCtx
	}()

	profileName = ""
	triggerName = ""
	fromCI = ""
	eventFile = ""

	buildEventCtx = func(_ func(string) string, _ func(string) ([]byte, error)) *model.EventContext {
		return &model.EventContext{
			Provider: "github",
			Event:    "workflow_dispatch",
		}
	}

	profile, trigName, err := resolveTriggerAndProfile(normalized)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile != "" || trigName != "" {
		t.Fatalf("expected empty profile/trigger for unmatched event, got %q/%q", profile, trigName)
	}
}

func TestResolveTriggerAndProfile_AutoDetect_NoTriggers(t *testing.T) {
	normalized := &model.NormalizedIntent{
		Execution: model.IntentExecution{
			Profiles: map[string]model.ExecutionProfile{
				"dry-run": {Controls: map[string]map[string]interface{}{}},
			},
		},
	}

	oldProfile := profileName
	oldTrigger := triggerName
	oldFromCI := fromCI
	oldEventFile := eventFile
	oldBuildEventCtx := buildEventCtx
	defer func() {
		profileName = oldProfile
		triggerName = oldTrigger
		fromCI = oldFromCI
		eventFile = oldEventFile
		buildEventCtx = oldBuildEventCtx
	}()

	profileName = ""
	triggerName = ""
	fromCI = ""
	eventFile = ""

	buildEventCtx = func(_ func(string) string, _ func(string) ([]byte, error)) *model.EventContext {
		return &model.EventContext{
			Provider: "github",
			Event:    "pull_request",
			Action:   "opened",
		}
	}

	profile, trigName, err := resolveTriggerAndProfile(normalized)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile != "" || trigName != "" {
		t.Fatalf("expected empty profile/trigger when no triggers defined, got %q/%q", profile, trigName)
	}
}

func TestResolveTriggerAndProfile_ExplicitFlagOverridesAutoDetect(t *testing.T) {
	normalized := &model.NormalizedIntent{
		Execution: model.IntentExecution{
			Profiles: map[string]model.ExecutionProfile{
				"dry-run": {Controls: map[string]map[string]interface{}{}},
				"verify":  {Controls: map[string]map[string]interface{}{}},
			},
		},
		Automation: model.IntentAutomation{
			Triggers: []model.AutomationTrigger{
				{
					Name: "github-pull-request",
					On:   model.TriggerOn{Provider: "github", Event: "pull_request"},
					Plan: model.TriggerPlan{Profile: "dry-run"},
				},
			},
		},
	}

	oldProfile := profileName
	oldTrigger := triggerName
	oldFromCI := fromCI
	oldEventFile := eventFile
	oldBuildEventCtx := buildEventCtx
	defer func() {
		profileName = oldProfile
		triggerName = oldTrigger
		fromCI = oldFromCI
		eventFile = oldEventFile
		buildEventCtx = oldBuildEventCtx
	}()

	profileName = "verify"
	triggerName = ""
	fromCI = ""
	eventFile = ""

	buildEventCtx = func(_ func(string) string, _ func(string) ([]byte, error)) *model.EventContext {
		return &model.EventContext{
			Provider: "github",
			Event:    "pull_request",
			Action:   "opened",
		}
	}

	profile, trigName, err := resolveTriggerAndProfile(normalized)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile != "verify" {
		t.Fatalf("profile = %q, want verify (explicit flag should override auto-detect)", profile)
	}
	if trigName != "" {
		t.Fatalf("triggerName = %q, want empty", trigName)
	}
}
