package views

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/tui/theme"
)

// DAGNode is the renderer-facing shape: identity + display + dependencies.
type DAGNode struct {
	ID          string
	Label       string
	Status      string // running | completed | failed | waiting | "" (idle)
	DependsOn   []string
	Environment string // optional — used by RenderDAG to group rows into env lanes
}

// PlanToDAGNodes flattens a plan into renderer-ready nodes. The label is
// the component name when present (with the env stripped — RenderDAG
// surfaces env via section headers, not per-row noise).
func PlanToDAGNodes(plan *model.Plan, statuses map[string]string) []DAGNode {
	if plan == nil {
		return nil
	}
	out := make([]DAGNode, 0, len(plan.Jobs))
	for _, j := range plan.Jobs {
		label := j.ID
		if j.Component != "" {
			label = j.Component
		}
		st := statuses[j.ID]
		if st == "" {
			st = "waiting"
		}
		out = append(out, DAGNode{
			ID:          j.ID,
			Label:       label,
			Status:      st,
			DependsOn:   append([]string(nil), j.DependsOn...),
			Environment: j.Environment,
		})
	}
	return out
}

// RenderDAG renders a clean, hierarchical view of the plan's job graph.
//
// Layout rules (the cleanup pass — less cluttered than v1):
//
//   - Jobs are grouped by environment when more than one env is present.
//     Each env becomes a subtle header chip; jobs flow underneath.
//   - Inside an env, jobs render as a tree rooted at dependency-less nodes.
//     A child appears under its first parent (subsequent parents surface
//     in the row's dim "← parent[, parent…]" suffix instead of duplicating
//     the row).
//   - The trailing job-ID echo from v1 is gone — the label IS the identity
//     for everything visual; IDs only matter when the same component runs
//     in two envs, and the env section header already disambiguates that.
//   - The selected row gets a full-height accent bar at column 0 plus a
//     brighter label; everything else stays restful.
//   - Status is a 1-glyph badge at a fixed column so the eye can scan
//     down a single "what's running" stripe.
func RenderDAG(nodes []DAGNode, selectedID string, width int) string {
	if len(nodes) == 0 {
		return theme.StyleDim.Render("  (no jobs yet)")
	}
	if width < 12 {
		width = 12
	}

	// Bucket by environment, preserving first-seen order so the section
	// list matches the plan's topological ordering.
	type envBucket struct {
		env   string
		nodes []DAGNode
	}
	var (
		buckets   []envBucket
		bucketIdx = map[string]int{}
	)
	for _, n := range nodes {
		env := n.Environment
		if env == "" {
			env = "—"
		}
		if i, ok := bucketIdx[env]; ok {
			buckets[i].nodes = append(buckets[i].nodes, n)
			continue
		}
		bucketIdx[env] = len(buckets)
		buckets = append(buckets, envBucket{env: env, nodes: []DAGNode{n}})
	}
	multiEnv := len(buckets) > 1

	var b strings.Builder
	for bi, bk := range buckets {
		if multiEnv {
			if bi > 0 {
				b.WriteString("\n")
			}
			b.WriteString("  " + theme.StyleChipDim.Render(" env · "+bk.env) + "\n")
		}
		renderBucket(&b, bk.nodes, selectedID, width, multiEnv)
	}
	return b.String()
}

func renderBucket(b *strings.Builder, nodes []DAGNode, selectedID string, width int, indent bool) {
	byID := make(map[string]*DAGNode, len(nodes))
	for i := range nodes {
		byID[nodes[i].ID] = &nodes[i]
	}
	children := make(map[string][]string)
	seenChild := map[string]bool{}
	var roots []string
	for _, n := range nodes {
		attached := false
		// Find the first dep that lives in this bucket — that's the
		// visual parent. Cross-bucket deps surface as " ← parent" suffix.
		for _, p := range n.DependsOn {
			if _, ok := byID[p]; ok {
				if !seenChild[n.ID] {
					children[p] = append(children[p], n.ID)
					seenChild[n.ID] = true
				}
				attached = true
				break
			}
		}
		if !attached {
			roots = append(roots, n.ID)
		}
	}
	for k := range children {
		sort.Strings(children[k])
	}
	sort.Strings(roots)

	leftPad := ""
	if indent {
		leftPad = "  "
	}
	visited := map[string]bool{}
	for i, r := range roots {
		last := i == len(roots)-1
		drawNode(b, byID, children, r, leftPad, "", last, true, selectedID, visited, width)
	}
}

