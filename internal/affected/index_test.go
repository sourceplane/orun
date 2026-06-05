package affected

import (
	"testing"

	"github.com/sourceplane/orun/internal/objcatalog"
)

func TestOwnership_DeepestPrefixWins(t *testing.T) {
	cat := &objcatalog.CatalogView{
		Components: []objcatalog.CatalogComponentView{
			{ComponentKey: "ns/repo/outer", Name: "outer"},
			{ComponentKey: "ns/repo/inner", Name: "inner"},
		},
		Ownership: &objcatalog.OwnershipView{
			SchemaVersion: 1,
			Components: map[string]string{
				"apps":       "ns/repo/outer",
				"apps/inner": "ns/repo/inner",
			},
		},
	}
	idx := NewDetector(cat, IntentImpactWatch).index()
	if got := idx.ownerOf("apps/inner/main.go"); got != "ns/repo/inner" {
		t.Errorf("deepest-prefix: nested file owned by %q, want inner", got)
	}
	if got := idx.ownerOf("apps/other/main.go"); got != "ns/repo/outer" {
		t.Errorf("file under apps owned by %q, want outer", got)
	}
	if got := idx.ownerOf("libs/x.go"); got != "" {
		t.Errorf("unowned file → %q, want empty", got)
	}
}

func TestOwnership_RootComponentOwnsOnlyRootFiles(t *testing.T) {
	cat := &objcatalog.CatalogView{
		Components: []objcatalog.CatalogComponentView{{ComponentKey: "ns/repo/root", Name: "root"}},
		Ownership: &objcatalog.OwnershipView{
			SchemaVersion: 1,
			Components:    map[string]string{".": "ns/repo/root"},
		},
	}
	idx := NewDetector(cat, IntentImpactWatch).index()
	if got := idx.ownerOf("main.go"); got != "ns/repo/root" {
		t.Errorf("root file owned by %q, want root", got)
	}
	// A nested file is NOT owned by the root component (it would belong to a
	// nested component if one existed).
	if got := idx.ownerOf("apps/api/main.go"); got != "" {
		t.Errorf("nested file under root component → %q, want empty", got)
	}
}

func TestClassify(t *testing.T) {
	cat := sampleCatalog()
	idx := NewDetector(cat, IntentImpactWatch).index()
	cases := []struct {
		path string
		kind classKind
	}{
		{"intent.yaml", classGlobal},
		{"apps/api/component.yaml", classStructural},
		{"node_modules/x/y.js", classIgnore},
		{"apps/api/main.go", classComponent},
		{"unowned/file.txt", classIgnore},
	}
	for _, c := range cases {
		if got := idx.classify(c.path); got.kind != c.kind {
			t.Errorf("classify(%q).kind = %d, want %d", c.path, got.kind, c.kind)
		}
	}
}

func TestComponentWatches(t *testing.T) {
	c := objcatalog.CatalogComponentView{Spec: map[string]any{
		"change": map[string]any{"watches": []any{"env", "groups", 7}},
	}}
	got := componentWatches(c)
	if len(got) != 2 || got[0] != "env" || got[1] != "groups" {
		t.Errorf("watches = %v, want [env groups]", got)
	}
	// No change block → nil.
	if componentWatches(objcatalog.CatalogComponentView{Spec: map[string]any{}}) != nil {
		t.Errorf("missing change block should yield nil watches")
	}
}

func TestForwardAndReverseClosure_Transitive(t *testing.T) {
	// a → b → c (a depends_on b depends_on c).
	cat := &objcatalog.CatalogView{
		Components: []objcatalog.CatalogComponentView{
			{ComponentKey: "x/y/a", Name: "a"}, {ComponentKey: "x/y/b", Name: "b"}, {ComponentKey: "x/y/c", Name: "c"},
		},
		Graph: map[string]objcatalog.GraphView{"dependencies": {Edges: []objcatalog.GraphEdgeView{
			{From: "x/y/a", To: "x/y/b"}, {From: "x/y/b", To: "x/y/c"},
		}}},
	}
	idx := NewDetector(cat, IntentImpactWatch).index()
	fwd := idx.forwardClosure(map[string]bool{"x/y/a": true})
	if len(fwd) != 2 || fwd[0] != "x/y/b" || fwd[1] != "x/y/c" {
		t.Errorf("forward closure of a = %v, want [b c]", fwd)
	}
	rev := idx.reverseClosure(map[string]bool{"x/y/c": true})
	if len(rev) != 2 || rev[0] != "x/y/a" || rev[1] != "x/y/b" {
		t.Errorf("reverse closure of c = %v, want [a b]", rev)
	}
}

func TestIntentInFilesAndNormPath(t *testing.T) {
	if !intentInFiles([]string{"src/x.go", "intent.yaml"}, "intent.yaml") {
		t.Errorf("exact intent match missed")
	}
	if !intentInFiles([]string{"./intent.yaml"}, "intent.yaml") {
		t.Errorf("./-prefixed intent match missed")
	}
	if !intentInFiles([]string{"sub/dir/intent.yaml"}, "intent.yaml") {
		t.Errorf("basename suffix match missed")
	}
	if intentInFiles([]string{"src/x.go"}, "intent.yaml") {
		t.Errorf("false positive intent match")
	}
	if intentInFiles([]string{"intent.yaml"}, "") {
		t.Errorf("empty intent path should never match")
	}
	if normPath(".\\a\\b") != "a/b" {
		t.Errorf("normPath backslash/./ handling: %q", normPath(".\\a\\b"))
	}
}
