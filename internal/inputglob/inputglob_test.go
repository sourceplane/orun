package inputglob

import "testing"

func TestValidate_Accepts(t *testing.T) {
	for _, p := range []string{
		"pnpm-lock.yaml",
		"turbo.json",
		"tooling/eslint/**",
		"**/*.proto",
		"packages/*/package.json",
		"config/{a,b}.yaml",
		"docs/[a-z]*.md",
	} {
		if err := Validate(p); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", p, err)
		}
	}
}

func TestValidate_Rejects(t *testing.T) {
	for _, p := range []string{
		"",
		"   ",
		" turbo.json",
		"/etc/passwd",
		"C:/repo/x",
		`tooling\eslint\**`,
		"../other-repo/**",
		"tooling/../secrets",
		"./turbo.json",
		"tooling//x",
		"tooling/",
		"config/[a-z.yaml",
		"config/{a,b.yaml",
	} {
		if err := Validate(p); err == nil {
			t.Errorf("Validate(%q) = nil, want an error", p)
		}
	}
}

func TestValidateAll_ReportsIndex(t *testing.T) {
	err := ValidateAll([]string{"ok.json", "../bad"})
	if err == nil || err.Error()[:9] != "inputs[1]" {
		t.Fatalf("ValidateAll error = %v, want inputs[1] prefix", err)
	}
	if err := ValidateAll(nil); err != nil {
		t.Fatalf("ValidateAll(nil) = %v", err)
	}
}

func TestMatch(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"pnpm-lock.yaml", "pnpm-lock.yaml", true},
		{"pnpm-lock.yaml", "apps/web/pnpm-lock.yaml", false},
		{"tooling/eslint/**", "tooling/eslint/index.js", true},
		{"tooling/eslint/**", "tooling/eslint/rules/deep/no-x.js", true},
		{"tooling/eslint/**", "tooling/prettier/index.js", false},
		{"**/*.proto", "api/v1/svc.proto", true},
		{"**/*.proto", "svc.proto", true},
		{"*.json", "packages/x/package.json", false},
		{"turbo.json", "./turbo.json", true},
		{"tooling/**", `tooling\a\b.js`, true},
		{"[bad", "[bad", false},
		{"turbo.json", "", false},
	}
	for _, c := range cases {
		if got := Match(c.pattern, c.path); got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestMatchAny(t *testing.T) {
	p, ok := MatchAny([]string{"turbo.json", "tooling/**"}, "tooling/tsconfig/base.json")
	if !ok || p != "tooling/**" {
		t.Fatalf("MatchAny = %q, %v", p, ok)
	}
	if _, ok := MatchAny(nil, "turbo.json"); ok {
		t.Fatal("MatchAny(nil) matched")
	}
}
