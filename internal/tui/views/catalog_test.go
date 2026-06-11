package views

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/sourceplane/orun/internal/tui/services"
)

func testCatalogSnapshot() *services.CatalogSnapshot {
	return &services.CatalogSnapshot{
		HumanKey: "cat-abc123",
		CountsByKind: map[string]int{
			"Component": 2, "System": 1, "Group": 1, "Composition": 1,
		},
		Entities: []services.EntitySummary{
			{Kind: "Component", EntityKey: "ns/repo/api", Name: "api", Namespace: "ns", Repo: "repo",
				Type: "worker", Domain: "edge", System: "checkout", Owner: "group:edge-team",
				OwnerSource: "codeowners", Stage: "production", Tier: "tier-1", Envs: []string{"dev", "prod"}},
			{Kind: "Component", EntityKey: "ns/repo/web", Name: "web", Namespace: "ns", Repo: "repo",
				Type: "worker", Domain: "edge", Owner: "group:web-team", OwnerSource: "authored"},
			{Kind: "System", EntityKey: "ns/repo/checkout", Name: "checkout", Namespace: "ns", Repo: "repo",
				MemberCount: 2, Members: []string{"ns/repo/api", "ns/repo/web"}},
			{Kind: "Group", EntityKey: "ns/repo/edge-team", Name: "edge-team", Namespace: "ns", Repo: "repo",
				MemberCount: 1, Members: []string{"ns/repo/api"}},
			{Kind: "Composition", EntityKey: "ns/repo/deploy-k8s", Name: "deploy-k8s", Namespace: "ns", Repo: "repo",
				Version: "1.2.0", Lifecycle: "stable", MemberCount: 2, Members: []string{"ns/repo/api", "ns/repo/web"}},
		},
		Relations: []services.RelationSummary{
			{From: "ns/repo/api", FromKind: "Component", Type: "dependsOn", To: "ns/repo/web", ToKind: "Component", Include: "always"},
			{From: "ns/repo/api", FromKind: "Component", Type: "partOf", To: "ns/repo/checkout", ToKind: "System"},
			{From: "ns/repo/api", FromKind: "Component", Type: "ownedBy", To: "ns/repo/edge-team", ToKind: "Group"},
		},
	}
}

func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestCatalog_KindTabsFromSnapshot(t *testing.T) {
	m := NewCatalogModel().SetSnapshot(testCatalogSnapshot())
	if got := m.ActiveKind(); got != "All" {
		t.Fatalf("default kind = %q, want All", got)
	}
	// Canonical order: All, Component, System, Group, Composition.
	want := []string{"All", "Component", "System", "Group", "Composition"}
	for i, k := range want {
		if m.ActiveKind() != k {
			t.Fatalf("tab %d: kind = %q, want %q", i, m.ActiveKind(), k)
		}
		m, _ = m.Update(keyRunes("]"))
	}
	if m.ActiveKind() != "All" {
		t.Fatalf("kind cycling should wrap to All, got %q", m.ActiveKind())
	}
	m, _ = m.Update(keyRunes("["))
	if m.ActiveKind() != "Composition" {
		t.Fatalf("[ should cycle backwards to Composition, got %q", m.ActiveKind())
	}
}

func TestCatalog_FilterAndSelection(t *testing.T) {
	m := NewCatalogModel().SetSnapshot(testCatalogSnapshot())
	m = m.SetFilter("edge-team")
	rows := m.filtered()
	// Matches the Group by name and the api Component by owner.
	if len(rows) != 2 {
		t.Fatalf("filter rows = %d, want 2 (%v)", len(rows), rows)
	}
	sel := m.Selected()
	if sel == nil || sel.Name != "api" {
		t.Fatalf("selected = %+v, want api (Component sorts first)", sel)
	}
	m = m.SetFilter("")
	m, _ = m.Update(keyRunes("]")) // → Component tab
	if m.ActiveKind() != "Component" {
		t.Fatalf("kind = %q, want Component", m.ActiveKind())
	}
	if got := len(m.filtered()); got != 2 {
		t.Fatalf("Component rows = %d, want 2", got)
	}
}

