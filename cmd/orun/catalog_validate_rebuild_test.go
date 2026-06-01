package main

// catalog_validate_rebuild_test.go covers the C8 additions to
// `orun catalog validate`: the --rebuild-indexes flag (which must actually
// reconstruct the global index files via catalogstore.RebuildIndexes), the
// preserved --strict behavior, and a byte-identical rebuild assertion driven
// through the CLI path.

import (
	"context"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/catalogstore"
)

// readAllIndexBodies snapshots every `indexes/*.json` body in the local store
// so a rebuild can be asserted byte-identical. Keyed by object path.
func readAllIndexBodies(t *testing.T) map[string]string {
	t.Helper()
	st, _, err := openLocalStateStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	infos, err := st.List(context.Background(), "indexes")
	if err != nil {
		t.Fatalf("list indexes/: %v", err)
	}
	out := map[string]string{}
	for _, info := range infos {
		if !strings.HasSuffix(info.Path, ".json") {
			continue
		}
		body, _, rerr := st.Read(context.Background(), info.Path)
		if rerr != nil {
			t.Fatalf("read %s: %v", info.Path, rerr)
		}
		out[info.Path] = string(body)
	}
	return out
}

// TestCatalogValidate_RebuildIndexes_ByteIdentical proves the CLI
// --rebuild-indexes path reproduces every global index byte-for-byte: capture
// the originals, scrub them, run validate --rebuild-indexes, then compare.
func TestCatalogValidate_RebuildIndexes_ByteIdentical(t *testing.T) {
	refreshSeededCatalog(t)

	originals := readAllIndexBodies(t)
	if len(originals) == 0 {
		t.Fatal("expected at least one global index after refresh")
	}

	// Scrub every captured index file.
	st, _, err := openLocalStateStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	for path := range originals {
		if derr := st.Delete(context.Background(), path); derr != nil {
			t.Fatalf("delete %s: %v", path, derr)
		}
	}

	// Run validate --rebuild-indexes through the CLI entrypoint.
	catalogValidateRebuildFlag = true
	t.Cleanup(func() { catalogValidateRebuildFlag = false })
	if rerr := runCatalogValidate(nil); rerr != nil {
		t.Fatalf("validate --rebuild-indexes: %v", rerr)
	}

	// Every original body must be reproduced exactly.
	rebuilt := readAllIndexBodies(t)
	if len(rebuilt) != len(originals) {
		t.Errorf("rebuilt index count = %d, want %d", len(rebuilt), len(originals))
	}
	for path, want := range originals {
		got, ok := rebuilt[path]
		if !ok {
			t.Errorf("rebuild missing index %s", path)
			continue
		}
		if got != want {
			t.Errorf("rebuilt %s not byte-identical:\n got %s\nwant %s", path, got, want)
		}
	}
}

// TestCatalogValidate_RebuildIndexes_Text confirms the text path prints the
// rebuilt banner and exits 0 on a clean catalog.
func TestCatalogValidate_RebuildIndexes_Text(t *testing.T) {
	refreshSeededCatalog(t)

	catalogValidateRebuildFlag = true
	t.Cleanup(func() { catalogValidateRebuildFlag = false })

	out := captureStdout(t, func() error { return runCatalogValidate(nil) })
	if !strings.Contains(out, "Global indexes rebuilt") {
		t.Errorf("expected rebuild banner, got:\n%s", out)
	}
}

// TestCatalogValidate_RebuildIndexes_EmptyStoreOK confirms a rebuild over a
// store with no sources is a no-op success (no panic, exit 0) — the rebuild
// wiring tolerates an empty tree.
func TestCatalogValidate_RebuildIndexes_EmptyStoreOK(t *testing.T) {
	withTempIntentRoot(t)
	resetCatalogFlags(t)

	st, _, err := openLocalStateStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	store := catalogstore.New(st)
	if rerr := store.RebuildIndexes(context.Background()); rerr != nil {
		t.Fatalf("rebuild on empty store should succeed, got %v", rerr)
	}
}

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
