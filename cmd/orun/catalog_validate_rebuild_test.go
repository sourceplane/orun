package main

// catalog_validate_strict_test.go covers `orun catalog validate --strict`.
//
// The --rebuild-indexes flag (and its byte-identical-rebuild tests) was removed
// with the catalogstore retirement: it rebuilt the legacy global index files,
// which no longer exist. The core validate path runs the resolver
// (catalogresolve) directly, so the --strict behavior below is the surviving
// contract.

import (
	"testing"
)

// TestCatalogValidate_Strict_PromotesWarnings proves --strict turns a warning
// into a failure (exit 1) while the same catalog validates clean without it.
// The standard seeded component omits spec.lifecycle, which the resolver flags
// as a warning (resolution-pipeline.md §6).
func TestCatalogValidate_Strict_PromotesWarnings(t *testing.T) {
	refreshSeededCatalog(t)

	// Non-strict validate: warnings are allowed → exit 0.
	if err := runCatalogValidate(nil); err != nil {
		var coder interface{ ExitCode() int }
		if asExit(err, &coder) {
			t.Fatalf("non-strict validate should pass, got exit %d (%v)", coder.ExitCode(), err)
		}
		t.Fatalf("non-strict validate should pass, got %v", err)
	}

	// Strict validate: the warning is promoted to an error → exit 1.
	catalogStrictFlag = true
	t.Cleanup(func() { catalogStrictFlag = false })
	err := runCatalogValidate(nil)
	if err == nil {
		t.Fatal("strict validate should fail when a warning is present")
	}
	var coder interface{ ExitCode() int }
	if !asExit(err, &coder) || coder.ExitCode() != 1 {
		t.Errorf("expected exit 1 under --strict, got %v", err)
	}
}
