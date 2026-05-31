package catalogmodel_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

func TestNewIDsHavePrefixes(t *testing.T) {
	src := catalogmodel.NewSourceSnapshotID()
	cat := catalogmodel.NewCatalogSnapshotID()
	cmp := catalogmodel.NewComponentID()
	if !strings.HasPrefix(src, "src_") {
		t.Errorf("NewSourceSnapshotID = %q; missing src_ prefix", src)
	}
	if !strings.HasPrefix(cat, "cat_") {
		t.Errorf("NewCatalogSnapshotID = %q; missing cat_ prefix", cat)
	}
	if !strings.HasPrefix(cmp, "cmp_") {
		t.Errorf("NewComponentID = %q; missing cmp_ prefix", cmp)
	}
	if !catalogmodel.HasIDPrefix(src) || !catalogmodel.HasIDPrefix(cat) || !catalogmodel.HasIDPrefix(cmp) {
		t.Error("HasIDPrefix returned false for freshly minted ID")
	}
	if catalogmodel.HasIDPrefix("foo_01ABC") {
		t.Error("HasIDPrefix accepted unknown prefix")
	}
}

func TestFormatAndValidateSourceSnapshotKey(t *testing.T) {
	tests := []struct {
		name  string
		parts catalogmodel.SourceKeyParts
		valid bool
	}{
		{
			name:  "branch-main with head + tree",
			parts: catalogmodel.SourceKeyParts{Scope: "branch-main", HeadShort: "def456a", TreeShort: "5ab21c3"},
			valid: true,
		},
		{
			name:  "local-nogit with dirty",
			parts: catalogmodel.SourceKeyParts{Scope: "local-nogit", DirtyShort: "abcdef012"},
			valid: true,
		},
		{
			name:  "local-nogit no dirty",
			parts: catalogmodel.SourceKeyParts{Scope: "local-nogit"},
			valid: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			key := catalogmodel.FormatSourceSnapshotKey(tc.parts)
			err := catalogmodel.ValidateSourceSnapshotKey(key)
			if tc.valid && err != nil {
				t.Fatalf("expected %q to validate, got err: %v", key, err)
			}
			if !tc.valid && err == nil {
				t.Fatalf("expected %q to fail validation", key)
			}
		})
	}
}

func TestValidateSourceSnapshotKey_RejectsMalformed(t *testing.T) {
	cases := []string{"", "main", "src-", "SRC-main", "src-" + strings.Repeat("x", 200)}
	for _, k := range cases {
		err := catalogmodel.ValidateSourceSnapshotKey(k)
		if err == nil {
			t.Errorf("expected error for %q", k)
		}
		if err != nil && !errors.Is(err, catalogmodel.ErrInvalidKey) {
			t.Errorf("error for %q does not wrap ErrInvalidKey: %v", k, err)
		}
	}
}

func TestFormatAndValidateCatalogSnapshotKey(t *testing.T) {
	hash := "sha256:c8e91d2a0123456789abcdef"
	got := catalogmodel.FormatCatalogSnapshotKey(hash, 8)
	if got != "cat-c8e91d2a" {
		t.Errorf("FormatCatalogSnapshotKey: got %q want %q", got, "cat-c8e91d2a")
	}
	if err := catalogmodel.ValidateCatalogSnapshotKey(got); err != nil {
		t.Errorf("validate %q: %v", got, err)
	}
	// width clamp
	if k := catalogmodel.FormatCatalogSnapshotKey(hash, 1); len(k) != len("cat-")+6 {
		t.Errorf("expected width clamp to 6, got %q", k)
	}
	if k := catalogmodel.FormatCatalogSnapshotKey(hash, 100); len(k) != len("cat-")+16 {
		t.Errorf("expected width clamp to 16, got %q", k)
	}
	// invalid
	if err := catalogmodel.ValidateCatalogSnapshotKey("bogus"); err == nil {
		t.Error("expected error on bogus catalog key")
	}
}

func TestFormatAndValidateComponentKey(t *testing.T) {
	got := catalogmodel.FormatComponentKey("sourceplane", "orun", "api-edge")
	if got != "sourceplane/orun/api-edge" {
		t.Errorf("FormatComponentKey = %q", got)
	}
	if err := catalogmodel.ValidateComponentKey(got); err != nil {
		t.Errorf("ValidateComponentKey: %v", err)
	}
	for _, k := range []string{"", "two/segments", "X/Y/Z", "a/b/c/d"} {
		if err := catalogmodel.ValidateComponentKey(k); err == nil {
			t.Errorf("expected error for %q", k)
		}
	}
}

func TestKeyPatternsExposed(t *testing.T) {
	if catalogmodel.SourceKeyPattern() == nil {
		t.Error("SourceKeyPattern returned nil")
	}
	if catalogmodel.CatalogKeyPattern() == nil {
		t.Error("CatalogKeyPattern returned nil")
	}
	if catalogmodel.ComponentKeyPattern() == nil {
		t.Error("ComponentKeyPattern returned nil")
	}
}
