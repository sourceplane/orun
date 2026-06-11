package views

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/cockpit/style"
	"github.com/sourceplane/orun/internal/tui/services"
	"github.com/sourceplane/orun/internal/tui/theme"
)

// CatalogModel renders the multi-kind entity explorer over the object-model
// catalog (orun-service-catalog SC2/SC3): a kind tab bar with per-kind counts,
// a per-kind entity table, and a drillable entity detail page whose members
// and relation edges are themselves navigable — so the catalog reads as a
// browsable graph, not a flat list.
//
// Component entities additionally carry the work surface: the changed/affected
// overlay (c toggles changed-only), a component-scoped run (r), compose (g),
// the classic component page (o), and an EXECUTIONS section on the detail page
// whose rows drill into the Activity run view.
//
// Levels:
//
//	list   — kind tabs + entity rows ([ ] or ←/→ cycle kinds, ⏎ opens)
//	detail — one entity: identity, ownership, lifecycle, members, relations,
//	         executions; ↑↓ moves over the rows, ⏎ follows one, esc pops back
type CatalogModel struct {
	Snapshot *services.CatalogSnapshot
	Width    int
	Height   int
	Filter   string
	// ChangedOnly filters the list to Component entities in the
	// changed/affected overlay (toggled with `c`, mirrors Browse).
	ChangedOnly bool

	kinds   []string // "All" + kinds present, canonical order
	kindIdx int
	Cursor  int

	// Graph-browse stack: each drill pushes the opened entity; esc pops.
	stack        []entityRef
	detailCursor int

	byRef    map[entityRef]services.EntitySummary
	outEdges map[entityRef][]services.RelationSummary
	inEdges  map[entityRef][]services.RelationSummary

	// Work-surface context for Component entities, injected by the root model
	// from the workspace + run history: change overlay, last-run status, and
	// recent executions, keyed by component name.
	compInfo map[string]services.ComponentSummary
	compRuns map[string][]services.RunSummary
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

// KindGlyph returns the single-cell glyph for an entity kind. The mapping
// itself lives in the shared cockpit glyph language (cockpit/style) so the
// CLI and TUI can never drift.
func KindGlyph(kind string) string { return style.EntityKindGlyph(kind) }

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

// SetComponentContext installs the work-surface context for Component
// entities: the workspace component summaries (change overlay, last-run
// status, envs) and the recent executions per component name. Called by the
// root model whenever the workspace or run history refreshes.
func (m CatalogModel) SetComponentContext(comps []services.ComponentSummary, runs map[string][]services.RunSummary) CatalogModel {
	m.compInfo = make(map[string]services.ComponentSummary, len(comps))
	for _, c := range comps {
		m.compInfo[c.Name] = c
	}
	m.compRuns = runs
	// The changed set can shrink on refresh; keep the cursor on a real row.
	if rows := m.filtered(); m.Cursor >= len(rows) {
		m.Cursor = 0
	}
	if rows := m.detailRows(); m.detailCursor >= len(rows) {
		m.detailCursor = 0
	}
	return m
}

// componentInfo returns the workspace summary for a Component entity (zero
// value when the entity is not a component or has no workspace counterpart).
func (m CatalogModel) componentInfo(e services.EntitySummary) (services.ComponentSummary, bool) {
	if e.Kind != "Component" {
		return services.ComponentSummary{}, false
	}
	c, ok := m.compInfo[e.Name]
	return c, ok
}

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
	// Clamp the detail cursor: the surviving tip's row set can shrink across a
	// refresh, and a cursor past the end would scroll the viewport beyond the
	// rows (an empty pane).
	if rows := m.detailRows(); m.detailCursor >= len(rows) {
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
		if m.ChangedOnly {
			c, ok := m.componentInfo(e)
			if !ok || !c.Changed {
				continue
			}
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

// --- Component work-surface messages -----------------------------------------

// componentActionCmd maps the r/g/o action keys on a Component entity to the
// same messages the Browse/Component surfaces emit, so the root model's run,
// compose, and open flows are shared verbatim. Nil for non-component entities
// and unknown keys.
func componentActionCmd(sel *services.EntitySummary, key string) tea.Cmd {
	if sel == nil || sel.Kind != "Component" {
		return nil
	}
	name := sel.Name
	switch key {
	case "r":
		return func() tea.Msg { return ComponentRunRequestedMsg{Name: name} }
	case "g":
		return func() tea.Msg { return ComponentEnterMsg{Name: name} }
	case "o":
		return func() tea.Msg { return ComponentOpenMsg{Name: name} }
	}
	return nil
}

func (m CatalogModel) Update(msg tea.Msg) (CatalogModel, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if len(m.stack) > 0 {
		return m.updateDetail(km)
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
	case "c":
		m.ChangedOnly = !m.ChangedOnly
		m.Cursor = 0
	case "r", "g", "o":
		return m, componentActionCmd(m.Selected(), km.String())
	case "enter":
		if sel := m.Selected(); sel != nil {
			m.stack = append(m.stack, entityRef{sel.Kind, sel.EntityKey})
			m.detailCursor = 0
		}
	}
	return m, nil
}

func (m CatalogModel) updateDetail(km tea.KeyMsg) (CatalogModel, tea.Cmd) {
	rows := m.detailRows()
	switch km.String() {
	case "down", "j":
		if m.detailCursor+1 < len(rows) {
			m.detailCursor++
		}
	case "up", "k":
		if m.detailCursor > 0 {
			m.detailCursor--
		}
	case "home":
		m.detailCursor = 0
	case "end", "G":
		if len(rows) > 0 {
			m.detailCursor = len(rows) - 1
		}
	case "r", "g", "o":
		return m, componentActionCmd(m.Selected(), km.String())
	case "enter":
		if m.detailCursor >= 0 && m.detailCursor < len(rows) {
			row := rows[m.detailCursor]
			switch {
			case row.run != nil:
				// Hand off to the Activity run → job → logs drilldown.
				execID := row.run.ExecID
				return m, func() tea.Msg { return ComponentJobOpenMsg{ExecID: execID} }
			case row.link != nil && row.link.Resolved:
				m.stack = append(m.stack, row.link.Ref)
				m.detailCursor = 0
			}
		}
	case "esc", "backspace":
		m.stack = m.stack[:len(m.stack)-1]
		m.detailCursor = 0
	}
	return m, nil
}

// detailRow is one navigable row on the detail page: a graph connection or a
// recent execution (Component entities only). Exactly one field is set.
type detailRow struct {
	link *catalogLink
	run  *services.RunSummary
}

// detailRows is the full navigable row set of the drilled entity's detail
// page: connections first, then recent executions.
func (m CatalogModel) detailRows() []detailRow {
	links := m.detailLinks()
	runs := m.detailExecutions()
	rows := make([]detailRow, 0, len(links)+len(runs))
	for i := range links {
		rows = append(rows, detailRow{link: &links[i]})
	}
	for i := range runs {
		rows = append(rows, detailRow{run: &runs[i]})
	}
	return rows
}

// detailExecutions returns the drilled Component entity's recent executions
// (nil for other kinds or when no history is loaded).
func (m CatalogModel) detailExecutions() []services.RunSummary {
	if len(m.stack) == 0 {
		return nil
	}
	e, ok := m.byRef[m.stack[len(m.stack)-1]]
	if !ok || e.Kind != "Component" {
		return nil
	}
	return m.compRuns[e.Name]
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
	desc := entityDesc(sel, m.outEdges[entityRef{sel.Kind, sel.EntityKey}], m.inEdges[entityRef{sel.Kind, sel.EntityKey}])
	// Work-surface context for components: change overlay, last run, and the
	// recent execution list (mirrors the Browse inspector).
	if c, ok := m.componentInfo(*sel); ok {
		if c.ChangeKind != "" {
			desc.Fields = append(desc.Fields, services.DescField{Label: "change", Value: c.ChangeKind})
		}
		if c.LastRunStatus != "" {
			desc.Fields = append(desc.Fields, services.DescField{Label: "last run", Value: c.LastRunStatus})
		}
		if runs := m.compRuns[sel.Name]; len(runs) > 0 {
			lines := make([]string, 0, len(runs))
			for _, r := range runs {
				id := r.ExecID
				if len(id) > 8 {
					id = id[:8]
				}
				lines = append(lines, fmt.Sprintf("%s %s", id, r.Status))
			}
			desc.Fields = append(desc.Fields, services.DescField{
				Label: "recent runs",
				Value: strings.Join(lines, "\n"),
			})
		}
	}
	return desc
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
	if changed := m.changedCount(); changed > 0 {
		headerL += "  " + theme.StyleChangedDot.Render(fmt.Sprintf("● %d changed", changed))
	}
	if m.ChangedOnly {
		headerL += "  " + theme.StyleChipAccent.Render("changed-only")
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
		switch {
		case m.ChangedOnly:
			hint = "No changed components — press c to show all entities."
		case m.Filter != "":
			hint = fmt.Sprintf("No entities match %q.", m.Filter)
		}
		b.WriteString(centerCard(width, m.Height-6, hint))
		return b.String()
	}

	kind := m.ActiveKind()
	cols := m.columnsForKind(kind, width)
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
	start, end := viewportWindow(m.Cursor, len(rows), maxRows)

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
	hints := "enter open · [ ] kind · c changed-only · / filter"
	if sel := m.Selected(); sel != nil && sel.Kind == "Component" {
		hints = "enter open · r run · g compose · o page · c changed-only · [ ] kind · / filter"
	}
	b.WriteString(theme.StyleDim.Render(hints))
	return b.String()
}

// changedCount counts Component entities in the changed/affected overlay.
func (m CatalogModel) changedCount() int {
	if m.Snapshot == nil {
		return 0
	}
	n := 0
	for _, e := range m.Snapshot.Entities {
		if c, ok := m.componentInfo(e); ok && c.Changed {
			n++
		}
	}
	return n
}

// catalogColumn is one list column: a title, a width, and a value projector.
type catalogColumn struct {
	title string
	width int
	value func(services.EntitySummary) string
}

// changeMark renders the changed/affected overlay dot for a Component entity
// row (mirrors Browse), or blanks for unaffected rows and other kinds.
func (m CatalogModel) changeMark(e services.EntitySummary) string {
	c, ok := m.componentInfo(e)
	if !ok {
		return "   "
	}
	switch c.ChangeKind {
	case "changed":
		return " " + theme.ChangedDot() + " "
	case "affected":
		return " " + theme.AffectedDot() + " "
	default:
		if c.Changed {
			return " " + theme.ChangedDot() + " "
		}
	}
	return "   "
}

// lastRunCell renders a Component entity's last-run status (glyph + word).
func (m CatalogModel) lastRunCell(e services.EntitySummary) string {
	c, ok := m.componentInfo(e)
	if !ok || c.LastRunStatus == "" {
		return "—"
	}
	return theme.StatusGlyph(c.LastRunStatus) + " " + c.LastRunStatus
}

// columnsForKind picks the table shape per kind so each tab leads with the
// fields that matter for that kind. Component columns are responsive: the
// envelope columns (stage, type) yield to the work-surface columns (last run,
// change mark) as the stage narrows.
func (m CatalogModel) columnsForKind(kind string, width int) []catalogColumn {
	name := catalogColumn{"NAME", clamp(width*30/100, 14, 40),
		func(e services.EntitySummary) string { return e.Name }}
	owner := catalogColumn{"OWNER", clamp(width*20/100, 10, 26),
		func(e services.EntitySummary) string { return zoa(e.Owner) }}
	members := catalogColumn{"MEMBERS", 7,
		func(e services.EntitySummary) string {
			if e.MemberCount == 0 {
				return "—"
			}
			return fmt.Sprintf("%d", e.MemberCount)
		}}
	last := catalogColumn{"LAST", 12, m.lastRunCell}
	chg := catalogColumn{"CHG", 3, m.changeMark}
	switch kind {
	case "All":
		return []catalogColumn{
			{"KIND", 12, func(e services.EntitySummary) string { return e.Kind }},
			name,
			{"DETAIL", clamp(width*26/100, 12, 40), func(e services.EntitySummary) string {
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
			chg,
		}
	case "Component":
		cols := []catalogColumn{name, owner}
		if width >= 110 {
			cols = append(cols,
				catalogColumn{"STAGE", 10, func(e services.EntitySummary) string { return zoa(e.Stage) }})
		}
		if width >= 84 {
			cols = append(cols,
				catalogColumn{"TYPE", clamp(width*14/100, 8, 20), func(e services.EntitySummary) string { return zoa(e.Type) }})
		}
		return append(cols, last, chg)
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
	// Work-surface context for components: live change overlay + last run.
	if c, ok := m.componentInfo(e); ok {
		if c.ChangeKind != "" {
			field("change", c.ChangeKind)
		}
		if c.LastRunStatus != "" {
			b.WriteString("  " + theme.StyleLabel.Render(pad("last run", 11)) +
				theme.StatusGlyph(c.LastRunStatus) + " " +
				theme.StyleValue.Render(c.LastRunStatus) + "\n")
		}
	}

	links := m.detailLinks()
	runs := m.detailExecutions()
	if len(links) == 0 && len(runs) == 0 {
		b.WriteString("\n")
		b.WriteString(theme.StyleDim.Render("  no members, relations, or executions"))
		b.WriteString("\n")
	}

	// Budget the remaining stage height across the two navigable sections;
	// the section holding the cursor gets the larger share so the selected
	// row is always on screen.
	remaining := m.Height - strings.Count(b.String(), "\n") - 8
	if remaining < 6 {
		remaining = 6
	}
	connWindow := remaining / 2
	if len(runs) == 0 {
		connWindow = remaining
	}
	if connWindow > len(links) {
		connWindow = len(links)
	}
	execWindow := remaining - connWindow
	if execWindow > len(runs) {
		execWindow = len(runs)
	}

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

		// The unified detail cursor spans links then runs; this section's
		// local cursor is only live while it points inside the links range.
		connCursor := 0
		if m.detailCursor < len(links) {
			connCursor = m.detailCursor
		}
		start, end := viewportWindow(connCursor, len(links), connWindow)
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
	}

	if len(runs) > 0 {
		b.WriteString("\n")
		b.WriteString(theme.StyleSectionTitle.Render(fmt.Sprintf("EXECUTIONS (%d)", len(runs))))
		b.WriteString("\n")

		execCursor := 0
		if m.detailCursor >= len(links) {
			execCursor = m.detailCursor - len(links)
		}
		start, end := viewportWindow(execCursor, len(runs), execWindow)
		for i := start; i < end; i++ {
			line := " " + executionRow(runs[i])
			if m.detailCursor == len(links)+i {
				b.WriteString(theme.StyleCursorBar.Render("▌") + theme.StyleTableRowSelected.Render(line))
			} else {
				b.WriteString(" " + theme.StyleTableRow.Render(line))
			}
			b.WriteString("\n")
		}
		if end < len(runs) {
			b.WriteString(theme.StyleDim.Render(fmt.Sprintf(" … %d more", len(runs)-end)))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	hints := "enter follow · esc back"
	if e.Kind == "Component" {
		hints = "enter follow/open run · r run · g compose · o page · esc back"
	}
	b.WriteString(theme.StyleDim.Render(hints))
	return b.String()
}

// executionRow formats one recent execution: status glyph, short exec id,
// status, duration, trigger, and a dry-run marker.
func executionRow(r services.RunSummary) string {
	id := r.ExecID
	if len(id) > 10 {
		id = id[:10]
	}
	dur := "—"
	if r.Duration > 0 {
		dur = r.Duration.Round(time.Second).String()
	}
	line := fmt.Sprintf("%s %s  %s  %s",
		theme.StatusGlyph(r.Status), pad(id, 10), pad(r.Status, 9), pad(dur, 7))
	if r.Trigger != "" {
		line += "  " + theme.StyleDim.Render(r.Trigger)
	}
	if r.DryRun {
		line += "  " + theme.StyleChipDim.Render("dry-run")
	}
	return line
}
