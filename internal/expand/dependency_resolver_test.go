package expand

import (
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func mkIntent(comps ...model.Component) *model.NormalizedIntent {
	intent := &model.NormalizedIntent{
		Components:     map[string]model.Component{},
		ComponentIndex: map[string]model.Component{},
	}
	for _, c := range comps {
		intent.Components[c.Name] = c
		intent.ComponentIndex[c.Name] = c
	}
	return intent
}

// ResolveComponentSet must NOT pull in dependencies whose Include is
// "if-selected" (the default after normalization).
func TestResolveComponentSet_IfSelectedDoesNotExpand(t *testing.T) {
	intent := mkIntent(
		model.Component{
			Name: "api",
			DependsOn: []model.Dependency{
				{Component: "database", Include: model.IncludeIfSelected},
			},
		},
		model.Component{Name: "database"},
	)
	r := NewDependencyResolver(intent)
	got := r.ResolveComponentSet(map[string]bool{"api": true})
	if got["database"] {
		t.Fatalf("expected database NOT pulled in via if-selected edge, got %v", got)
	}
	if !got["api"] {
		t.Fatalf("expected api in selected set, got %v", got)
	}
}

// ResolveComponentSet MUST pull in dependencies marked include: always.
func TestResolveComponentSet_AlwaysExpands(t *testing.T) {
	intent := mkIntent(
		model.Component{
			Name: "api",
			DependsOn: []model.Dependency{
				{Component: "migration", Include: model.IncludeAlways},
			},
		},
		model.Component{
			Name: "migration",
			DependsOn: []model.Dependency{
				{Component: "secrets", Include: model.IncludeAlways},
			},
		},
		model.Component{Name: "secrets"},
		model.Component{Name: "unrelated"},
	)
	r := NewDependencyResolver(intent)
	got := r.ResolveComponentSet(map[string]bool{"api": true})
	for _, want := range []string{"api", "migration", "secrets"} {
		if !got[want] {
			t.Fatalf("expected %q in selected set, got %v", want, got)
		}
	}
	if got["unrelated"] {
		t.Fatalf("unrelated must not be selected, got %v", got)
	}
}

// ResolveComponentSetAll (legacy) still pulls every transitive dependency
// regardless of include policy.
func TestResolveComponentSetAll_IncludesAllTransitively(t *testing.T) {
	intent := mkIntent(
		model.Component{
			Name: "api",
			DependsOn: []model.Dependency{
				{Component: "database", Include: model.IncludeIfSelected},
			},
		},
		model.Component{Name: "database"},
	)
	r := NewDependencyResolver(intent)
	got := r.ResolveComponentSetAll(map[string]bool{"api": true})
	if !got["database"] {
		t.Fatalf("legacy ResolveComponentSetAll must pull all deps, got %v", got)
	}
}
