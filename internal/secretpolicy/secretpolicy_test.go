package secretpolicy

import (
	"strings"
	"testing"
	"testing/fstest"
)

func parseDoc(t *testing.T, body string, tier Tier, source string) *Document {
	t.Helper()
	doc, err := ParseDocument([]byte(body), tier, source, "test.yaml")
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	return doc
}

const goodDoc = `
apiVersion: orun.io/v1
kind: SecretPolicy
metadata: { name: prod-secrets }
spec:
  rules:
    - id: admins-prod-from-ci
      effect: allow
      subjects: ["team:platform-admins", "workflow"]
      scope: { env: prod, key: "*" }
      when:
        - platform in ["ci-oidc", "service"]
    - id: billing-stripe-main
      effect: allow
      subjects: ["*authenticated"]
      scope: { env: prod, key: "STRIPE_*" }
      when:
        - component.type == "billing-worker"
        - trigger.declared
        - trigger.branch == "main"
    - id: laptops-never-prod
      effect: deny
      subjects: ["*authenticated"]
      scope: { env: prod, key: "*" }
      when:
        - platform == "local-cli"
`

func TestParseGood(t *testing.T) {
	doc := parseDoc(t, goodDoc, TierStack, "stack:acme@1.0.0")
	if doc.Name != "prod-secrets" {
		t.Fatalf("name = %q", doc.Name)
	}
	if len(doc.Rules) != 3 {
		t.Fatalf("rules = %d, want 3", len(doc.Rules))
	}
	r := doc.Rules[1]
	if r.ID != "billing-stripe-main" || r.Effect != EffectAllow {
		t.Errorf("rule[1] = %+v", r)
	}
	if r.Scope.Env != "prod" || r.Scope.Key != "STRIPE_*" {
		t.Errorf("scope = %+v", r.Scope)
	}
	if len(r.When) != 3 {
		t.Fatalf("when = %d, want 3", len(r.When))
	}
	// A platform predicate round-trips its authored text.
	if got := doc.Rules[0].When[0].String(); got != `platform in ["ci-oidc", "service"]` {
		t.Errorf("platform predicate text = %q", got)
	}
	if doc.Rules[0].When[0].Kind != PredPlatform {
		t.Errorf("kind = %v, want platform", doc.Rules[0].When[0].Kind)
	}
}

func TestParseIDSynthesis(t *testing.T) {
	doc := parseDoc(t, `
apiVersion: orun.io/v1
kind: SecretPolicy
metadata: { name: noid }
spec:
  rules:
    - effect: allow
      scope: { env: "*", key: "*" }
`, TierIntent, "intent")
	if doc.Rules[0].ID != "noid-rule-0" {
		t.Errorf("synthesized id = %q, want noid-rule-0", doc.Rules[0].ID)
	}
}

func TestParseRejections(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"unknown predicate", ruleWith(`when: ["trigger.branch ~ main"]`), "not in the locked vocabulary"},
		{"unknown field", ruleWith(`when: ['subject.email == "x@y"']`), "unknown field"},
		{"bad effect", `
apiVersion: orun.io/v1
kind: SecretPolicy
metadata: { name: d }
spec:
  rules:
    - id: r
      effect: permit
      scope: { env: "*", key: "*" }
`, "effect must be"},
		{"unknown platform", ruleWith(`when: ['platform == "laptop"']`), "unknown platform"},
		{"unknown subject", `
apiVersion: orun.io/v1
kind: SecretPolicy
metadata: { name: d }
spec:
  rules:
    - id: r
      effect: allow
      subjects: ["group:admins"]
      scope: { env: "*", key: "*" }
`, "unknown subject"},
		{"bad apiVersion", `
apiVersion: v2
kind: SecretPolicy
metadata: { name: d }
spec: { rules: [] }
`, "apiVersion must be"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseDocument([]byte(c.body), TierStack, "stack", "d.yaml")
			if err == nil {
				t.Fatalf("expected error containing %q", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %q, want substring %q", err.Error(), c.want)
			}
		})
	}
}

