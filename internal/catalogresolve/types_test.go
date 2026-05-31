package catalogresolve

import (
	"errors"
	"strings"
	"testing"
)

func TestErrorTypes_RenderMessages(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		wantIn []string
	}{
		{
			name:   "ManifestInvalidNoPointer",
			err:    &ErrManifestInvalid{File: "apps/x/component.yaml", Reason: "boom"},
			wantIn: []string{"apps/x/component.yaml", "boom"},
		},
		{
			name:   "ManifestInvalidWithPointer",
			err:    &ErrManifestInvalid{File: "apps/x/component.yaml", Pointer: "/spec/type", Reason: "boom"},
			wantIn: []string{"apps/x/component.yaml", "/spec/type", "boom"},
		},
		{
			name: "MixedExtension",
			err: &ErrManifestMixedExtension{
				Dir:   "apps/dup",
				Paths: []string{"apps/dup/component.yaml", "apps/dup/component.yml"},
			},
			wantIn: []string{"apps/dup", "component.yaml", "component.yml"},
		},
		{
			name:   "IntentInvalid",
			err:    &ErrIntentInvalid{File: "intent.yaml", Reason: "yaml: blah"},
			wantIn: []string{"intent.yaml", "yaml: blah"},
		},
		{
			name:   "WorkspaceInvalid",
			err:    &ErrWorkspaceInvalid{Reason: "missing"},
			wantIn: []string{"workspace invalid", "missing"},
		},
		{
			name:   "Internal",
			err:    &errInternal{Stage: "schema-compile", Err: errors.New("oops")},
			wantIn: []string{"schema-compile", "oops"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := tc.err.Error()
			for _, sub := range tc.wantIn {
				if !strings.Contains(msg, sub) {
					t.Errorf("Error() = %q, missing substring %q", msg, sub)
				}
			}
		})
	}
}

func TestErrInternal_Unwrap(t *testing.T) {
	inner := errors.New("inner")
	e := &errInternal{Stage: "x", Err: inner}
	if !errors.Is(e, inner) {
		t.Fatal("errors.Is did not unwrap errInternal to inner")
	}
}

func TestBuildExcludeSet_TrimAndSlash(t *testing.T) {
	set := buildExcludeSet([]string{"  ", "fixtures/", "/dist", "has/slash", ""})
	if _, ok := set["fixtures"]; !ok {
		t.Errorf("trailing slash not stripped: %v", set)
	}
	if _, ok := set["dist"]; !ok {
		t.Errorf("leading slash not stripped: %v", set)
	}
	if _, ok := set["has/slash"]; ok {
		t.Errorf("path-style entry leaked into exclude set: %v", set)
	}
	// Default excludes always present.
	for _, def := range defaultExcludes {
		if _, ok := set[def]; !ok {
			t.Errorf("default exclude %q missing", def)
		}
	}
}
