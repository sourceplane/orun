package views

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui/services"
	"github.com/sourceplane/orun/internal/tui/theme"
)

// CatalogModel renders the multi-kind entity explorer over the object-model
// catalog (orun-service-catalog SC2/SC3): a kind tab bar with per-kind counts,
// a per-kind entity table, and a drillable entity detail page whose members
// and relation edges are themselves navigable — so the catalog reads as a
// browsable graph, not a flat list.
//
// Levels:
//
//	list   — kind tabs + entity rows ([ ] or ←/→ cycle kinds, ⏎ opens)
//	detail — one entity: identity, ownership, lifecycle, members, relations;
//	         ↑↓ moves over linked entities, ⏎ jumps to one, esc pops back
type CatalogModel struct {
	Snapshot *services.CatalogSnapshot
	Width    int
	Height   int
	Filter   string

	kinds   []string // "All" + kinds present, canonical order
	kindIdx int
	Cursor  int

	// Graph-browse stack: each drill pushes the opened entity; esc pops.
	stack        []entityRef
	detailCursor int

	byRef    map[entityRef]services.EntitySummary
	outEdges map[entityRef][]services.RelationSummary
	inEdges  map[entityRef][]services.RelationSummary
}

// entityRef identifies an entity across kinds (entity keys are only unique
// per kind, data-model.md §1).
type entityRef struct {
	Kind string
	Key  string
}

// catalogLink is one navigable row on the detail page: a member or a relation
// endpoint. Resolved is false when the target is not in the loaded catalog
// (e.g. a cross-repo dependency) — the row renders but cannot be entered.
type catalogLink struct {
	Glyph    string
	Label    string // relation type / "member"
	Display  string // target name (or key when unresolved)
	Ref      entityRef
	Resolved bool
	Note     string // optional / include annotations
}

// KindGlyph returns the single-cell glyph for an entity kind, extending the
// cockpit glyph language (cockpit/style) to the catalog's kinds.
func KindGlyph(kind string) string {
	switch kind {
	case "Component":
		return "◆"
	case "API":
		return "◇"
	case "Resource":
		return "▣"
	case "System":
		return "⬢"
	case "Domain":
		return "▦"
	case "Group":
		return "◎"
	case "User":
		return "●"
	case "Composition":
		return "❖"
	case "Environment":
		return "◍"
	case "Deployment":
		return "▲"
	}
	return "·"
}

func NewCatalogModel() CatalogModel { return CatalogModel{} }

func (m CatalogModel) Init() tea.Cmd { return nil }

// SetSize stores the stage geometry.
func (m CatalogModel) SetSize(w, h int) CatalogModel {
	m.Width = w
	m.Height = h
	return m
}

// SetFilter sets the case-insensitive substring filter for entity rows and
// resets the cursor.
func (m CatalogModel) SetFilter(f string) CatalogModel {
	m.Filter = f
	m.Cursor = 0
	return m
}

// AtRoot reports whether the model is on the list level (no entity drilled
// into), so the root model knows whether esc should pop a level or leave the
// mode.
func (m CatalogModel) AtRoot() bool { return len(m.stack) == 0 }

