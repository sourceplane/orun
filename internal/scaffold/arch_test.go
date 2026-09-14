package scaffold

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// renderCoreFiles are the files that make up the sandboxed template engine. The
// structural import denylist (design §7.2) binds exactly these: the render path
// MUST be reachable to no filesystem/exec/net/clock/rng primitive. Placement
// and source resolution (place.go, source.go) legitimately touch the
// filesystem through a contained, audited path and are excluded.
var renderCoreFiles = []string{"engine.go", "funcmap.go"}

// bannedRenderImports is the structural denylist for the render core.
var bannedRenderImports = []string{
	"os", "os/exec", "exec",
	"net", "net/http",
	"io", "io/ioutil",
	"time",
	"math/rand", "crypto/rand",
}

func TestRenderCoreHasNoBannedImports(t *testing.T) {
	fset := token.NewFileSet()
	for _, name := range renderCoreFiles {
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			for _, banned := range bannedRenderImports {
				if path == banned {
					t.Errorf("%s imports banned package %q — the render engine must stay pure (design §7.2)", name, path)
				}
			}
		}
	}
}

// ecosystemLiterals are the framework-specific strings that must never appear
// in internal/scaffold's CODE (invariant 8, design §12). The baseline declares
// them in its blueprint + hooks; orun executes declared argv but names no
// ecosystem. We inspect string literals and identifiers via the AST — a doc
// comment explaining the rule is allowed to name the concept, but no compiled
// token may.
var ecosystemLiterals = []string{"pnpm", "wrangler", "cloudflare"}

func TestEcosystemNeutrality(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			var text string
			switch v := n.(type) {
			case *ast.BasicLit:
				if v.Kind == token.STRING {
					text = strings.ToLower(v.Value)
				}
			case *ast.Ident:
				text = strings.ToLower(v.Name)
			}
			for _, lit := range ecosystemLiterals {
				if strings.Contains(text, lit) {
					t.Errorf("%s: token %q contains ecosystem literal %q — framework specifics belong in the baseline blueprint, not orun (invariant 8)", name, text, lit)
				}
			}
			return true
		})
	}
}

// TestScaffoldDoesNotImportTheFlowEngine binds BE-O8's deletion.
//
// A hook could once run an orun workflow file: it was the only way to reach a
// capability an argv could not express, and it cost a credential grant, a
// digest pin and a whole engine behind the scaffold path. Typed actions are
// that way now — in process, parameter-checked at parse time, with addressable
// outputs and a closed set — so two answers to one question became one.
//
// `orun workflow` itself is untouched and `internal/flow` is very much alive.
// What this asserts is that INSTANTIATION no longer reaches for it, because a
// dependency that is easy to re-add by reflex is exactly the kind worth
// writing down.
func TestScaffoldDoesNotImportTheFlowEngine(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range f.Imports {
			if strings.Trim(imp.Path.Value, `"`) == "github.com/sourceplane/orun/internal/flow" {
				t.Errorf("%s imports internal/flow — the workflow hook was retired in BE-O8; a hook reaches a capability through a typed action (internal/actions) or an argv escape", name)
			}
		}
	}
}

// TestHookIsExactlyRunOrUses binds the two-kind rule the deletion leaves.
func TestHookIsExactlyRunOrUses(t *testing.T) {
	cases := []struct {
		name string
		hook Hook
		ok   bool
	}{
		{"run", Hook{ID: "a", Run: []string{"true"}}, true},
		{"uses", Hook{ID: "b", Uses: "orun.http/probe@v1"}, true},
		{"both", Hook{ID: "c", Run: []string{"true"}, Uses: "orun.http/probe@v1"}, false},
		{"neither", Hook{ID: "d"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.hook.validate()
			if tc.ok != (err == nil) {
				t.Fatalf("validate() = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}

// A blueprint still carrying `workflow:` on a hook must fail with a message
// that says where the capability went — not with a silently ignored field.
func TestAWorkflowHookIsNowRefusedWithAPointer(t *testing.T) {
	src := `
apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: legacy
modules:
  - name: only
    mode: template
    files:
      a.txt: "a"
hooks:
  postInstantiate:
    - id: notify
      workflow: notify.yaml
`
	_, err := ParseBlueprint([]byte(src))
	if err == nil {
		t.Fatal("a hook that sets only `workflow:` now sets neither run nor uses, and must be refused rather than silently doing nothing")
	}
	if !strings.Contains(err.Error(), "run") || !strings.Contains(err.Error(), "uses") {
		t.Errorf("the error should name what a hook may be; got %v", err)
	}
}
