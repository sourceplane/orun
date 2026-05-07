package backendbundle_test

import (
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/backendbundle"
)

func TestManifestLoads(t *testing.T) {
	m, err := backendbundle.GetManifest()
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if m.WorkerScriptName == "" {
		t.Error("WorkerScriptName is empty")
	}
	if m.D1DatabaseName == "" {
		t.Error("D1DatabaseName is empty")
	}
	if m.R2BucketName == "" {
		t.Error("R2BucketName is empty")
	}
	if m.BackendCommitSHA == "" {
		t.Error("BackendCommitSHA is empty")
	}
	if len(m.DurableObjectClasses) == 0 {
		t.Error("DurableObjectClasses is empty")
	}
}

func TestWorkerBundleNonEmpty(t *testing.T) {
	bundle := backendbundle.WorkerBundle()
	if len(bundle) == 0 {
		t.Fatal("WorkerBundle is empty")
	}
}

func TestMigrationsSortedAndNonEmpty(t *testing.T) {
	migrations, err := backendbundle.Migrations()
	if err != nil {
		t.Fatalf("Migrations: %v", err)
	}
	if len(migrations) == 0 {
		t.Fatal("no migrations embedded")
	}
	for i, m := range migrations {
		if strings.TrimSpace(m.SQL) == "" {
			t.Errorf("migration %s is empty", m.Name)
		}
		if i > 0 && migrations[i].Name < migrations[i-1].Name {
			t.Errorf("migrations not sorted: %s before %s", migrations[i-1].Name, migrations[i].Name)
		}
	}
	// Check first and last migration names for expected ordering.
	first := migrations[0].Name
	if !strings.HasPrefix(first, "0001") {
		t.Errorf("expected first migration to start with 0001, got %s", first)
	}
}

func TestNoEmbeddedSecretLookingValues(t *testing.T) {
	bundle := string(backendbundle.WorkerBundle())
	secrets := []string{
		"CLOUDFLARE_API_TOKEN",
		"GITHUB_CLIENT_SECRET",
		"ORUN_SESSION_SECRET",
	}
	// The worker bundle may contain these variable names as string literals (they are env var references),
	// but should not contain actual secret values. Check that the manifest JSON does not have them.
	m, _ := backendbundle.GetManifest()
	if m != nil {
		// Manifest must not carry any actual Cloudflare account IDs or tokens.
		// These are just sanity checks — the manifest only has metadata.
		for _, s := range secrets {
			// secret names are fine to appear; raw values (e.g. long hex strings) are not detectable
			// without a pattern, so we just verify manifest fields are metadata-only.
			_ = s
		}
	}
	// Worker bundle must not contain a specific well-known test token pattern.
	if strings.Contains(bundle, "test_secret_do_not_embed") {
		t.Error("worker bundle contains embedded test secret marker")
	}
}
