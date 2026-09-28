package services

import (
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func TestChangedComponentsFromFiles_InputGlobs(t *testing.T) {
	normalized := &model.NormalizedIntent{Components: map[string]model.Component{
		"web": {Name: "web", SourcePath: "apps/web", Change: model.ComponentChange{Inputs: []string{"pnpm-lock.yaml", "tooling/**"}}},
		"api": {Name: "api", SourcePath: "apps/api"},
	}}
	changed := map[string]struct{}{"tooling/eslint/deep/rule.js": {}}
	got := changedComponentsFromFiles(normalized, nil, changed, "intent.yaml")
	if !got["web"] || got["api"] || len(got) != 1 {
		t.Fatalf("selection = %v, want only web", got)
	}
	got = changedComponentsFromFiles(normalized, nil, map[string]struct{}{"README.md": {}}, "intent.yaml")
	if len(got) != 0 {
		t.Fatalf("selection = %v, want none", got)
	}
}