func drawNode(
	b *strings.Builder,
	byID map[string]*DAGNode,
	kids map[string][]string,
	id, leftPad, prefix string,
	last, root bool,
	selected string,
	visited map[string]bool,
	width int,
) {
	n := byID[id]
	if n == nil {
		return
	}
	cycle := visited[id]
	visited[id] = true

	var connector string
	switch {
	case root:
		connector = ""
	case last:
		connector = "╰─ "
	default:
		connector = "├─ "
	}

	isSelected := id == selected
	cursor := "  "
	if isSelected {
		cursor = theme.StyleCursorBar.Render("▌") + " "
	}

	badge := dagStatusIcon(n.Status)
	label := n.Label
	if cycle {
		label += theme.StyleDim.Render(" ↺")
	}
	labelStyled := dagLabelStyle(n.Status, isSelected).Render(label)

	// Show secondary parents (not the visual one — that's already in the
	// tree) as a dim suffix so the dependency relationship is recoverable
	// without doubling rows.
	var secondaryParents []string
	for i, p := range n.DependsOn {
		if i == 0 {
			continue
		}
		secondaryParents = append(secondaryParents, p)
	}
	depHint := ""
	if len(secondaryParents) > 0 {
		depHint = "  " + theme.StyleDim.Render("← "+strings.Join(secondaryParents, ", "))
	}

	line := cursor + leftPad + theme.StyleDim.Render(prefix+connector) + badge + " " + labelStyled + depHint
	b.WriteString(line + "\n")

	if cycle {
		return
	}
	cs := kids[id]
	childPrefix := prefix
	if !root {
		if last {
			childPrefix += "   "
		} else {
			childPrefix += "│  "
		}
	}
	for i, c := range cs {
		drawNode(b, byID, kids, c, leftPad, childPrefix, i == len(cs)-1, false, selected, visited, width)
	}
}

func dagStatusIcon(s string) string {
	switch s {
	case "running":
		return theme.StylePillRunning.Render(pulseGlyph())
	case "completed", "done", "success":
		return theme.StylePillSuccess.Render("●")
	case "failed", "error":
		return theme.StylePillError.Render("✗")
	case "waiting":
		return theme.StyleDim.Render("○")
	}
	return theme.StyleDim.Render("○")
}

// pulseGlyph returns a braille spinner frame that advances every 120ms of wall
// clock. Braille glyphs (U+2800 block) are East-Asian-Narrow, so they are
// always one terminal cell wide — unlike the circle-quadrant glyphs (◐◓◑◒)
// which are ambiguous-width and render as TWO cells in many terminals. A
// mis-measured live glyph makes its row one column too wide, wraps in the
// terminal, and desyncs the renderer's cursor — leaving ghosted rows. Keeping
// every animated glyph single-width is what keeps the live view stable.
func pulseGlyph() string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	idx := (time.Now().UnixMilli() / 120) % int64(len(frames))
	return frames[idx]
}

func dagLabelStyle(status string, selected bool) interface{ Render(...string) string } {
	if selected {
		return theme.StyleValue
	}
	switch status {
	case "running":
		return theme.StylePillRunning
	case "completed", "done", "success":
		return theme.StylePillSuccess
	case "failed", "error":
		return theme.StylePillError
	}
	return theme.StyleLabel
}

// DAGSummary returns a one-line status summary suitable for a header.
func DAGSummary(nodes []DAGNode) string {
	var done, running, failed, waiting int
	for _, n := range nodes {
		switch n.Status {
		case "completed", "done", "success":
			done++
		case "running":
			running++
		case "failed", "error":
			failed++
		default:
			waiting++
		}
	}
	parts := []string{
		theme.StyleDim.Render(fmt.Sprintf("%d jobs", len(nodes))),
	}
	if running > 0 {
		parts = append(parts, theme.StylePillRunning.Render(fmt.Sprintf("%d running", running)))
	}
	if done > 0 {
		parts = append(parts, theme.StylePillSuccess.Render(fmt.Sprintf("%d done", done)))
	}
	if failed > 0 {
		parts = append(parts, theme.StylePillError.Render(fmt.Sprintf("%d failed", failed)))
	}
	if waiting > 0 {
		parts = append(parts, theme.StyleDim.Render(fmt.Sprintf("%d waiting", waiting)))
	}
	return strings.Join(parts, "  ")
}