func TestCatalog_DrillAndGraphWalk(t *testing.T) {
	m := NewCatalogModel().SetSnapshot(testCatalogSnapshot())
	if !m.AtRoot() {
		t.Fatal("expected AtRoot before drilling")
	}
	// Cursor 0 on All = api; enter drills into it.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.AtRoot() {
		t.Fatal("expected detail level after enter")
	}
	sel := m.Selected()
	if sel == nil || sel.Name != "api" {
		t.Fatalf("drilled entity = %+v, want api", sel)
	}
	// api has 3 outgoing edges (dependsOn web, partOf checkout, ownedBy
	// edge-team) and no members; incoming: none.
	links := m.detailLinks()
	if len(links) != 3 {
		t.Fatalf("links = %d, want 3: %+v", len(links), links)
	}
	// Walk the partOf edge to the checkout System.
	m, _ = m.Update(keyRunes("j")) // cursor → partOf checkout
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	sel = m.Selected()
	if sel == nil || sel.Kind != "System" || sel.Name != "checkout" {
		t.Fatalf("after graph walk: selected = %+v, want System checkout", sel)
	}
	// checkout: 2 members + 1 incoming partOf edge, but the api member row is
	// suppressed because the partOf edge already covers it.
	links = m.detailLinks()
	if len(links) != 2 {
		t.Fatalf("checkout links = %d, want 2 (member dedupe): %+v", len(links), links)
	}
	if bc := m.Breadcrumb(); strings.Join(bc, ">") != "api>checkout" {
		t.Fatalf("breadcrumb = %v, want [api checkout]", bc)
	}
	// esc pops back to api, then to the list.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if sel = m.Selected(); sel == nil || sel.Name != "api" {
		t.Fatalf("after esc: selected = %+v, want api", sel)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.AtRoot() {
		t.Fatal("expected AtRoot after popping the whole stack")
	}
}

func TestCatalog_ViewRendersTabsAndRows(t *testing.T) {
	m := NewCatalogModel().SetSnapshot(testCatalogSnapshot())
	m = m.SetSize(100, 30)
	out := m.View()
	for _, want := range []string{"Catalog", "5 entities", "cat-abc123", "Component 2", "System 1", "api", "web"} {
		if !strings.Contains(out, want) {
			t.Errorf("list view missing %q", want)
		}
	}
	// Component tab leads with OWNER and STAGE columns.
	m, _ = m.Update(keyRunes("]"))
	out = m.View()
	for _, want := range []string{"OWNER", "STAGE", "group:edge-team", "production"} {
		if !strings.Contains(out, want) {
			t.Errorf("component tab missing %q", want)
		}
	}
	// Detail view shows the envelope + connections.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	out = m.View()
	for _, want := range []string{"COMPONENT", "api", "ns/repo/api", "group:edge-team", "codeowners", "CONNECTIONS (3)", "dependsOn", "partOf", "ownedBy"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail view missing %q", want)
		}
	}
}

func TestCatalog_EmptyAndNilStates(t *testing.T) {
	m := NewCatalogModel().SetSize(80, 20)
	if out := m.View(); !strings.Contains(out, "No catalog yet") {
		t.Errorf("nil snapshot should render empty state, got: %s", out)
	}
	m = m.SetSnapshot(&services.CatalogSnapshot{})
	if out := m.View(); !strings.Contains(out, "No entities") {
		t.Errorf("empty snapshot should render no-entities state, got: %s", out)
	}
	if d := m.InspectorDesc(); d != nil {
		t.Errorf("empty snapshot inspector desc = %+v, want nil", d)
	}
}

func TestCatalog_InspectorDesc(t *testing.T) {
	m := NewCatalogModel().SetSnapshot(testCatalogSnapshot())
	d := m.InspectorDesc()
	if d == nil {
		t.Fatal("nil inspector desc")
	}
	if d.Kind != "component" || d.Name != "api" {
		t.Fatalf("desc = %s/%s, want component/api", d.Kind, d.Name)
	}
	var hasOwner, hasRelations bool
	for _, f := range d.Fields {
		if f.Label == "owner" && strings.Contains(f.Value, "group:edge-team") {
			hasOwner = true
		}
		if f.Label == "relations" && f.Value == "3 out · 0 in" {
			hasRelations = true
		}
	}
	if !hasOwner || !hasRelations {
		t.Fatalf("desc fields missing owner/relations: %+v", d.Fields)
	}
}

