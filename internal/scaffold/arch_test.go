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
