package catalogsync

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// forbiddenImports are packages internal/catalogsync must never import. The
// sync seam is local-only and driver-ready: no networking (net/http), no
// runtime coupling (internal/runner), and no CLI coupling (cmd/orun). Keeping
// the package free of these proves the local catalog model is shaped for a
// future remote driver without dragging the rest of the binary along.
var forbiddenImports = []string{
	"net/http",
	"github.com/sourceplane/orun/internal/runner",
	"github.com/sourceplane/orun/cmd/orun",
}

// TestNoForbiddenImports scans every non-test .go file in this package and
// fails if any imports a forbidden package. It checks direct imports — the
// architecture rule the C9 milestone specifies — using the Go parser so it
// stays self-contained (no toolchain shell-out, no network).
func TestNoForbiddenImports(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	fset := token.NewFileSet()
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		scanned++

		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, spec := range f.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("unquote import %s in %s: %v", spec.Path.Value, name, err)
			}
			for _, bad := range forbiddenImports {
				if path == bad || strings.HasPrefix(path, bad+"/") {
					t.Errorf("%s imports forbidden package %q", name, path)
				}
			}
		}
	}

	if scanned == 0 {
		t.Fatal("no non-test .go files scanned; import-boundary check is vacuous")
	}
}