func TestCatalog_SnapshotRefreshPreservesDrill(t *testing.T) {
	m := NewCatalogModel().SetSnapshot(testCatalogSnapshot())
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // drill api
	m = m.SetSnapshot(testCatalogSnapshot())        // background refresh
	if m.AtRoot() {
		t.Fatal("refresh must not pop a still-resolvable drilled entity")
	}
	// A refresh that drops the drilled entity pops back to the list.
	snap := testCatalogSnapshot()
	snap.Entities = snap.Entities[2:] // remove components
	m = m.SetSnapshot(snap)
	if !m.AtRoot() {
		t.Fatal("refresh dropping the drilled entity must pop the stack")
	}
}

// A refresh that deletes an entity in the *middle* of the walk path must drop
// it from the stack (not just the tip), so esc never lands on a dead page.
func TestCatalog_RefreshDropsDeadMiddleOfStack(t *testing.T) {
	m := NewCatalogModel().SetSnapshot(testCatalogSnapshot())
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // drill api
	m, _ = m.Update(keyRunes("j"))                  // → partOf checkout
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // drill checkout
	if got := strings.Join(m.Breadcrumb(), ">"); got != "api>checkout" {
		t.Fatalf("breadcrumb = %q, want api>checkout", got)
	}
	// Delete api (the middle of the path); checkout survives.
	snap := testCatalogSnapshot()
	kept := snap.Entities[:0]
	for _, e := range snap.Entities {
		if e.Name != "api" {
			kept = append(kept, e)
		}
	}
	snap.Entities = kept
	m = m.SetSnapshot(snap)
	if got := strings.Join(m.Breadcrumb(), ">"); got != "checkout" {
		t.Fatalf("breadcrumb after refresh = %q, want checkout", got)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.AtRoot() {
		t.Fatal("esc from the surviving tip should reach the list, not a dead page")
	}
}

// A refresh that shrinks the drilled entity's connections must clamp the
// detail cursor — a stale cursor would scroll the viewport past every row.
func TestCatalog_RefreshClampsDetailCursor(t *testing.T) {
	m := NewCatalogModel().SetSnapshot(testCatalogSnapshot())
	// Drill checkout (System): 2 links after member dedupe.
	m, _ = m.Update(keyRunes("]")) // Component tab
	m, _ = m.Update(keyRunes("]")) // System tab
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = m.Update(keyRunes("G")) // cursor to last link
	if m.detailCursor == 0 {
		t.Fatal("setup: expected a non-zero detail cursor")
	}
	// Refresh with all relations gone and only web as member.
	snap := testCatalogSnapshot()
	snap.Relations = nil
	for i := range snap.Entities {
		if snap.Entities[i].Name == "checkout" {
			snap.Entities[i].Members = []string{"ns/repo/web"}
			snap.Entities[i].MemberCount = 1
		}
	}
	m = m.SetSnapshot(snap)
	links := m.detailLinks()
	if m.detailCursor >= len(links) {
		t.Fatalf("detail cursor %d not clamped to %d links", m.detailCursor, len(links))
	}
	// The single remaining link must actually render.
	out := m.SetSize(100, 30).View()
	if !strings.Contains(out, "web") {
		t.Errorf("connections viewport empty after shrink: %s", out)
	}
}

// pad/padStyled/truncate must be rune- and width-safe: clipping a multi-byte
// name must never produce invalid UTF-8 or a wrong visible width.
func TestPadAndTruncate_RuneSafe(t *testing.T) {
	cases := []string{"café-sérvice", "サービス・カタログ", "plain-ascii", "héllo"}
	for _, s := range cases {
		for w := 2; w <= 14; w++ {
			got := pad(s, w)
			if lipgloss.Width(got) != w {
				t.Errorf("pad(%q, %d): visible width = %d, want %d (got %q)", s, w, lipgloss.Width(got), w, got)
			}
			if !strings.ContainsRune(got, '�') && strings.ToValidUTF8(got, "") != got {
				t.Errorf("pad(%q, %d) produced invalid UTF-8: %q", s, w, got)
			}
			tr := truncate(s, w)
			if lipgloss.Width(tr) > w {
				t.Errorf("truncate(%q, %d): visible width = %d > %d", s, w, lipgloss.Width(tr), w)
			}
			if strings.ToValidUTF8(tr, "") != tr {
				t.Errorf("truncate(%q, %d) produced invalid UTF-8: %q", s, w, tr)
			}
		}
	}
}
