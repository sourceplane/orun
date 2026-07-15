package main

import (
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/configsurface"
)

func boolPtr(b bool) *bool { return &b }

// A static-only scope keeps the pre-broker table shape — no HEALTH column.
func TestRenderSecretsTableNoHealthColumnWhenAllStatic(t *testing.T) {
	items := []configsurface.SecretMeta{
		{SecretKey: "DATABASE_URL", Scope: "project", Version: 2, Status: "active"},
		{SecretKey: "STRIPE_KEY", Scope: "workspace", Version: 1, Status: "active"},
	}
	out := renderSecretsTable(items, false)
	if strings.Contains(out, "HEALTH") {
		t.Fatalf("static-only scope must not render a HEALTH column:\n%s", out)
	}
}

// A scope with any brokered row gains a HEALTH column, and the derived health
// axis renders orphaned / ok / unknown correctly.
func TestRenderSecretsTableHealthColumn(t *testing.T) {
	items := []configsurface.SecretMeta{
		{SecretKey: "STATIC_ONE", Scope: "project", Version: 1, Status: "active", Source: "static"},
		{SecretKey: "BROKERED_OK", Scope: "project", Version: 1, Status: "active", Source: "brokered", BindingStatus: "active", Orphaned: boolPtr(false)},
		{SecretKey: "BROKERED_ORPHAN", Scope: "project", Version: 1, Status: "active", Source: "brokered", BindingStatus: "revoked", Orphaned: boolPtr(true)},
		{SecretKey: "BROKERED_UNKNOWN", Scope: "project", Version: 1, Status: "active", Source: "brokered"},
	}
	out := renderSecretsTable(items, false)
	if !strings.Contains(out, "HEALTH") {
		t.Fatalf("brokered content must render a HEALTH column:\n%s", out)
	}
	// Rows are key-sorted: BROKERED_OK, BROKERED_ORPHAN, BROKERED_UNKNOWN, STATIC_ONE.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	find := func(key string) string {
		for _, l := range lines {
			if strings.HasPrefix(l, key) {
				return l
			}
		}
		t.Fatalf("row for %s not found:\n%s", key, out)
		return ""
	}
	if !strings.Contains(find("BROKERED_ORPHAN"), "orphaned") {
		t.Errorf("orphaned brokered row must show HEALTH=orphaned: %q", find("BROKERED_ORPHAN"))
	}
	if !strings.Contains(find("BROKERED_OK"), "ok") {
		t.Errorf("healthy brokered row must show HEALTH=ok: %q", find("BROKERED_OK"))
	}
	if !strings.Contains(find("BROKERED_UNKNOWN"), "unknown") {
		t.Errorf("brokered row with unknown health must show HEALTH=unknown: %q", find("BROKERED_UNKNOWN"))
	}
	// A static row within a brokered scope has no binding: HEALTH is "-".
	staticRow := find("STATIC_ONE")
	if strings.Contains(staticRow, "orphaned") || strings.Contains(staticRow, "ok") {
		t.Errorf("static row must render HEALTH=-, got: %q", staticRow)
	}
}

func TestOrphanWarning(t *testing.T) {
	// No orphans → no warning.
	if w := orphanWarning([]configsurface.SecretMeta{
		{SecretKey: "A", Source: "brokered", Orphaned: boolPtr(false)},
		{SecretKey: "B"},
	}); w != "" {
		t.Fatalf("expected no warning, got %q", w)
	}
	// Orphans → an actionable warning naming the keys and both remedies.
	w := orphanWarning([]configsurface.SecretMeta{
		{SecretKey: "SUPABASE_API", Source: "brokered", Orphaned: boolPtr(true)},
		{SecretKey: "CF_TOKEN", Source: "brokered", Orphaned: boolPtr(true)},
	})
	if !strings.Contains(w, "orphaned") {
		t.Errorf("warning must say orphaned: %q", w)
	}
	// Keys are listed, sorted.
	if !strings.Contains(w, "CF_TOKEN, SUPABASE_API") {
		t.Errorf("warning must list the orphaned keys sorted: %q", w)
	}
	if !strings.Contains(w, "revoke") {
		t.Errorf("warning must point at the revoke remedy: %q", w)
	}
}
