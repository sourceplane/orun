package workflowbackend

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// ecosystemLiterals are provider/framework-specific strings that must never
// appear in the workflow execution core (orun-workflows invariant 7): orun ships
// no provider — every actionRef (slack/github/http/…) lives in torkflow's action
// store. A doc comment may name the concept to explain the rule; no compiled
// token may. We inspect string literals and identifiers via the AST.
var ecosystemLiterals = []string{
	"slack", "github", "gitlab", "pagerduty", "wrangler", "cloudflare", "openai", "anthropic",
}

func TestWorkflowCoreIsEcosystemNeutral(t *testing.T) {
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
		f, perr := parser.ParseFile(fset, name, nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", name, perr)
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
				if text != "" && strings.Contains(text, lit) {
					t.Errorf("%s: token %q contains ecosystem literal %q — providers live in torkflow's action store, not orun (invariant 7)", name, text, lit)
				}
			}
			return true
		})
	}
}
