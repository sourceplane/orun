package integrationscli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// providerAPIHostnames are provider API endpoints that must never appear in
// this package (design.md §5, extending the workflowbackend ecosystem-literal
// invariant): this surface talks to orun-cloud through configsurface/
// remotestate, never to provider APIs directly. Provider *names* may appear as
// data (descriptors are data); provider *hostnames* may not. The one
// grandfathered SDK exception stays internal/cloudflare (orun backend/
// materialize), which is not this package.
var providerAPIHostnames = []string{
	"api.cloudflare.com",
	"api.github.com",
	"slack.com",
	"api.openai.com",
	"api.anthropic.com",
	"gitlab.com",
	"pagerduty.com",
	"supabase.co",
	"amazonaws.com",
	"api.daytona.io",
}

func TestIntegrationsCliHasNoProviderAPIHostnames(t *testing.T) {
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
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			text := strings.ToLower(lit.Value)
			for _, host := range providerAPIHostnames {
				if strings.Contains(text, host) {
					t.Errorf("%s: string %s contains provider API hostname %q — this surface talks to orun-cloud only (design.md §5)", name, lit.Value, host)
				}
			}
			return true
		})
	}
}