// SetSnapshot installs a freshly loaded catalog, rebuilding the kind tabs and
// the entity/relation indexes. Cursor and drill positions are preserved where
// they still resolve (a background refresh must not yank the user around).
func (m CatalogModel) SetSnapshot(snap *services.CatalogSnapshot) CatalogModel {
	m.Snapshot = snap
	m.byRef = map[entityRef]services.EntitySummary{}
	m.outEdges = map[entityRef][]services.RelationSummary{}
	m.inEdges = map[entityRef][]services.RelationSummary{}
	if snap == nil {
		m.kinds = nil
		m.kindIdx = 0
		m.Cursor = 0
		m.stack = nil
		return m
	}
	for _, e := range snap.Entities {
		m.byRef[entityRef{e.Kind, e.EntityKey}] = e
	}
	for _, r := range snap.Relations {
		from := entityRef{r.FromKind, r.From}
		to := entityRef{r.ToKind, r.To}
		m.outEdges[from] = append(m.outEdges[from], r)
		m.inEdges[to] = append(m.inEdges[to], r)
	}

	present := map[string]bool{}
	for _, e := range snap.Entities {
		present[e.Kind] = true
	}
	kinds := []string{"All"}
	for _, k := range services.EntityKindOrder {
		if present[k] {
			kinds = append(kinds, k)
			delete(present, k)
		}
	}
	// Unknown kinds (a newer resolver) still get a tab, alphabetically.
	extra := make([]string, 0, len(present))
	for k := range present {
		extra = append(extra, k)
	}
	sort.Strings(extra)
	kinds = append(kinds, extra...)
	m.kinds = kinds
	if m.kindIdx >= len(m.kinds) {
		m.kindIdx = 0
	}
	if rows := m.filtered(); m.Cursor >= len(rows) {
		m.Cursor = 0
	}
	// Drop drilled entities that no longer resolve in the new snapshot — a
	// refresh can delete any entry in the walk path, not just the tip, and a
	// dead middle entry would strand esc on a "no longer in catalog" page.
	if len(m.stack) > 0 {
		kept := make([]entityRef, 0, len(m.stack))
		for _, ref := range m.stack {
			if _, ok := m.byRef[ref]; ok {
				kept = append(kept, ref)
			}
		}
		if len(kept) != len(m.stack) {
			m.detailCursor = 0
		}
		m.stack = kept
	}
	// Clamp the detail cursor: the surviving tip's connections list can shrink
	// across a refresh, and a cursor past the end would scroll the viewport
	// beyond the rows (an empty CONNECTIONS pane).
	if links := m.detailLinks(); m.detailCursor >= len(links) {
		m.detailCursor = 0
	}
	return m
}

// ActiveKind returns the selected kind tab ("All" on the mixed view).
func (m CatalogModel) ActiveKind() string {
	if len(m.kinds) == 0 {
		return "All"
	}
	return m.kinds[m.kindIdx]
}

// Selected returns the entity under the cursor: the drilled entity on the
// detail level, the highlighted row on the list level. Nil when nothing
// resolves.
func (m CatalogModel) Selected() *services.EntitySummary {
	if len(m.stack) > 0 {
		if e, ok := m.byRef[m.stack[len(m.stack)-1]]; ok {
			return &e
		}
		return nil
	}
	rows := m.filtered()
	if m.Cursor < 0 || m.Cursor >= len(rows) {
		return nil
	}
	e := rows[m.Cursor]
	return &e
}

// Breadcrumb returns header crumbs past the mode chip (entity names along the
// drill stack).
func (m CatalogModel) Breadcrumb() []string {
	out := make([]string, 0, len(m.stack))
	for _, ref := range m.stack {
		if e, ok := m.byRef[ref]; ok {
			out = append(out, e.Name)
		}
	}
	return out
}

