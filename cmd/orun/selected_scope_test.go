package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/triggerctx"
)

func TestAutoPushAfterPlan(t *testing.T) {
	prevFn, prevFlag := autoPushCatalog, planAutoPushDisabled
	t.Cleanup(func() { autoPushCatalog, planAutoPushDisabled = prevFn, prevFlag })
	calls := 0
	autoPushCatalog = func(context.Context) { calls++ }

	planAutoPushDisabled = true
	autoPushAfterPlan(context.Background())
	if calls != 0 {
		t.Fatalf("plan.autopush: false must skip the push, got %d calls", calls)
	}
	planAutoPushDisabled = false
	autoPushAfterPlan(context.Background())
	if calls != 1 {
		t.Fatalf("unset autopush keeps the push, got %d calls", calls)
	}
}

func TestPlanScopeMode_SelectedRecordsFull(t *testing.T) {
	for mode, want := range map[string]string{"": "full", "full": "full", "changed": "changed", model.PlanScopeSelected: "full"} {
		trig := triggerctx.TriggerOccurrence{PlanScope: triggerctx.PlanScope{Mode: mode}}
		if got := planScopeMode(trig); got != want {
			t.Errorf("planScopeMode(%q) = %q, want %q (the revision scope accepts full | changed)", mode, got, want)
		}
	}
}

func inst(name string) *model.ComponentInstance { return &model.ComponentInstance{ComponentName: name} }

