package catalogresolve

import (
	"context"
	"testing"
)

// TestResolve_LegacyShape verifies that the legacy plan-engine authoring shape
// — spec.domain, spec.subscribe (object and bare-string forms), spec.parameters,
// spec.labels, spec.env, and unknown/legacy keys like spec.inputs — validates
// against the catalog schema and folds into the resolved manifest as expected.
func TestResolve_LegacyShape(t *testing.T) {
	root := fixturePath(t, "legacy_shape")
	rc, issues, err := Resolve(context.Background(), Options{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("Resolve hard error (issues=%v): %v", issues, err)
	}
	for _, is := range issues {
		if is.Severity == SeverityError {
			t.Fatalf("unexpected error-severity issue: %+v", is)
		}
	}

	// --- web: subscribe object form + domain + parameters + labels --------
	web := findByName(rc.Manifests, "web")
	if web == nil {
		t.Fatal("web manifest missing")
	}
	if web.Spec.Domain != "platform-ui" {
		t.Errorf("web Spec.Domain = %q, want platform-ui", web.Spec.Domain)
	}
	// subscribe.environments fold into the resolved environments map using the
	// base profile; profileRules are accepted but not interpreted.
	if got := web.Spec.Environments["staging"]; got.Profile != "verify" || !got.Active {
		t.Errorf("web staging env = %+v, want {verify true}", got)
	}
	if got := web.Spec.Environments["production"]; got.Profile != "release" || !got.Active {
		t.Errorf("web production env = %+v, want {release true}", got)
	}
	// parameters mapped (incl. a non-string scalar stringified deterministically).
	wantParams := map[string]string{
		"nodeVersion":  "20",
		"buildCommand": "pnpm build",
		"replicas":     "3",
	}
	for k, want := range wantParams {
		if got := web.Spec.Parameters[k]; got != want {
			t.Errorf("web param[%q] = %q, want %q", k, got, want)
		}
	}
	// spec.labels surface in resolved metadata.labels.
	if web.Metadata.Labels["team"] != "platform" || web.Metadata.Labels["surface"] != "web" {
		t.Errorf("web labels = %v, want team=platform surface=web", web.Metadata.Labels)
	}

	// Provenance is recorded for every new authored field (asserted at the
	// load layer, where authored provenance lives).
	am, err := loadAuthored(root, "apps/web/component.yaml")
	if err != nil {
		t.Fatalf("loadAuthored(web): %v", err)
	}
	for _, field := range []string{
		"spec.domain",
		"spec.parameters.nodeVersion",
		"spec.subscribe.environments.staging",
		"spec.env.BASE_DOMAIN",
		"metadata.labels.surface",
	} {
		if _, ok := am.Provenance[field]; !ok {
			t.Errorf("provenance missing for %q", field)
		}
	}

	// --- stack: bare-string shorthand + legacy inputs + label merge -------
	stack := findByName(rc.Manifests, "stack")
	if stack == nil {
		t.Fatal("stack manifest missing")
	}
	if stack.Spec.Domain != "infra" {
		t.Errorf("stack Spec.Domain = %q, want infra", stack.Spec.Domain)
	}
	// bare-string subscribe entries become envs with no explicit profile.
	if got, ok := stack.Spec.Environments["dev"]; !ok || got.Profile != "" || !got.Active {
		t.Errorf("stack dev env = %+v (present=%v), want {<empty> true}", got, ok)
	}
	if _, ok := stack.Spec.Environments["production"]; !ok {
		t.Errorf("stack production env missing from shorthand subscribe")
	}
	// legacy spec.inputs is accepted (open schema) but not mapped to parameters.
	if len(stack.Spec.Parameters) != 0 {
		t.Errorf("stack Spec.Parameters = %v, want empty (spec.inputs is ignored)", stack.Spec.Parameters)
	}
	// metadata.labels wins over spec.labels on key conflict; spec-only keys merge in.
	if stack.Metadata.Labels["surface"] != "metadata-wins" {
		t.Errorf("stack label surface = %q, want metadata-wins (metadata beats spec)", stack.Metadata.Labels["surface"])
	}
	if stack.Metadata.Labels["scope"] != "database" || stack.Metadata.Labels["provider"] != "orun" {
		t.Errorf("stack labels merge = %v, want scope=database provider=orun", stack.Metadata.Labels)
	}
}