func (m CatalogModel) filtered() []services.EntitySummary {
	if m.Snapshot == nil {
		return nil
	}
	kind := m.ActiveKind()
	f := strings.ToLower(m.Filter)
	out := make([]services.EntitySummary, 0, len(m.Snapshot.Entities))
	for _, e := range m.Snapshot.Entities {
		if kind != "All" && e.Kind != kind {
			continue
		}
		if f != "" &&
			!strings.Contains(strings.ToLower(e.Name), f) &&
			!strings.Contains(strings.ToLower(e.Kind), f) &&
			!strings.Contains(strings.ToLower(e.EntityKey), f) &&
			!strings.Contains(strings.ToLower(e.Owner), f) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func (m CatalogModel) Update(msg tea.Msg) (CatalogModel, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if len(m.stack) > 0 {
		return m.updateDetail(km), nil
	}
	switch km.String() {
	case "down", "j":
		if rows := m.filtered(); m.Cursor+1 < len(rows) {
			m.Cursor++
		}
	case "up", "k":
		if m.Cursor > 0 {
			m.Cursor--
		}
	case "home":
		m.Cursor = 0
	case "end", "G":
		if rows := m.filtered(); len(rows) > 0 {
			m.Cursor = len(rows) - 1
		}
	case "right", "l", "]":
		if len(m.kinds) > 0 {
			m.kindIdx = (m.kindIdx + 1) % len(m.kinds)
			m.Cursor = 0
		}
	case "left", "h", "[":
		if len(m.kinds) > 0 {
			m.kindIdx = (m.kindIdx - 1 + len(m.kinds)) % len(m.kinds)
			m.Cursor = 0
		}
	case "enter":
		if sel := m.Selected(); sel != nil {
			m.stack = append(m.stack, entityRef{sel.Kind, sel.EntityKey})
			m.detailCursor = 0
		}
	}
	return m, nil
}

func (m CatalogModel) updateDetail(km tea.KeyMsg) CatalogModel {
	links := m.detailLinks()
	switch km.String() {
	case "down", "j":
		if m.detailCursor+1 < len(links) {
			m.detailCursor++
		}
	case "up", "k":
		if m.detailCursor > 0 {
			m.detailCursor--
		}
	case "home":
		m.detailCursor = 0
	case "end", "G":
		if len(links) > 0 {
			m.detailCursor = len(links) - 1
		}
	case "enter":
		if m.detailCursor >= 0 && m.detailCursor < len(links) {
			l := links[m.detailCursor]
			if l.Resolved {
				m.stack = append(m.stack, l.Ref)
				m.detailCursor = 0
			}
		}
	case "esc", "backspace":
		m.stack = m.stack[:len(m.stack)-1]
		m.detailCursor = 0
	}
	return m
}

// detailLinks flattens the drilled entity's navigable neighbours: members
// first, then outgoing relation edges, then incoming ones.
func (m CatalogModel) detailLinks() []catalogLink {
	if len(m.stack) == 0 {
		return nil
	}
	ref := m.stack[len(m.stack)-1]
	e, ok := m.byRef[ref]
	if !ok {
		return nil
	}
	// A derived entity's members usually re-appear as typed edges (an
	// Environment's members are exactly its incoming deployedTo sources); the
	// edge label carries more signal, so member rows that duplicate an edge
	// endpoint are suppressed.
	edgeRefs := map[entityRef]bool{}
	for _, r := range m.outEdges[ref] {
		edgeRefs[entityRef{r.ToKind, r.To}] = true
	}
	for _, r := range m.inEdges[ref] {
		edgeRefs[entityRef{r.FromKind, r.From}] = true
	}
	var links []catalogLink
	for _, member := range e.Members {
		target := entityRef{"Component", member}
		if edgeRefs[target] {
			continue
		}
		t, resolved := m.byRef[target]
		display := lastKeySegment(member)
		if resolved {
			display = t.Name
		}
		links = append(links, catalogLink{
			Glyph:    KindGlyph("Component"),
			Label:    "member",
			Display:  display,
			Ref:      target,
			Resolved: resolved,
		})
	}
	for _, r := range m.outEdges[ref] {
		target := entityRef{r.ToKind, r.To}
		t, resolved := m.byRef[target]
		display := lastKeySegment(r.To)
		if resolved {
			display = t.Name
		}
		links = append(links, catalogLink{
			Glyph:    KindGlyph(r.ToKind),
			Label:    r.Type,
			Display:  display,
			Ref:      target,
			Resolved: resolved,
			Note:     edgeNote(r),
		})
	}
	for _, r := range m.inEdges[ref] {
		source := entityRef{r.FromKind, r.From}
		t, resolved := m.byRef[source]
		display := lastKeySegment(r.From)
		if resolved {
			display = t.Name
		}
		links = append(links, catalogLink{
			Glyph:    KindGlyph(r.FromKind),
			Label:    "◂ " + r.Type,
			Display:  display,
			Ref:      source,
			Resolved: resolved,
			Note:     edgeNote(r),
		})
	}
	return links
}

func edgeNote(r services.RelationSummary) string {
	parts := []string{}
	if r.Optional {
		parts = append(parts, "optional")
	}
	if r.Include != "" && r.Include != "always" {
		parts = append(parts, r.Include)
	}
	return strings.Join(parts, " · ")
}

func lastKeySegment(key string) string {
	if i := strings.LastIndex(key, "/"); i >= 0 && i < len(key)-1 {
		return key[i+1:]
	}
	return key
}

// InspectorDesc returns the right-drawer description for the current
// selection (drilled entity or highlighted row).
func (m CatalogModel) InspectorDesc() *services.ResourceDescription {
	sel := m.Selected()
	if sel == nil {
		return nil
	}
	return entityDesc(sel, m.outEdges[entityRef{sel.Kind, sel.EntityKey}], m.inEdges[entityRef{sel.Kind, sel.EntityKey}])
}

func entityDesc(e *services.EntitySummary, out, in []services.RelationSummary) *services.ResourceDescription {
	fields := []services.DescField{
		{Label: "key", Value: e.EntityKey},
	}
	add := func(label, value string) {
		if value != "" {
			fields = append(fields, services.DescField{Label: label, Value: value})
		}
	}
	add("namespace", e.Namespace)
	add("repo", e.Repo)
	add("type", e.Type)
	add("domain", e.Domain)
	add("system", e.System)
	if e.Owner != "" {
		owner := e.Owner
		if e.OwnerSource != "" {
			owner += " (" + e.OwnerSource + ")"
		}
		add("owner", owner)
	}
	add("stage", e.Stage)
	add("tier", e.Tier)
	add("version", e.Version)
	add("lifecycle", e.Lifecycle)
	if len(e.Envs) > 0 {
		add("envs", strings.Join(e.Envs, ","))
	}
	if e.MemberCount > 0 {
		add("members", fmt.Sprintf("%d", e.MemberCount))
	}
	if len(out) > 0 || len(in) > 0 {
		add("relations", fmt.Sprintf("%d out · %d in", len(out), len(in)))
	}
	summary := e.Kind
	if e.Repo != "" {
		summary += " · " + e.Repo
	}
	return &services.ResourceDescription{
		Kind:    strings.ToLower(e.Kind),
		Name:    e.Name,
		Summary: summary,
		Fields:  fields,
	}
}

// --- Rendering ---------------------------------------------------------------

func (m CatalogModel) View() string {
	width := m.Width
	if width <= 0 {
		width = 80
	}
	if m.Snapshot == nil {
		return centerCard(width, m.Height,
			"No catalog yet — press ⌃r to resolve the workspace into a catalog.")
	}
	if len(m.stack) > 0 {
		return m.viewDetail(width)
	}
	return m.viewList(width)
}

func (m CatalogModel) viewList(width int) string {
	var b strings.Builder

	total := len(m.Snapshot.Entities)
	headerL := theme.StyleSectionTitle.Render("Catalog") +
		theme.StyleDim.Render(fmt.Sprintf("  ·  %d entities", total))
	if m.Snapshot.HumanKey != "" {
		headerL += theme.StyleDim.Render("  ·  " + m.Snapshot.HumanKey)
	}
	if m.Filter != "" {
		headerL += "  " + theme.StyleDim.Render(fmt.Sprintf("(filter: %s)", m.Filter))
	}
	b.WriteString(headerL)
	b.WriteString("\n")
	b.WriteString(m.renderKindTabs(width))
	b.WriteString("\n\n")

	rows := m.filtered()
	if len(rows) == 0 {
		hint := "No entities in this catalog."
		if m.Filter != "" {
			hint = fmt.Sprintf("No entities match %q.", m.Filter)
		}
		b.WriteString(centerCard(width, m.Height-6, hint))
		return b.String()
	}

	kind := m.ActiveKind()
	cols := columnsForKind(kind, width)
	header := " "
	for _, c := range cols {
		header += pad(c.title, c.width) + "  "
	}
	b.WriteString(" " + theme.StyleTableHeader.Render(strings.TrimRight(header, " ")))
	b.WriteString("\n")

	maxRows := m.Height - 8
	if maxRows < 3 {
		maxRows = 3
	}
	start := 0
	if m.Cursor >= maxRows {
		start = m.Cursor - maxRows + 1
	}
	end := start + maxRows
	if end > len(rows) {
		end = len(rows)
	}

	for i := start; i < end; i++ {
		e := rows[i]
		line := " " + KindGlyph(e.Kind) + " "
		for ci, c := range cols {
			val := c.value(e)
			if ci == 0 && i == m.Cursor {
				line += padStyled(theme.StyleTitle.Render(val), val, c.width) + "  "
				continue
			}
			line += pad(val, c.width) + "  "
		}
		line = strings.TrimRight(line, " ")
		if i == m.Cursor {
			b.WriteString(theme.StyleCursorBar.Render("▌") + theme.StyleTableRowSelected.Render(line))
		} else if i%2 == 1 {
			b.WriteString(" " + theme.StyleTableRowAlt.Render(line))
		} else {
			b.WriteString(" " + theme.StyleTableRow.Render(line))
		}
		b.WriteString("\n")
	}

	if end < len(rows) {
		b.WriteString(theme.StyleDim.Render(fmt.Sprintf(" … %d more", len(rows)-end)))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(theme.StyleDim.Render(
		"enter open · [ ] kind · / filter · : commands"))
	return b.String()
}

// catalogColumn is one list column: a title, a width, and a value projector.
type catalogColumn struct {
	title string
	width int
	value func(services.EntitySummary) string
}

// columnsForKind picks the table shape per kind so each tab leads with the
// fields that matter for that kind.
func columnsForKind(kind string, width int) []catalogColumn {
	name := catalogColumn{"NAME", clamp(width*32/100, 14, 40),
		func(e services.EntitySummary) string { return e.Name }}
	owner := catalogColumn{"OWNER", clamp(width*22/100, 10, 28),
		func(e services.EntitySummary) string { return zoa(e.Owner) }}
	members := catalogColumn{"MEMBERS", 7,
		func(e services.EntitySummary) string {
			if e.MemberCount == 0 {
				return "—"
			}
			return fmt.Sprintf("%d", e.MemberCount)
		}}
	switch kind {
	case "All":
		return []catalogColumn{
			{"KIND", 12, func(e services.EntitySummary) string { return e.Kind }},
			name,
			{"DETAIL", clamp(width*30/100, 12, 44), func(e services.EntitySummary) string {
				switch e.Kind {
				case "Component":
					return zoa(e.Owner)
				case "Composition":
					return zoa(e.Version)
				default:
					if e.MemberCount > 0 {
						return fmt.Sprintf("%d members", e.MemberCount)
					}
					return "—"
				}
			}},
		}
	case "Component":
		return []catalogColumn{
			name,
			owner,
			{"STAGE", 10, func(e services.EntitySummary) string { return zoa(e.Stage) }},
			{"TYPE", clamp(width*14/100, 8, 20), func(e services.EntitySummary) string { return zoa(e.Type) }},
		}
	case "Composition":
		return []catalogColumn{
			name,
			{"VERSION", 10, func(e services.EntitySummary) string { return zoa(e.Version) }},
			{"STAGE", 12, func(e services.EntitySummary) string { return zoa(e.Lifecycle) }},
			members,
		}
	default:
		return []catalogColumn{
			name,
			members,
			{"KEY", clamp(width*34/100, 14, 48), func(e services.EntitySummary) string { return e.EntityKey }},
		}
	}
}

func (m CatalogModel) renderKindTabs(width int) string {
	if m.Snapshot == nil || len(m.kinds) == 0 {
		return ""
	}
	parts := make([]string, 0, len(m.kinds))
	for i, k := range m.kinds {
		count := len(m.Snapshot.Entities)
		if k != "All" {
			count = m.Snapshot.CountsByKind[k]
		}
		label := fmt.Sprintf("%s %d", k, count)
		if k != "All" {
			label = KindGlyph(k) + " " + label
		}
		if i == m.kindIdx {
			parts = append(parts, theme.StyleChipAccent.Render(label))
		} else {
			parts = append(parts, theme.StyleChipDim.Render(label))
		}
	}
	line := strings.Join(parts, theme.StyleDim.Render("│"))
	return truncate(line, width)
}

func (m CatalogModel) viewDetail(width int) string {
	ref := m.stack[len(m.stack)-1]
	e, ok := m.byRef[ref]
	if !ok {
		return centerCard(width, m.Height, "Entity no longer in catalog — esc to go back.")
	}

	var b strings.Builder
	b.WriteString(theme.StyleSectionTitle.Render(strings.ToUpper(e.Kind)) + "  " +
		theme.StyleTitle.Render(KindGlyph(e.Kind)+" "+e.Name))
	b.WriteString("\n")
	b.WriteString(theme.StyleDim.Render(e.EntityKey))
	b.WriteString("\n\n")

	field := func(label, value string) {
		if value == "" {
			return
		}
		b.WriteString("  " + theme.StyleLabel.Render(pad(label, 11)) +
			theme.StyleValue.Render(truncate(value, width-16)) + "\n")
	}
	field("namespace", e.Namespace)
	field("repo", e.Repo)
	field("type", e.Type)
	field("domain", e.Domain)
	field("system", e.System)
	if e.Owner != "" {
		b.WriteString("  " + theme.StyleLabel.Render(pad("owner", 11)) +
			theme.StyleValue.Render(e.Owner))
		if e.OwnerSource != "" {
			b.WriteString(theme.StyleDim.Render("  (" + e.OwnerSource + ")"))
		}
		b.WriteString("\n")
	}
	field("stage", e.Stage)
	field("tier", e.Tier)
	field("version", e.Version)
	field("lifecycle", e.Lifecycle)
	if len(e.Envs) > 0 {
		field("envs", strings.Join(e.Envs, ", "))
	}

	links := m.detailLinks()
	if len(links) > 0 {
		b.WriteString("\n")
		b.WriteString(theme.StyleSectionTitle.Render(fmt.Sprintf("CONNECTIONS (%d)", len(links))))
		b.WriteString("\n")

		labelW := 0
		for _, l := range links {
			if w := len(l.Label); w > labelW {
				labelW = w
			}
		}
		if labelW > 18 {
			labelW = 18
		}

		// Scroll window over links so deep graphs stay inside the stage:
		// remaining height = stage height − lines already written − room for
		// the "… more" line and the footer hint.
		maxRows := m.Height - strings.Count(b.String(), "\n") - 3
		if maxRows < 3 {
			maxRows = 3
		}
		start := 0
		if m.detailCursor >= maxRows {
			start = m.detailCursor - maxRows + 1
		}
		end := start + maxRows
		if end > len(links) {
			end = len(links)
		}
		for i := start; i < end; i++ {
			l := links[i]
			label := theme.StyleDim.Render(pad(l.Label, labelW))
			display := l.Display
			if !l.Resolved {
				display += " ⤴" // external — not in this catalog
			}
			line := fmt.Sprintf(" %s  %s %s", label, l.Glyph, display)
			if l.Note != "" {
				line += "  " + theme.StyleDim.Render("("+l.Note+")")
			}
			if i == m.detailCursor {
				b.WriteString(theme.StyleCursorBar.Render("▌") + theme.StyleTableRowSelected.Render(line))
			} else {
				b.WriteString(" " + theme.StyleTableRow.Render(line))
			}
			b.WriteString("\n")
		}
		if end < len(links) {
			b.WriteString(theme.StyleDim.Render(fmt.Sprintf(" … %d more", len(links)-end)))
			b.WriteString("\n")
		}
	} else {
		b.WriteString("\n")
		b.WriteString(theme.StyleDim.Render("  no members or relations"))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(theme.StyleDim.Render("enter follow · esc back"))
	return b.String()
}
