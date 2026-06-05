package viewmodel

import (
	"reflect"
	"testing"

	"github.com/sourceplane/orun/internal/affected"
	"github.com/sourceplane/orun/internal/objcatalog"
)

func sampleObjCatalog() objcatalog.CatalogView {
	return objcatalog.CatalogView{
		SourceID: "sha256:src",
		ObjectID: "sha256:cat",
		Components: []objcatalog.CatalogComponentView{
			{ComponentKey: "ns/repo/api", Name: "api", Type: "worker", Domain: "edge",
				Path: "apps/api/component.yaml", DependsOn: []string{"ns/repo/shared"},
				Environments: map[string]objcatalog.EnvView{
					"prod":    {Profile: "release", Active: true},
					"staging": {Profile: "pr", Active: false},
				},
				Spec: map[string]any{"change": map[string]any{"watches": []any{"env", "groups"}}}},
			{ComponentKey: "ns/repo/shared", Name: "shared", Type: "library"},
			{ComponentKey: "ns/repo/web", Name: "web", Type: "worker"},
		},
	}
}

func TestBuildCatalogView_NoOverlay(t *testing.T) {
	v := BuildCatalogView(sampleObjCatalog(), nil)
	if v.SourceID != "sha256:src" || v.CatalogID != "sha256:cat" {
		t.Errorf("ids = %q / %q", v.SourceID, v.CatalogID)
	}
	if v.Overlay {
		t.Errorf("overlay should be false with nil result")
	}
	if len(v.Components) != 3 {
		t.Fatalf("components = %d", len(v.Components))
	}
	api := v.Components[0]
	if api.Key != "ns/repo/api" || api.Type != "worker" || api.Domain != "edge" {
		t.Errorf("api row = %+v", api)
	}
	if !reflect.DeepEqual(api.Envs, []string{"prod"}) { // only active envs
		t.Errorf("api.Envs = %v, want [prod]", api.Envs)
	}
	if !reflect.DeepEqual(api.DependsOn, []string{"ns/repo/shared"}) {
		t.Errorf("api.DependsOn = %v", api.DependsOn)
	}
	if api.Changed() || api.Badge() != "" {
		t.Errorf("no overlay → not changed, empty badge: %+v", api)
	}
}

func TestBuildCatalogView_WithOverlay(t *testing.T) {
	overlay := &affected.Result{
		DirectlyChanged: []string{"ns/repo/shared"},
		Dependents:      []string{"ns/repo/api", "ns/repo/web"},
	}
	v := BuildCatalogView(sampleObjCatalog(), overlay)
	if !v.Overlay {
		t.Fatalf("overlay should be true")
	}
	byKey := map[string]ComponentRow{}
	for _, r := range v.Components {
		byKey[r.Key] = r
	}
	if !byKey["ns/repo/shared"].DirectlyChanged || byKey["ns/repo/shared"].Badge() != "changed" {
		t.Errorf("shared should be directly changed: %+v", byKey["ns/repo/shared"])
	}
	if !byKey["ns/repo/api"].Dependent || byKey["ns/repo/api"].DirectlyChanged {
		t.Errorf("api should be dependent only: %+v", byKey["ns/repo/api"])
	}
	if byKey["ns/repo/api"].Badge() != "affected" {
		t.Errorf("api badge = %q, want affected", byKey["ns/repo/api"].Badge())
	}
	if byKey["ns/repo/web"].Badge() != "affected" {
		t.Errorf("web badge = %q", byKey["ns/repo/web"].Badge())
	}
}

func TestComponentRow_DirectlyChangedWinsOverDependent(t *testing.T) {
	// A component both directly changed and listed as a dependent badges "changed".
	overlay := &affected.Result{
		DirectlyChanged: []string{"ns/repo/api"},
		Dependents:      []string{"ns/repo/api"},
	}
	v := BuildCatalogView(sampleObjCatalog(), overlay)
	for _, r := range v.Components {
		if r.Key == "ns/repo/api" {
			if !r.DirectlyChanged || r.Dependent || r.Badge() != "changed" {
				t.Errorf("directly-changed must win: %+v", r)
			}
		}
	}
}

func TestFilterChanged(t *testing.T) {
	overlay := &affected.Result{
		DirectlyChanged: []string{"ns/repo/shared"},
		Dependents:      []string{"ns/repo/api"},
	}
	v := BuildCatalogView(sampleObjCatalog(), overlay)
	filtered := v.FilterChanged()
	if len(filtered.Components) != 2 {
		t.Fatalf("filtered = %d rows, want 2 (shared + api)", len(filtered.Components))
	}
	for _, r := range filtered.Components {
		if !r.Changed() {
			t.Errorf("filtered row not changed: %+v", r)
		}
	}
	// Without an overlay, filter is a no-op.
	plain := BuildCatalogView(sampleObjCatalog(), nil)
	if len(plain.FilterChanged().Components) != 3 {
		t.Errorf("no-overlay filter should keep all rows")
	}
}

func TestBuildComponentView(t *testing.T) {
	cv := BuildComponentView(sampleObjCatalog().Components[0])
	if cv.Key != "ns/repo/api" || cv.Type != "worker" || cv.Domain != "edge" {
		t.Errorf("component view = %+v", cv)
	}
	if !reflect.DeepEqual(cv.Watches, []string{"env", "groups"}) {
		t.Errorf("watches = %v", cv.Watches)
	}
	// Envs include both active and inactive, sorted by name.
	if len(cv.Envs) != 2 || cv.Envs[0].Name != "prod" || cv.Envs[1].Name != "staging" {
		t.Fatalf("envs = %+v", cv.Envs)
	}
	if !cv.Envs[0].Active || cv.Envs[0].Profile != "release" {
		t.Errorf("prod binding = %+v", cv.Envs[0])
	}
	if cv.Envs[1].Active {
		t.Errorf("staging should be inactive")
	}
}

func TestBuildComponentView_NoWatchesNoEnvs(t *testing.T) {
	cv := BuildComponentView(objcatalog.CatalogComponentView{ComponentKey: "ns/repo/x", Name: "x"})
	if cv.Watches != nil || len(cv.Envs) != 0 {
		t.Errorf("empty component → nil watches, no envs: %+v", cv)
	}
}

func TestSpecWatchesEdgeCases(t *testing.T) {
	cases := []map[string]any{
		nil,
		{"change": "not-a-map"},
		{"change": map[string]any{"watches": "not-a-list"}},
		{"change": map[string]any{"watches": []any{7, true}}}, // no strings
	}
	for i, spec := range cases {
		if got := specWatches(spec); got != nil {
			t.Errorf("case %d: specWatches = %v, want nil", i, got)
		}
	}
	// Mixed list keeps only the strings.
	if got := specWatches(map[string]any{"change": map[string]any{"watches": []any{"a", 7, "b"}}}); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("mixed watches = %v, want [a b]", got)
	}
}

func TestActiveEnvNames_AllInactive(t *testing.T) {
	row := BuildCatalogView(objcatalog.CatalogView{Components: []objcatalog.CatalogComponentView{
		{ComponentKey: "ns/repo/x", Name: "x", Environments: map[string]objcatalog.EnvView{
			"prod": {Active: false}, "dev": {Active: false},
		}},
	}}, nil).Components[0]
	if row.Envs != nil {
		t.Errorf("all-inactive envs should yield nil Envs, got %v", row.Envs)
	}
}
