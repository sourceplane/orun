package scaffold

import (
	"strings"
	"testing"
)

func TestParseBlueprintValid(t *testing.T) {
	bp, err := ParseBlueprint([]byte(singleComponentBP))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if bp.Metadata.Name != "cloudflare-worker-svc" {
		t.Errorf("name = %q", bp.Metadata.Name)
	}
	if len(bp.Modules) != 1 || bp.Modules[0].Mode != ModeTemplate {
		t.Errorf("modules = %+v", bp.Modules)
	}
}

func TestParseBlueprintErrors(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		want string
	}{
		{"bad apiVersion", "apiVersion: wrong\nkind: Blueprint\nmetadata: {name: x}\nmodules: [{name: m, mode: template, files: {a: b}}]", "apiVersion"},
		{"bad kind", "apiVersion: orun.dev/v1\nkind: Nope\nmetadata: {name: x}\nmodules: [{name: m, mode: template, files: {a: b}}]", "kind"},
		{"no name", "apiVersion: orun.dev/v1\nkind: Blueprint\nmetadata: {}\nmodules: [{name: m, mode: template, files: {a: b}}]", "metadata.name"},
		{"no modules", "apiVersion: orun.dev/v1\nkind: Blueprint\nmetadata: {name: x}\nmodules: []", "at least one module"},
		{"unknown mode", "apiVersion: orun.dev/v1\nkind: Blueprint\nmetadata: {name: x}\nmodules: [{name: m, mode: teleport}]", "unknown mode"},
		{"unknown source", "apiVersion: orun.dev/v1\nkind: Blueprint\nmetadata: {name: x}\nmodules: [{name: m, mode: copy, source: ghost, from: a}]", "unknown source"},
		{"dup module", "apiVersion: orun.dev/v1\nkind: Blueprint\nmetadata: {name: x}\nmodules: [{name: m, mode: template, files: {a: b}}, {name: m, mode: template, files: {c: d}}]", "duplicate module"},
		{"dangling dependsOn", "apiVersion: orun.dev/v1\nkind: Blueprint\nmetadata: {name: x}\nmodules: [{name: m, mode: template, files: {a: b}, dependsOn: [ghost]}]", "dependsOn unknown module"},
		{"unknown source kind", "apiVersion: orun.dev/v1\nkind: Blueprint\nmetadata: {name: x}\nsources: [{name: s, kind: smtp}]\nmodules: [{name: m, mode: copy, source: s, from: a}]", "unknown kind"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseBlueprint([]byte(c.doc))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), c.want)
			}
		})
	}
}
