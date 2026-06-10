package codeowners_test

import (
	"reflect"
	"testing"

	"github.com/sourceplane/orun/internal/codeowners"
)

func TestOwners_LastMatchWins(t *testing.T) {
	rs := codeowners.Parse([]byte(`
# default owner
*           @org/platform
/apps/api/  @org/api-team
*.tf        @org/infra @org/sre
`))
	cases := []struct {
		path string
		want []string
	}{
		{"README.md", []string{"@org/platform"}},
		{"apps/api/component.yaml", []string{"@org/api-team"}},  // anchored dir beats the global *
		{"apps/api/main.tf", []string{"@org/infra", "@org/sre"}}, // *.tf is the last matching rule
		{"libs/x/main.go", []string{"@org/platform"}},
	}
	for _, c := range cases {
		got := rs.Owners(c.path)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("Owners(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestOwners_AnchoredVsUnanchored(t *testing.T) {
	rs := codeowners.Parse([]byte(`
docs/          @org/docs
/build/        @org/ci
apps/api       @org/api
`))
	// Unanchored "docs/" matches docs at any depth.
	if got := rs.Owners("services/docs/intro.md"); !reflect.DeepEqual(got, []string{"@org/docs"}) {
		t.Errorf("nested docs = %v", got)
	}
	if got := rs.Owners("docs/intro.md"); !reflect.DeepEqual(got, []string{"@org/docs"}) {
		t.Errorf("root docs = %v", got)
	}
	// Anchored "/build/" only matches at root.
	if got := rs.Owners("build/out.js"); !reflect.DeepEqual(got, []string{"@org/ci"}) {
		t.Errorf("root build = %v", got)
	}
	if got := rs.Owners("nested/build/out.js"); got != nil {
		t.Errorf("nested build should not match anchored rule: %v", got)
	}
	// "apps/api" (slash → anchored) matches the dir and everything under it.
	if got := rs.Owners("apps/api/component.yaml"); !reflect.DeepEqual(got, []string{"@org/api"}) {
		t.Errorf("apps/api file = %v", got)
	}
}

func TestParse_SkipsCommentsBlanksAndOwnerless(t *testing.T) {
	rs := codeowners.Parse([]byte("# comment\n\n   \norphan-pattern-no-owner\n*  @x\n"))
	if rs.Empty() {
		t.Fatal("ruleset should have the one valid rule")
	}
	if got := rs.Owners("anything"); !reflect.DeepEqual(got, []string{"@x"}) {
		t.Errorf("owners = %v", got)
	}
}

func TestOwners_NoMatchAndEmpty(t *testing.T) {
	if got := codeowners.Parse([]byte("/only/here @x")).Owners("elsewhere/f"); got != nil {
		t.Errorf("no match should be nil: %v", got)
	}
	var nilRS *codeowners.Ruleset
	if !nilRS.Empty() {
		t.Error("nil ruleset should be Empty")
	}
	if got := codeowners.Parse(nil).Owners("x"); got != nil {
		t.Errorf("empty ruleset owners = %v", got)
	}
}

func TestOwners_Wildcards(t *testing.T) {
	rs := codeowners.Parse([]byte("*.go @go\napps/*/component.yaml @comp\n"))
	if got := rs.Owners("deep/nested/file.go"); !reflect.DeepEqual(got, []string{"@go"}) {
		t.Errorf("*.go at depth = %v", got)
	}
	if got := rs.Owners("apps/api/component.yaml"); !reflect.DeepEqual(got, []string{"@comp"}) {
		t.Errorf("apps/*/component.yaml = %v", got)
	}
	// `*` does not cross a path separator: apps/api/sub/component.yaml shouldn't
	// match apps/*/component.yaml.
	if got := rs.Owners("apps/api/sub/component.yaml"); reflect.DeepEqual(got, []string{"@comp"}) {
		t.Errorf("apps/*/component.yaml must not cross separators: %v", got)
	}
}
