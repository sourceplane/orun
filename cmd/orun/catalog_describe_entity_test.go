package main

// WO3.1b — kind-aware `catalog describe`. Unit tests for the selector routing
// (parseEntitySelector) and the entity resolver (selectObjEntity); the render
// paths are exercised end to end against a real catalog in catalog_read_test.go.

import (
	"testing"

	"github.com/sourceplane/orun/internal/objcatalog"
)

func TestParseEntitySelector(t *testing.T) {
	cases := []struct {
		name     string
		arg      string
		kindFlag string
		kind     string
		key      string
		isEntity bool
	}{
		{"kind-prefix", "repo:sourceplane/ogpic/ogpic", "", "Repo", "sourceplane/ogpic/ogpic", true},
		{"kind-prefix-api-caps", "api:sourceplane/ogpic/edge-api", "", "API", "sourceplane/ogpic/edge-api", true},
		{"kind-flag", "ogpic", "Repo", "Repo", "ogpic", true},
		{"kind-flag-caseless", "payments", "system", "System", "payments", true},
		{"bare-kind-keyword", "repo", "", "Repo", "", true},
		{"owner-alias-normalizes", "owner:@org/team", "", "Group", "@org/team", true},
		// Component (default) paths — isEntity false.
		{"bare-component", "api-edge", "", "", "api-edge", false},
		{"qualified-component", "sourceplane/ogpic/api-edge", "", "", "sourceplane/ogpic/api-edge", false},
		{"kind-flag-component", "api-edge", "Component", "", "api-edge", false},
		{"unknown-prefix-is-component", "http://x", "", "", "http://x", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kind, key, isEntity := parseEntitySelector(c.arg, c.kindFlag)
			if kind != c.kind || key != c.key || isEntity != c.isEntity {
				t.Errorf("parseEntitySelector(%q,%q) = (%q,%q,%v), want (%q,%q,%v)",
					c.arg, c.kindFlag, kind, key, isEntity, c.kind, c.key, c.isEntity)
			}
		})
	}
}

func entityFixture() objcatalog.CatalogView {
	return objcatalog.CatalogView{
		Entities: []objcatalog.EntityView{
			{Kind: "Repo", EntityKey: "sourceplane/ogpic/ogpic", Name: "ogpic", DisplayName: "Ogpic",
				Owner: "platform", Docs: map[string]any{"overview": map[string]any{"path": "docs/overview.md", "digest": "sha256:7b0b"}}},
			{Kind: "System", EntityKey: "ns/repo/payments", Name: "payments"},
			{Kind: "System", EntityKey: "ns/other/payments", Name: "payments"},
		},
	}
}

func exitCodeOf(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	coder, ok := err.(interface{ ExitCode() int })
	if !ok {
		t.Fatalf("error has no ExitCode(): %v", err)
	}
	return coder.ExitCode()
}

func TestSelectObjEntity_Singleton(t *testing.T) {
	e, err := selectObjEntity(entityFixture(), "Repo", "")
	if err != nil {
		t.Fatalf("singleton Repo: %v", err)
	}
	if e.EntityKey != "sourceplane/ogpic/ogpic" || e.DisplayName != "Ogpic" {
		t.Errorf("got %+v", e)
	}
}

func TestSelectObjEntity_ByKey(t *testing.T) {
	e, err := selectObjEntity(entityFixture(), "System", "ns/repo/payments")
	if err != nil || e.EntityKey != "ns/repo/payments" {
		t.Fatalf("by key = %+v, %v", e, err)
	}
}

func TestSelectObjEntity_AmbiguousSingletonExit4(t *testing.T) {
	// Two Systems, no key → ambiguous.
	_, err := selectObjEntity(entityFixture(), "System", "")
	if got := exitCodeOf(t, err); got != 4 {
		t.Errorf("exit = %d, want 4", got)
	}
}

func TestSelectObjEntity_AmbiguousNameExit4(t *testing.T) {
	// Bare name "payments" matches two Systems across repos.
	_, err := selectObjEntity(entityFixture(), "System", "payments")
	if got := exitCodeOf(t, err); got != 4 {
		t.Errorf("exit = %d, want 4", got)
	}
}

func TestSelectObjEntity_AbsentKindExit6(t *testing.T) {
	_, err := selectObjEntity(entityFixture(), "Domain", "")
	if got := exitCodeOf(t, err); got != 6 {
		t.Errorf("exit = %d, want 6", got)
	}
}

func TestSelectObjEntity_AbsentKeyExit6(t *testing.T) {
	_, err := selectObjEntity(entityFixture(), "Repo", "no/such/key")
	if got := exitCodeOf(t, err); got != 6 {
		t.Errorf("exit = %d, want 6", got)
	}
}