func TestSelectInstances(t *testing.T) {
	expanded := map[string][]*model.ComponentInstance{
		"dev":   {inst("svc-a"), inst("svc-b"), inst("svc-c")},
		"stage": {inst("svc-a"), inst("svc-b")},
	}
	got, err := selectInstances(expanded, []string{"svc-b", "svc-a"}, []string{"stage"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || len(got["stage"]) != 2 {
		t.Fatalf("got %v, want exactly stage: svc-b, svc-a", got)
	}

	for name, tc := range map[string]struct {
		comps, envs []string
		want        string
	}{
		"wildcard":          {[]string{"svc-*"}, []string{"dev"}, "not exact"},
		"unknown":           {[]string{"svc-a", "nope"}, []string{"dev"}, "unknown component nope"},
		"not subscribed":    {[]string{"svc-c"}, []string{"stage"}, "svc-c does not subscribe to stage"},
		"one of two misses": {[]string{"svc-a", "svc-c"}, []string{"stage"}, "svc-c does not subscribe to stage"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := selectInstances(expanded, tc.comps, tc.envs); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestSelectedComponentsFor(t *testing.T) {
	ev := &model.TriggerResolution{MatchedTriggerNames: []string{"github-dispatch"}, SelectedComponents: []string{"a", "b"}}
	if got, _ := selectedComponentsFor(ev, nil); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("event alone: %v", got)
	}
	if got, _ := selectedComponentsFor(ev, []string{"b", "z"}); !reflect.DeepEqual(got, []string{"b"}) {
		t.Errorf("--component narrows the event's selection: %v", got)
	}
	if _, err := selectedComponentsFor(ev, []string{"z"}); err == nil {
		t.Errorf("--component that selects none of the event's components should fail")
	}
	named := &model.TriggerResolution{MatchedTriggerNames: []string{"github-dispatch"}}
	if got, _ := selectedComponentsFor(named, []string{"a"}); !reflect.DeepEqual(got, []string{"a"}) {
		t.Errorf("named trigger takes --component: %v", got)
	}
	if _, err := selectedComponentsFor(named, nil); err == nil || !strings.Contains(err.Error(), "--component") {
		t.Errorf("no selection at all should fail naming --component, got %v", err)
	}
}

// seedDispatchWorkspace writes an intent with a push binding and a selected
// workflow_dispatch binding over dev and stage, and three components: svc-a
// and svc-b in both, svc-c in dev only.
func seedDispatchWorkspace(t *testing.T, dir string) {
	t.Helper()
	seedCatalogRunWorkspace(t, dir) // compositions, git repo, catalog dir
	mustWriteFile(t, filepath.Join(dir, "intent.yaml"), `apiVersion: orun.io/v1alpha1
kind: Intent
metadata:
  name: dispatch-demo
discovery:
  roots:
    - runtime
compositions:
  sources:
    - name: core
      kind: dir
      path: ./core
automation:
  triggerBindings:
    github-push-main:
      on: { provider: github, event: push, branches: [main] }
    github-dispatch:
      on: { provider: github, event: workflow_dispatch, branches: [main] }
      plan:
        scope: selected
        components: inputs.component
        environments: inputs.environment
        autopush: false
environments:
  dev:
    activation: { triggerRefs: [github-push-main, github-dispatch] }
  stage:
    activation: { triggerRefs: [github-push-main, github-dispatch] }
components:
  - name: svc-a
    type: noop
    path: .
    subscribe: { environments: [{ name: dev }, { name: stage }] }
    parameters: {}
  - name: svc-b
    type: noop
    path: .
    subscribe: { environments: [{ name: dev }, { name: stage }] }
    parameters: {}
  - name: svc-c
    type: noop
    path: .
    subscribe: { environments: [{ name: dev }] }
    parameters: {}
`)
}

// TestSelectedScopeE2E_DispatchEventPlansTheSelection plans a workflow_dispatch
// event with the default CI flags (--from-ci github --event-file … --changed):
// the plan is exactly the event's components in its environment, --changed
// notwithstanding, and every refusal names its cause.
func TestSelectedScopeE2E_DispatchEventPlansTheSelection(t *testing.T) {
	dir := withTempIntentRoot(t)
	seedDispatchWorkspace(t, dir)
	resetCatalogFlags(t)
	resetCatalogRunE2EGlobals(t)
	prevTrigger, prevFromCI, prevEvent := triggerName, fromCI, eventFile
	t.Cleanup(func() { triggerName, fromCI, eventFile = prevTrigger, prevFromCI, prevEvent })

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	intentFile = filepath.Join(dir, "intent.yaml")
	intentRoot = dir
	outputFormat = "json"
	t.Setenv("GITHUB_EVENT_NAME", "workflow_dispatch")

	plan := func(t *testing.T, inputs map[string]any) (*model.Plan, error) {
		t.Helper()
		ev, _ := json.Marshal(map[string]any{"ref": "refs/heads/main", "inputs": inputs})
		evPath := filepath.Join(dir, "event.json")
		mustWriteFile(t, evPath, string(ev))
		planPath := filepath.Join(dir, "plan.json")
		_ = os.Remove(planPath)
		triggerName, fromCI, eventFile = "", "github", evPath
		environment, allEnvs, planComponents, changedOnly = "", false, nil, true
		changedFiles = []string{"README.md"}
		outputFile = planPath
		var genErr error
		_ = captureStdout(t, func() error { genErr = generatePlan(); return nil })
		if genErr != nil {
			return nil, genErr
		}
		raw, err := os.ReadFile(planPath)
		if err != nil {
			t.Fatalf("read plan: %v", err)
		}
		var p model.Plan
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatalf("decode plan: %v", err)
		}
		return &p, nil
	}
	jobIDs := func(p *model.Plan) []string {
		var ids []string
		for _, j := range p.Jobs {
			ids = append(ids, j.ID)
		}
		sort.Strings(ids)
		return ids
	}

	t.Run("the selection, and nothing else", func(t *testing.T) {
		p, err := plan(t, map[string]any{"component": "svc-b, svc-a", "environment": "stage", "deploy_id": "dpl_1"})
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		ids := jobIDs(p)
		if len(ids) != 2 || !strings.HasPrefix(ids[0], "svc-a.stage.") || !strings.HasPrefix(ids[1], "svc-b.stage.") {
			t.Fatalf("jobs = %v, want svc-a and svc-b in stage only", ids)
		}
		if tr := p.Metadata.Trigger; tr == nil || tr.Scope != model.PlanScopeSelected || !reflect.DeepEqual(tr.MatchedBindings, []string{"github-dispatch"}) {
			t.Errorf("trigger metadata = %+v, want scope selected by github-dispatch", p.Metadata.Trigger)
		}
		if !planAutoPushDisabled {
			t.Errorf("plan.autopush: false should disable the catalog push")
		}
	})

	t.Run("a dev-only component in dev", func(t *testing.T) {
		p, err := plan(t, map[string]any{"component": "svc-c", "environment": "dev"})
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		if ids := jobIDs(p); len(ids) != 1 || !strings.HasPrefix(ids[0], "svc-c.dev.") {
			t.Fatalf("jobs = %v, want svc-c in dev", ids)
		}
	})

	for name, tc := range map[string]struct {
		inputs map[string]any
		want   string
	}{
		"empty component":    {map[string]any{"component": "", "environment": "stage"}, "plan.components"},
		"unknown component":  {map[string]any{"component": "svc-a,nope", "environment": "stage"}, "unknown component nope"},
		"not subscribed":     {map[string]any{"component": "svc-c", "environment": "stage"}, "svc-c does not subscribe to stage"},
		"env not activated":  {map[string]any{"component": "svc-a", "environment": "prod"}, "environment prod is not activated"},
		"wildcard component": {map[string]any{"component": "svc-*", "environment": "dev"}, "not exact"},
	} {
		t.Run("refuses: "+name, func(t *testing.T) {
			if _, err := plan(t, tc.inputs); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestRequireRuleFilter(t *testing.T) {
	a := &model.ComponentInstance{ComponentName: "svc-a", ProfileRuleTriggerRef: "github-dispatch"}
	b := &model.ComponentInstance{ComponentName: "svc-b"}
	c := &model.ComponentInstance{ComponentName: "svc-c", ProfileRuleTriggerRef: "github-push-main"}
	in := map[string][]*model.ComponentInstance{"stage": {a, b, c}}

	if got, err := requireRuleFilter(in, nil, true); err != nil || len(got["stage"]) != 3 {
		t.Fatalf("no requireRule binding: unchanged, got %v %v", got, err)
	}
	got, err := requireRuleFilter(in, []string{"github-dispatch"}, false)
	if err != nil || len(got["stage"]) != 1 || got["stage"][0] != a {
		t.Fatalf("non-strict keeps only the opted-in lane, got %v %v", got, err)
	}
	if _, err := requireRuleFilter(in, []string{"github-dispatch"}, true); err == nil ||
		!strings.Contains(err.Error(), "not enabled for github-dispatch: svc-b.stage, svc-c.stage") ||
		!strings.Contains(err.Error(), "when.triggerRef: github-dispatch") {
		t.Fatalf("strict names every lane that did not opt in and the fix, got %v", err)
	}
	none := map[string][]*model.ComponentInstance{"stage": {b}}
	if _, err := requireRuleFilter(none, []string{"github-dispatch"}, false); err == nil || !strings.Contains(err.Error(), "no lane is enabled") {
		t.Fatalf("nothing left fails, got %v", err)
	}
}

// TestRequireRuleE2E plans dispatch and push events against an intent whose
// dispatch binding sets requireRule: svc-a opts dev (verify) and stage
// (deploy) in, svc-b opts nothing in.
func TestRequireRuleE2E(t *testing.T) {
	dir := withTempIntentRoot(t)
	seedCatalogRunWorkspace(t, dir)
	mustWriteFile(t, filepath.Join(dir, "intent.yaml"), `apiVersion: orun.io/v1alpha1
kind: Intent
metadata:
  name: require-rule-demo
discovery:
  roots:
    - runtime
compositions:
  sources:
    - name: core
      kind: dir
      path: ./core
automation:
  triggerBindings:
    github-push-main:
      on: { provider: github, event: push, branches: [main] }
    github-dispatch:
      on: { provider: github, event: workflow_dispatch, branches: [main] }
      plan:
        scope: selected
        components: inputs.component
        environments: inputs.environment
        autopush: false
        requireRule: true
environments:
  dev:
    activation: { triggerRefs: [github-push-main, github-dispatch] }
  stage:
    activation: { triggerRefs: [github-push-main, github-dispatch] }
components:
  - name: svc-a
    type: noop
    path: .
    subscribe:
      environments:
        - name: dev
          profile: verify
          profileRules:
            - profile: verify
              when: { triggerRef: github-dispatch }
        - name: stage
          profile: verify
          profileRules:
            - profile: deploy
              when: { triggerRef: github-push-main }
            - profile: deploy
              when: { triggerRef: github-dispatch }
    parameters: {}
  - name: svc-b
    type: noop
    path: .
    subscribe: { environments: [{ name: dev }, { name: stage }] }
    parameters: {}
`)
	resetCatalogFlags(t)
	resetCatalogRunE2EGlobals(t)
	prevTrigger, prevFromCI, prevEvent := triggerName, fromCI, eventFile
	t.Cleanup(func() { triggerName, fromCI, eventFile = prevTrigger, prevFromCI, prevEvent })
	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	intentFile = filepath.Join(dir, "intent.yaml")
	intentRoot = dir
	outputFormat = "json"

	plan := func(t *testing.T, event string, payload map[string]any) (*model.Plan, error) {
		t.Helper()
		t.Setenv("GITHUB_EVENT_NAME", event)
		raw, _ := json.Marshal(payload)
		evPath := filepath.Join(dir, "event.json")
		mustWriteFile(t, evPath, string(raw))
		planPath := filepath.Join(dir, "plan.json")
		_ = os.Remove(planPath)
		triggerName, fromCI, eventFile = "", "github", evPath
		environment, allEnvs, planComponents, changedOnly, changedFiles = "", false, nil, false, nil
		outputFile = planPath
		var genErr error
		_ = captureStdout(t, func() error { genErr = generatePlan(); return nil })
		if genErr != nil {
			return nil, genErr
		}
		body, err := os.ReadFile(planPath)
		if err != nil {
			t.Fatalf("read plan: %v", err)
		}
		var p model.Plan
		if err := json.Unmarshal(body, &p); err != nil {
			t.Fatalf("decode plan: %v", err)
		}
		return &p, nil
	}
	dispatch := func(component, env string) map[string]any {
		return map[string]any{"ref": "refs/heads/main", "inputs": map[string]any{"component": component, "environment": env}}
	}

	t.Run("an opted-in lane runs its rule's profile", func(t *testing.T) {
		p, err := plan(t, "workflow_dispatch", dispatch("svc-a", "stage"))
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		if len(p.Jobs) != 1 || !strings.HasPrefix(p.Jobs[0].ID, "svc-a.stage.") || p.Jobs[0].ProfileRuleTriggerRef != "github-dispatch" {
			t.Fatalf("jobs = %+v, want svc-a.stage chosen by the github-dispatch rule", p.Jobs)
		}
	})
	t.Run("a dev check opted in", func(t *testing.T) {
		p, err := plan(t, "workflow_dispatch", dispatch("svc-a", "dev"))
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		if len(p.Jobs) != 1 || !strings.HasPrefix(p.Jobs[0].ID, "svc-a.dev.") {
			t.Fatalf("jobs = %+v, want svc-a.dev", p.Jobs)
		}
	})
	t.Run("a lane that did not opt in is refused", func(t *testing.T) {
		if _, err := plan(t, "workflow_dispatch", dispatch("svc-b", "dev")); err == nil || !strings.Contains(err.Error(), "not enabled for github-dispatch: svc-b.dev") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("one refused lane fails the whole selection", func(t *testing.T) {
		if _, err := plan(t, "workflow_dispatch", dispatch("svc-a,svc-b", "stage")); err == nil || !strings.Contains(err.Error(), "svc-b.stage") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("a push plans every lane, as before", func(t *testing.T) {
		p, err := plan(t, "push", map[string]any{"ref": "refs/heads/main", "before": "", "after": ""})
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		if len(p.Jobs) != 4 {
			t.Fatalf("push planned %d jobs, want all 4 lanes (requireRule belongs to the dispatch binding)", len(p.Jobs))
		}
	})
}