func ruleWith(whenLine string) string {
	return `
apiVersion: orun.io/v1
kind: SecretPolicy
metadata: { name: d }
spec:
  rules:
    - id: r
      effect: allow
      scope: { env: "*", key: "*" }
      ` + whenLine + "\n"
}

func TestCompositionTypeInjection(t *testing.T) {
	doc := parseDoc(t, `
apiVersion: orun.io/v1
kind: SecretPolicy
metadata: { name: terraform-defaults }
spec:
  rules:
    - id: r1
      effect: allow
      subjects: ["workflow"]
      scope: { env: "*", key: "AWS_ROLE_ARN" }
      when:
        - trigger.declared
`, TierComposition, "composition:terraform")
	if err := doc.InjectComponentType("terraform"); err != nil {
		t.Fatalf("InjectComponentType: %v", err)
	}
	found := false
	for _, p := range doc.Rules[0].When {
		if p.Kind == PredEquals && p.Fact == "component.type" && p.Value == "terraform" {
			found = true
		}
	}
	if !found {
		t.Errorf("component.type == terraform not injected: %+v", doc.Rules[0].When)
	}
}

func TestCompositionForeignTypeRejected(t *testing.T) {
	doc := parseDoc(t, `
apiVersion: orun.io/v1
kind: SecretPolicy
metadata: { name: bad }
spec:
  rules:
    - id: r1
      effect: allow
      scope: { env: "*", key: "*" }
      when:
        - component.type == "other-worker"
`, TierComposition, "composition:terraform")
	err := doc.InjectComponentType("terraform")
	if err == nil || !strings.Contains(err.Error(), "foreign component.type") {
		t.Fatalf("expected foreign component.type rejection, got %v", err)
	}
}

func TestThreeTierDiscovery(t *testing.T) {
	stackFS := fstest.MapFS{
		"stack.yaml": &fstest.MapFile{Data: []byte(`
apiVersion: orun.io/v1
kind: Stack
metadata: { name: acme-platform, version: 1.4.0 }
`)},
		"compositions/terraform/secret-policy.yaml": &fstest.MapFile{Data: []byte(`
apiVersion: orun.io/v1
kind: SecretPolicy
metadata: { name: terraform-defaults }
spec:
  rules:
    - id: r1
      effect: allow
      subjects: ["workflow"]
      scope: { env: "*", key: "AWS_ROLE_ARN" }
      when: [ trigger.declared ]
`)},
		"policies/prod.SecretPolicy.yaml": &fstest.MapFile{Data: []byte(goodDoc)},
	}
	comp, stack, err := LoadStackRoot(stackFS)
	if err != nil {
		t.Fatalf("LoadStackRoot: %v", err)
	}
	if len(comp) != 1 || comp[0].Source != "composition:terraform" || comp[0].Tier != TierComposition {
		t.Fatalf("composition tier = %+v", comp)
	}
	// component.type injected at load
	injected := false
	for _, p := range comp[0].Rules[0].When {
		if p.Fact == "component.type" && p.Value == "terraform" {
			injected = true
		}
	}
	if !injected {
		t.Error("composition load did not inject component.type")
	}
	if len(stack) != 1 || stack[0].Source != "stack:acme-platform@1.4.0" {
		t.Fatalf("stack tier = %+v", stack)
	}

	intentFS := fstest.MapFS{
		"policies/overlay.SecretPolicy.yaml": &fstest.MapFile{Data: []byte(`
apiVersion: orun.io/v1
kind: SecretPolicy
metadata: { name: overlay }
spec:
  rules:
    - id: deny-laptop
      effect: deny
      scope: { env: prod, key: "*" }
      when: [ 'platform == "local-cli"' ]
`)},
	}
	intentDocs, err := LoadIntentPolicies(intentFS)
	if err != nil {
		t.Fatalf("LoadIntentPolicies: %v", err)
	}
	if len(intentDocs) != 1 || intentDocs[0].Source != "intent" || intentDocs[0].Tier != TierIntent {
		t.Fatalf("intent tier = %+v", intentDocs)
	}
}

