package catalogstore_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/catalogstore"
)

func TestPathHelpers_HappyPaths(t *testing.T) {
	const (
		srcKey = "src-branch-main-cabcdef-tabcdef0"
		catKey = "cat-deadbeef"
		name   = "web-console"
	)

	cases := []struct {
		label string
		fn    func() (string, error)
		want  string
	}{
		{"SourceDir", func() (string, error) { return catalogstore.SourceDir(srcKey) },
			"sources/" + srcKey},
		{"SourceDocPath", func() (string, error) { return catalogstore.SourceDocPath(srcKey) },
			"sources/" + srcKey + "/source.json"},
		{"CatalogDir", func() (string, error) { return catalogstore.CatalogDir(srcKey, catKey) },
			"sources/" + srcKey + "/catalogs/" + catKey},
		{"CatalogDocPath", func() (string, error) { return catalogstore.CatalogDocPath(srcKey, catKey) },
			"sources/" + srcKey + "/catalogs/" + catKey + "/catalog.json"},
		{"ComponentDir", func() (string, error) { return catalogstore.ComponentDir(srcKey, catKey, name) },
			"sources/" + srcKey + "/catalogs/" + catKey + "/components/" + name},
		{"ComponentManifestPath", func() (string, error) { return catalogstore.ComponentManifestPath(srcKey, catKey, name) },
			"sources/" + srcKey + "/catalogs/" + catKey + "/components/" + name + "/manifest.json"},
		{"CatalogGraphPath/dependencies", func() (string, error) { return catalogstore.CatalogGraphPath(srcKey, catKey, "dependencies") },
			"sources/" + srcKey + "/catalogs/" + catKey + "/graph/dependencies.json"},
		{"CatalogGraphPath/owners", func() (string, error) { return catalogstore.CatalogGraphPath(srcKey, catKey, "owners") },
			"sources/" + srcKey + "/catalogs/" + catKey + "/graph/owners.json"},
		{"CatalogRevisionDir", func() (string, error) { return catalogstore.CatalogRevisionDir(srcKey, catKey, "rev-001") },
			"sources/" + srcKey + "/catalogs/" + catKey + "/revisions/rev-001"},
		{"CatalogRevisionPlanPath", func() (string, error) { return catalogstore.CatalogRevisionPlanPath(srcKey, catKey, "rev-001") },
			"sources/" + srcKey + "/catalogs/" + catKey + "/revisions/rev-001/plan.json"},
		{"CatalogExecutionDir", func() (string, error) { return catalogstore.CatalogExecutionDir(srcKey, catKey, "rev-001", "exec-001") },
			"sources/" + srcKey + "/catalogs/" + catKey + "/revisions/rev-001/executions/exec-001"},
		{"SourceRefPath/latest", func() (string, error) { return catalogstore.SourceRefPath("latest") },
			"refs/sources/latest.json"},
		{"SourceRefPath/current", func() (string, error) { return catalogstore.SourceRefPath("current") },
			"refs/sources/current.json"},
		{"SourceRefPath/main", func() (string, error) { return catalogstore.SourceRefPath("main") },
			"refs/sources/main.json"},
		{"SourceBranchRefPath", func() (string, error) { return catalogstore.SourceBranchRefPath("feature-x") },
			"refs/sources/branches/feature-x.json"},
		{"SourcePRRefPath", func() (string, error) { return catalogstore.SourcePRRefPath("139") },
			"refs/sources/prs/139.json"},
		{"CatalogRefPath", func() (string, error) { return catalogstore.CatalogRefPath("current") },
			"refs/catalogs/current.json"},
		{"CatalogBranchRefPath", func() (string, error) { return catalogstore.CatalogBranchRefPath("feature-x") },
			"refs/catalogs/branches/feature-x.json"},
		{"CatalogPRRefPath", func() (string, error) { return catalogstore.CatalogPRRefPath("139") },
			"refs/catalogs/prs/139.json"},
		{"ComponentLocalIndexPath", func() (string, error) { return catalogstore.ComponentLocalIndexPath(srcKey, catKey, name) },
			"sources/" + srcKey + "/catalogs/" + catKey + "/indexes/components/" + name + ".json"},
		{"OwnerLocalIndexPath", func() (string, error) { return catalogstore.OwnerLocalIndexPath(srcKey, catKey, "team-platform") },
			"sources/" + srcKey + "/catalogs/" + catKey + "/indexes/owners/team-platform.json"},
		{"SystemLocalIndexPath", func() (string, error) { return catalogstore.SystemLocalIndexPath(srcKey, catKey, "billing") },
			"sources/" + srcKey + "/catalogs/" + catKey + "/indexes/systems/billing.json"},
		{"DomainLocalIndexPath", func() (string, error) { return catalogstore.DomainLocalIndexPath(srcKey, catKey, "infra") },
			"sources/" + srcKey + "/catalogs/" + catKey + "/indexes/domains/infra.json"},
		{"TypeLocalIndexPath", func() (string, error) { return catalogstore.TypeLocalIndexPath(srcKey, catKey, "service") },
			"sources/" + srcKey + "/catalogs/" + catKey + "/indexes/types/service.json"},
		{"ComponentGlobalIndexPath", func() (string, error) { return catalogstore.ComponentGlobalIndexPath("sourceplane/orun/web-console") },
			"indexes/components/sourceplane-orun-web-console.json"},
		{"CatalogGlobalIndexPath", func() (string, error) { return catalogstore.CatalogGlobalIndexPath(catKey) },
			"indexes/catalogs/" + catKey + ".json"},
		{"SourceGlobalIndexPath", func() (string, error) { return catalogstore.SourceGlobalIndexPath(srcKey) },
			"indexes/sources/" + srcKey + ".json"},
		{"ComponentHistoryEventPath", func() (string, error) {
			return catalogstore.ComponentHistoryEventPath(srcKey, catKey, name, 7, "execution.completed")
		},
			"sources/" + srcKey + "/catalogs/" + catKey + "/history/components/" + name + "/events/000000007-execution-completed.json"},
	}

	for _, tc := range cases {
		got, err := tc.fn()
		if err != nil {
			t.Errorf("%s: unexpected error: %v", tc.label, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: got %q want %q", tc.label, got, tc.want)
		}
	}
}

func TestValidateSegment_Rejections(t *testing.T) {
	cases := map[string]string{
		"empty":       "",
		"dot":         ".",
		"dotdot":      "..",
		"slash":       "a/b",
		"backslash":   "a\\b",
		"space":       "a b",
		"tab":         "a\tb",
		"uppercase":   "ABC",
		"colon":       "a:b",
		"oversized":   strings.Repeat("a", 129),
		"embedded-..": "x..y",
	}
	for label, in := range cases {
		err := catalogstore.ValidateSegment(in)
		if err == nil {
			t.Errorf("%s: %q must be rejected, got nil", label, in)
			continue
		}
		if !errors.Is(err, catalogstore.ErrInvalidPathInput) {
			t.Errorf("%s: %q error not under ErrInvalidPathInput: %v", label, in, err)
		}
	}
}

func TestValidateRefName(t *testing.T) {
	for _, ok := range []string{"latest", "current", "main"} {
		if err := catalogstore.ValidateRefName(ok); err != nil {
			t.Errorf("%q: unexpected error: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "branches", "prs", "Latest", "main2", "main."} {
		if err := catalogstore.ValidateRefName(bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

func TestValidateGraphKind_AllAndOnly(t *testing.T) {
	for _, k := range catalogstore.CatalogGraphKinds() {
		if err := catalogstore.ValidateGraphKind(k); err != nil {
			t.Errorf("%q: unexpected error: %v", k, err)
		}
	}
	for _, bad := range []string{"", "deps", "system", "owner", "Dependencies"} {
		if err := catalogstore.ValidateGraphKind(bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

func TestValidateEventKind_DotsAllowed(t *testing.T) {
	for _, ok := range []string{"execution.completed", "plan.created", "manifest-changed"} {
		if err := catalogstore.ValidateEventKind(ok); err != nil {
			t.Errorf("%q: unexpected error: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "Execution.Completed", "execution completed", "execution/completed", "x..y", strings.Repeat("a", 129)} {
		if err := catalogstore.ValidateEventKind(bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

func TestValidateSourceKey_RejectsBadShape(t *testing.T) {
	for _, bad := range []string{"", "src", "Src-foo", "src-Foo", "x-foo"} {
		if err := catalogstore.ValidateSourceKey(bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
	if err := catalogstore.ValidateSourceKey("src-branch-main-cabcdef-tabcdef0"); err != nil {
		t.Errorf("happy: %v", err)
	}
}

func TestValidateCatalogKey_AllowsCollisionSuffix(t *testing.T) {
	if err := catalogstore.ValidateCatalogKey("cat-deadbeef-x1"); err != nil {
		t.Errorf("collision suffix should be valid: %v", err)
	}
	for _, bad := range []string{"", "cat-", "cat-XYZ", "Cat-deadbeef"} {
		if err := catalogstore.ValidateCatalogKey(bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

func TestComponentGlobalIndexPath_Sanitizes(t *testing.T) {
	got, err := catalogstore.ComponentGlobalIndexPath("ns/repo/name")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "indexes/components/ns-repo-name.json" {
		t.Errorf("got %q", got)
	}
	for _, bad := range []string{"", "ns/repo", "ns/repo/name/extra", "NS/repo/name"} {
		if _, err := catalogstore.ComponentGlobalIndexPath(bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

func TestPathHelpers_RejectInvalidArgsAndDoNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("path helper panicked: %v", r)
		}
	}()
	for _, fn := range []func() (string, error){
		func() (string, error) { return catalogstore.SourceDir("../escape") },
		func() (string, error) { return catalogstore.SourceDocPath("") },
		func() (string, error) { return catalogstore.CatalogDir("src-ok-cabcdef-tabcdef0", "BAD") },
		func() (string, error) {
			return catalogstore.ComponentManifestPath("src-ok-cabcdef-tabcdef0", "cat-deadbeef", "..")
		},
		func() (string, error) {
			return catalogstore.CatalogGraphPath("src-ok-cabcdef-tabcdef0", "cat-deadbeef", "weird")
		},
		func() (string, error) {
			return catalogstore.ComponentHistoryEventPath("src-ok-cabcdef-tabcdef0", "cat-deadbeef", "name", 1, "")
		},
	} {
		if _, err := fn(); err == nil {
			t.Errorf("expected error, got nil")
		}
	}
}
