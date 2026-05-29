package triggerctx

import (
	"errors"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func minimalIntent() *model.Intent {
	return &model.Intent{
		Automation: model.AutomationConfig{
			TriggerBindings: map[string]model.TriggerBinding{
				"github-pull-request": {
					On:   model.TriggerMatch{Provider: "github", Event: "pull_request"},
					Plan: model.TriggerPlanOptions{Scope: "changed"},
				},
				"github-push-main": {
					On:   model.TriggerMatch{Provider: "github", Event: "push", Branches: []string{"main"}},
					Plan: model.TriggerPlanOptions{Scope: "full"},
				},
			},
		},
	}
}

func TestFromDeclaredTrigger_Success(t *testing.T) {
	t.Parallel()
	intent := minimalIntent()
	occ, err := FromDeclaredTrigger(intent, DeclaredOptions{
		TriggerName: "github-pull-request",
		Source: TriggerSource{
			Repo: "sourceplane/orun", SourceScope: "pr-139", HeadRevision: "def456a1b2c3", BaseRevision: "abc1239f8e7d",
		},
		Action: "synchronize",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if occ.TriggerType != TriggerTypeDeclared {
		t.Errorf("TriggerType = %q", occ.TriggerType)
	}
	if occ.Provider != "github" || occ.Event != "pull_request" {
		t.Errorf("provider/event defaults wrong: %+v", occ)
	}
	if occ.PlanScope.Mode != PlanScopeChanged {
		t.Errorf("PlanScope.Mode = %q want changed (from binding)", occ.PlanScope.Mode)
	}
	if occ.PlanScope.Base != "abc1239f8e7d" || occ.PlanScope.Head != "def456a1b2c3" {
		t.Errorf("changed-scope base/head not populated from source: %+v", occ.PlanScope)
	}
	if occ.Mode != ModeEventFile {
		t.Errorf("Mode = %q want event-file (action present)", occ.Mode)
	}
	if occ.TriggerKey != "trg-pr-139-def456a" {
		t.Errorf("TriggerKey = %q", occ.TriggerKey)
	}
}

func TestFromDeclaredTrigger_UnknownBinding(t *testing.T) {
	t.Parallel()
	_, err := FromDeclaredTrigger(minimalIntent(), DeclaredOptions{TriggerName: "nope"})
	if err == nil {
		t.Fatal("expected error for unknown binding")
	}
	// Must NOT be the no-match sentinel — that's reserved for provider-event
	// mismatches under --from-ci.
	if errors.Is(err, ErrNoMatchingBinding) {
		t.Errorf("unknown binding leaked the --from-ci sentinel")
	}
}

func TestFromDeclaredTrigger_NilIntent(t *testing.T) {
	t.Parallel()
	if _, err := FromDeclaredTrigger(nil, DeclaredOptions{TriggerName: "foo"}); err == nil {
		t.Fatal("expected error for nil intent")
	}
}

func TestFromDeclaredTrigger_EmptyName(t *testing.T) {
	t.Parallel()
	if _, err := FromDeclaredTrigger(minimalIntent(), DeclaredOptions{}); err == nil {
		t.Fatal("expected error for empty trigger name")
	}
}

func TestResolveProviderEvent_Match(t *testing.T) {
	t.Parallel()
	event := &model.NormalizedEvent{
		Provider: "github", Event: "pull_request", Action: "opened",
		Repository: "sourceplane/orun", Ref: "refs/pull/9/head",
		HeadSHA: "deadbeef1234567", BaseSHA: "cafef00d7654321",
	}
	occ, err := ResolveProviderEvent(minimalIntent(), event, DeclaredOptions{Source: TriggerSource{SourceScope: "pr-9"}})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if occ.TriggerName != "github-pull-request" {
		t.Errorf("TriggerName = %q", occ.TriggerName)
	}
	if occ.Mode != ModeEventFile {
		t.Errorf("Mode = %q want event-file", occ.Mode)
	}
	if occ.Provider != "github" || occ.Event != "pull_request" || occ.Action != "opened" {
		t.Errorf("provider/event/action drift: %+v", occ)
	}
	if occ.Source.HeadRevision != "deadbeef1234567" {
		t.Errorf("Source.HeadRevision = %q want from event", occ.Source.HeadRevision)
	}
	if occ.PlanScope.Mode != PlanScopeChanged {
		t.Errorf("PlanScope.Mode = %q", occ.PlanScope.Mode)
	}
	if len(occ.MatchedBindings) != 1 || occ.MatchedBindings[0] != "github-pull-request" {
		t.Errorf("MatchedBindings = %v", occ.MatchedBindings)
	}
}

func TestResolveProviderEvent_NoMatchReturnsTypedError(t *testing.T) {
	t.Parallel()
	// Specs: design.md §11 / implementation-plan.md M1 done-when — --from-ci
	// no-match returns a typed error the CLI maps to a deterministic exit code.
	event := &model.NormalizedEvent{Provider: "gitlab", Event: "merge_request"}
	_, err := ResolveProviderEvent(minimalIntent(), event, DeclaredOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrNoMatchingBinding) {
		t.Fatalf("expected errors.Is(..., ErrNoMatchingBinding); got %v", err)
	}
	var typed *NoMatchingBindingError
	if !errors.As(err, &typed) {
		t.Fatalf("expected *NoMatchingBindingError; got %T", err)
	}
	if typed.Provider != "gitlab" || typed.Event != "merge_request" {
		t.Errorf("typed error did not carry context: %+v", typed)
	}
}

func TestResolveProviderEvent_NilArgs(t *testing.T) {
	t.Parallel()
	if _, err := ResolveProviderEvent(nil, &model.NormalizedEvent{}, DeclaredOptions{}); err == nil {
		t.Fatal("expected error for nil intent")
	}
	if _, err := ResolveProviderEvent(minimalIntent(), nil, DeclaredOptions{}); err == nil {
		t.Fatal("expected error for nil event")
	}
}

func TestResolveProviderEvent_ConflictingScopes(t *testing.T) {
	t.Parallel()
	intent := &model.Intent{
		Automation: model.AutomationConfig{
			TriggerBindings: map[string]model.TriggerBinding{
				// Two bindings that both match the same event but disagree on scope.
				"a": {On: model.TriggerMatch{Provider: "github", Event: "pull_request"}, Plan: model.TriggerPlanOptions{Scope: "full"}},
				"b": {On: model.TriggerMatch{Provider: "github", Event: "pull_request"}, Plan: model.TriggerPlanOptions{Scope: "changed"}},
			},
		},
	}
	event := &model.NormalizedEvent{Provider: "github", Event: "pull_request"}
	if _, err := ResolveProviderEvent(intent, event, DeclaredOptions{}); err == nil {
		t.Fatal("expected conflict error")
	}
}