func TestNarrowOnly(t *testing.T) {
	stack := parseDoc(t, `
apiVersion: orun.io/v1
kind: SecretPolicy
metadata: { name: stack }
spec:
  rules:
    - id: stripe-allow
      effect: allow
      subjects: ["*authenticated"]
      scope: { env: prod, key: "STRIPE_*" }
      when: [ 'component.type == "billing-worker"' ]
`, TierStack, "stack:acme@1.0.0")

	within := parseDoc(t, `
apiVersion: orun.io/v1
kind: SecretPolicy
metadata: { name: within }
spec:
  rules:
    - id: stripe-key-narrow
      effect: allow
      subjects: ["*authenticated"]
      scope: { env: prod, key: "STRIPE_KEY" }
      when:
        - component.type == "billing-worker"
        - trigger.declared
`, TierIntent, "intent")

	broader := parseDoc(t, `
apiVersion: orun.io/v1
kind: SecretPolicy
metadata: { name: broader }
spec:
  rules:
    - id: all-keys
      effect: allow
      subjects: ["*authenticated"]
      scope: { env: prod, key: "*" }
`, TierIntent, "intent")

	denyBroad := parseDoc(t, `
apiVersion: orun.io/v1
kind: SecretPolicy
metadata: { name: deny }
spec:
  rules:
    - id: deny-all
      effect: deny
      scope: { env: prod, key: "*" }
`, TierIntent, "intent")

	// intent allow within stack allow → accepted
	if f := Lint(Tiers{Stack: []Document{*stack}, Intent: []Document{*within}}); len(f) != 0 {
		t.Errorf("within-stack overlay should pass, got findings: %+v", f)
	}
	// intent allow broader than stack → rejected
	f := Lint(Tiers{Stack: []Document{*stack}, Intent: []Document{*broader}})
	if len(f) != 1 || f[0].RuleID != "all-keys" || f[0].Kind != "narrow-only" {
		t.Errorf("broader overlay should be flagged once, got: %+v", f)
	}
	if err := Validate(Tiers{Stack: []Document{*stack}, Intent: []Document{*broader}}); err == nil {
		t.Error("Validate should reject a broader intent allow")
	}
	// intent deny is always accepted
	if f := Lint(Tiers{Stack: []Document{*stack}, Intent: []Document{*denyBroad}}); len(f) != 0 {
		t.Errorf("intent deny should always pass, got: %+v", f)
	}
}

func TestGlobCovers(t *testing.T) {
	cases := []struct {
		broad, narrow string
		want          bool
	}{
		{"*", "STRIPE_KEY", true},
		{"STRIPE_*", "STRIPE_KEY", true},
		{"STRIPE_*", "*", false},
		{"STRIPE_*", "STRIPE_KEY_*", true},
		{"prod", "prod", true},
		{"prod", "staging", false},
		{"*_KEY", "APP_*_KEY", true},
	}
	for _, c := range cases {
		if got := globCovers(c.broad, c.narrow); got != c.want {
			t.Errorf("globCovers(%q,%q) = %v, want %v", c.broad, c.narrow, got, c.want)
		}
	}
}

func TestLintFindingsVocabularyLenient(t *testing.T) {
	stackFS := fstest.MapFS{
		"policies/bad.SecretPolicy.yaml": &fstest.MapFile{Data: []byte(ruleWith(`when: ["trigger.branch ~ main"]`))},
	}
	_, _, findings := LoadStackRootLenient(stackFS)
	if len(findings) != 1 || findings[0].Severity != SevError || findings[0].Kind != "vocabulary" {
		t.Fatalf("expected one vocabulary finding, got: %+v", findings)
	}
}
