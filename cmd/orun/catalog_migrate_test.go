package main

import (
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/objplan"
)

func authored(apiVersion, name, owner, lifecycle, system, file string) catalogresolve.AuthoredManifest {
	return catalogresolve.AuthoredManifest{
		SourceFile: file,
		Component: catalogmodel.ComponentYAML{
			APIVersion: apiVersion,
			Metadata:   catalogmodel.ComponentYAMLMetadata{Name: name},
			Spec:       catalogmodel.ComponentYAMLSpec{Type: "service", Owner: owner, Lifecycle: lifecycle, System: system},
		},
	}
}

func codes(fs []catalogMigrateFinding) map[string]string {
	m := map[string]string{}
	for _, f := range fs {
		m[f.Code] = f.Severity
	}
	return m
}

func TestLintComponentForV1(t *testing.T) {
	// A fully v1-ready component (orun.io apiVersion, authored owner, lifecycle,
	// system) yields no findings.
	ready := authored("orun.io/v1", "api", "team", "production", "edge", "apps/api/component.yaml")
	if fs := lintComponentForV1(ready, nil); len(fs) != 0 {
		t.Errorf("ready component should have no findings: %v", fs)
	}

	// A legacy, unowned, stageless, systemless component flags all four.
	legacy := authored("sourceplane.io/v1", "legacy", "", "", "", "libs/legacy/component.yaml")
	got := codes(lintComponentForV1(legacy, nil))
	for code, wantSev := range map[string]string{
		"legacy-apiversion": "info",
		"unowned":           "recommend",
		"no-lifecycle":      "recommend",
		"no-system":         "info",
	} {
		if got[code] != wantSev {
			t.Errorf("finding %q severity = %q, want %q (all: %v)", code, got[code], wantSev, got)
		}
	}
	if !hasRecommend(lintComponentForV1(legacy, nil)) {
		t.Error("legacy component should have recommend-level findings")
	}

	// CODEOWNERS coverage suppresses the unowned finding even with no authored owner.
	resolver := objplan.OwnerResolver(func(path string) []string {
		if path == "libs/legacy/component.yaml" {
			return []string{"@org/team"}
		}
		return nil
	})
	got = codes(lintComponentForV1(legacy, resolver))
	if _, ok := got["unowned"]; ok {
		t.Errorf("CODEOWNERS coverage should suppress 'unowned': %v", got)
	}
}
