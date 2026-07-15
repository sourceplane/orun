package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/agent/attach"
	"github.com/sourceplane/orun/internal/tui/services"
	"github.com/sourceplane/orun/internal/tui/theme"
)

// AgentModel is the cockpit's Agent surface, elevated to an interactive head
// (orun-agents-live AL3). Not attached, it browses the workspace's live
// sessions and git-authored agent types. Attached to a session, it IS the
// desktop-app experience: a streaming conversation, collapsible tool cards,
// sticky approval cards, an activity line, and an always-on composer — the
// same attach protocol the CLI and cloud console speak, so the transport
// (in-process, local socket, cloud relay) is invisible here.
type AgentModel struct {
	// Sidebar: live sessions then agent types.
	Sessions []services.LiveSessionRow
	Types    []services.AgentTypeRow
	Cursor   int
	Filter   string
	Width    int
	Height   int

	// Attached conversation state (nil head ⇒ browsing).
	head       AgentHead
	frames     <-chan attach.Frame
	info       attach.Frame // the hello frame
	live       bool
	items      []convItem
	pending    []approval // unresolved approval requests, sticky
	activity   string     // the live activity line (current delta/turn)
	deltaBuf   strings.Builder
	composer   textinput.Model
	composerOn bool
	status     string // transient status ("queued", "detached", errors)
}

// AgentHead is the seam the conversation drives: the local SocketClient and a
// future remote client both satisfy it, so the TUI head is one render path
// over any transport (design §2 — interchangeability is the absence of
// transport-specific behavior).
type AgentHead interface {
	Frames() <-chan attach.Frame
	Steer(text string) error
	Verdict(requestID string, approved bool, reason string) error
	Interrupt() error
	End() error
	Detach()
}

type convItemKind int

const (
	itemAgent convItemKind = iota
	itemUser
	itemTool
	itemNote // state/harness/artifact/error/cost lines
)

type convItem struct {
	kind      convItemKind
	text      string
	principal string
	detail    string // tool decision, artifact url, etc.
}

type approval struct {
	requestID string
	tool      string
}

// AgentFrameMsg wraps one attach frame for the tea loop. Closed marks the head
// feed as ended (the frame is zero).
type AgentFrameMsg struct {
	Frame  attach.Frame
	Closed bool
}

// WaitForFrame blocks for the next attach frame, mirroring the run-view stream
// pattern. A closed feed yields AgentFrameMsg{Closed: true}.
func WaitForFrame(ch <-chan attach.Frame) tea.Cmd {
	return func() tea.Msg {
		f, ok := <-ch
		if !ok {
			return AgentFrameMsg{Closed: true}
		}
		return AgentFrameMsg{Frame: f}
	}
}

// NewAgentModel builds the Agent surface from the loaded agent types.
func NewAgentModel(types []services.AgentTypeRow) AgentModel {
	ti := textinput.New()
	ti.Placeholder = "message the agent…  (enter steer · esc interrupt · ctrl+d detach)"
	ti.Prompt = "› "
	ti.CharLimit = 4000
	return AgentModel{Types: types, composer: ti}
}

func (m AgentModel) Init() tea.Cmd { return nil }

// Attached reports whether a session head is attached.
func (m AgentModel) Attached() bool { return m.head != nil }

// SetFilter narrows the type list by name/harness/owner substring.
func (m AgentModel) SetFilter(f string) AgentModel {
	m.Filter = f
	m.Cursor = 0
	return m
}

// Attach connects the surface to a live session head and starts streaming.
func (m AgentModel) Attach(head AgentHead) (AgentModel, tea.Cmd) {
	m.head = head
	m.frames = head.Frames()
	m.items = nil
	m.pending = nil
	m.activity = ""
	m.live = false
	m.composerOn = true
	m.composer.Focus()
	m.status = ""
	return m, WaitForFrame(m.frames)
}

// Detach leaves the session (it keeps running) and returns to browsing.
func (m AgentModel) Detach() AgentModel {
	if m.head != nil {
		m.head.Detach()
	}
	m.head = nil
	m.frames = nil
	m.composerOn = false
	m.composer.Blur()
	m.status = "detached"
	return m
}

// ComposerFocused reports whether the composer is capturing text input — the
// root model routes printable keys here and withholds them from mode keys.
func (m AgentModel) ComposerFocused() bool { return m.composerOn && m.composer.Focused() }

// Selected returns the highlighted agent type when the cursor is over the
// types section, or nil.
func (m AgentModel) Selected() *services.AgentTypeRow {
	rows := m.filteredTypes()
	idx := m.Cursor - len(m.Sessions)
	if idx < 0 || idx >= len(rows) {
		return nil
	}
	return &rows[idx]
}

// SelectedSession returns the highlighted live session, or nil.
func (m AgentModel) SelectedSession() *services.LiveSessionRow {
	if m.Cursor < 0 || m.Cursor >= len(m.Sessions) {
		return nil
	}
	return &m.Sessions[m.Cursor]
}

func (m AgentModel) filteredTypes() []services.AgentTypeRow {
	if m.Filter == "" {
		return m.Types
	}
	f := strings.ToLower(m.Filter)
	var out []services.AgentTypeRow
	for _, t := range m.Types {
		if strings.Contains(strings.ToLower(t.Name), f) ||
			strings.Contains(strings.ToLower(t.Harness), f) ||
			strings.Contains(strings.ToLower(t.Owner), f) {
			out = append(out, t)
		}
	}
	return out
}

func (m AgentModel) rowCount() int { return len(m.Sessions) + len(m.filteredTypes()) }

// RowCount is the sidebar row count (sessions + filtered types) — the root
// model clamps the cursor against it after a data load.
func (m AgentModel) RowCount() int { return m.rowCount() }

// --- test-only accessors (the model-level head integration test) ---

// FramesForTest exposes the head feed so a test can drive the stream.
func (m AgentModel) FramesForTest() <-chan attach.Frame { return m.frames }

// LiveForTest reports whether the live marker has been folded.
func (m AgentModel) LiveForTest() bool { return m.live }

// PendingCountForTest reports the number of unresolved approvals.
func (m AgentModel) PendingCountForTest() int { return len(m.pending) }

// SetComposerForTest sets the composer text (as if the user typed it).
func (m AgentModel) SetComposerForTest(text string) AgentModel {
	m.composer.SetValue(text)
	return m
}

func (m AgentModel) Update(msg tea.Msg) (AgentModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)
	case AgentFrameMsg:
		if msg.Closed {
			m.live = false
			m.composerOn = false
			m.composer.Blur()
			if m.status == "" {
				m.status = "session ended"
			}
			m.head = nil
			m.frames = nil
			return m, nil
		}
		m = m.foldFrame(msg.Frame)
		if m.frames != nil {
			return m, WaitForFrame(m.frames)
		}
	}
	return m, nil
}

func (m AgentModel) handleKey(msg tea.KeyMsg) (AgentModel, tea.Cmd) {
	// Attached: the composer owns text; a few control keys are reserved.
	if m.composerOn {
		switch msg.String() {
		case "enter":
			text := strings.TrimSpace(m.composer.Value())
			if text == "" {
				return m, nil
			}
			m.composer.Reset()
			// A pending approval answered by number shorthand: "y"/"n" are
			// composer text, so approvals use explicit keys below; here we
			// always steer.
			if m.head != nil {
				if err := m.head.Steer(text); err != nil {
					m.status = err.Error()
				} else {
					m.status = "queued"
				}
			}
			return m, nil
		case "esc":
			if m.head != nil {
				_ = m.head.Interrupt()
				m.status = "interrupted"
			}
			return m, nil
		case "ctrl+d":
			return m.Detach(), nil
		case "ctrl+y":
			return m.answerTopApproval(true), nil
		case "ctrl+n":
			return m.answerTopApproval(false), nil
		}
		var cmd tea.Cmd
		m.composer, cmd = m.composer.Update(msg)
		return m, cmd
	}
	// Browsing: navigate the sidebar.
	switch msg.String() {
	case "up", "k":
		if m.Cursor > 0 {
			m.Cursor--
		}
	case "down", "j":
		if m.Cursor < m.rowCount()-1 {
			m.Cursor++
		}
	}
	return m, nil
}

// answerTopApproval resolves the oldest pending approval; the ctrl+y/ctrl+n
// keys make an approval impossible to miss while the composer holds focus.
func (m AgentModel) answerTopApproval(approved bool) AgentModel {
	if len(m.pending) == 0 || m.head == nil {
		return m
	}
	top := m.pending[0]
	reason := ""
	if err := m.head.Verdict(top.requestID, approved, reason); err != nil {
		m.status = err.Error()
	} else if approved {
		m.status = "approved " + top.tool
	} else {
		m.status = "denied " + top.tool
	}
	return m
}

func (m AgentModel) foldFrame(f attach.Frame) AgentModel {
	switch f.T {
	case attach.THello:
		m.info = f
	case attach.TLive:
		m.live = true
	case attach.TDelta:
		m.deltaBuf.WriteString(f.Text)
		m.activity = m.deltaBuf.String()
	case attach.TEvent:
		m = m.foldEvent(f.Kind, f.Payload)
	case attach.TPresence:
		// advisory; rendered from f.Heads on demand — nothing to store
	case attach.TBye:
		m.live = false
		m.status = "detached (" + f.Reason + ")"
	case attach.TError:
		m.items = append(m.items, convItem{kind: itemNote, text: "protocol error: " + f.Code + " " + f.Message})
	}
	return m
}

func (m AgentModel) foldEvent(kind string, payload map[string]any) AgentModel {
	str := func(k string) string { s, _ := payload[k].(string); return s }
	switch kind {
	case "message_agent":
		m.activity = ""
		m.deltaBuf.Reset()
		m.items = append(m.items, convItem{kind: itemAgent, text: str("text")})
	case "message_user":
		m.items = append(m.items, convItem{kind: itemUser, text: str("text"), principal: str("principal")})
	case "tool_call":
		m.items = append(m.items, convItem{kind: itemTool, text: str("tool"), detail: str("decision")})
	case "tool_result":
		m.activity = ""
	case "approval_requested":
		req := str("requestId")
		m.pending = append(m.pending, approval{requestID: req, tool: str("tool")})
		m.items = append(m.items, convItem{kind: itemNote, text: "approval needed: " + str("tool"), detail: req})
	case "approval_resolved":
		req := str("requestId")
		m.pending = removeApproval(m.pending, req)
		verdict := "denied"
		if b, _ := payload["approved"].(bool); b {
			verdict = "approved"
		}
		m.items = append(m.items, convItem{kind: itemNote,
			text: fmt.Sprintf("approval %s %s by %s", req, verdict, orDashView(str("principal")))})
	case "artifact_produced":
		pr, _ := payload["pr"].(string)
		m.items = append(m.items, convItem{kind: itemNote, text: "artifact", detail: pr})
	case "cost_sample":
		m.activity = fmt.Sprintf("· %v tokens", payload["tokens"])
	case "state_changed":
		st := str("state")
		m.items = append(m.items, convItem{kind: itemNote, text: "state: " + st})
		if st != "running" {
			m.live = false
		}
	case "harness_event":
		if phase := str("phase"); phase != "" {
			m.items = append(m.items, convItem{kind: itemNote, text: "harness: " + phase})
		}
	case "error":
		m.items = append(m.items, convItem{kind: itemNote, text: "error: " + str("text")})
	}
	return m
}

func removeApproval(list []approval, requestID string) []approval {
	out := list[:0]
	for _, a := range list {
		if a.requestID != requestID {
			out = append(out, a)
		}
	}
	return out
}

func (m AgentModel) View() string {
	if m.head != nil || len(m.items) > 0 {
		return m.viewConversation()
	}
	return m.viewBrowse()
}

func (m AgentModel) viewBrowse() string {
	var left strings.Builder
	left.WriteString(theme.StyleSectionTitle.Render("Sessions"))
	left.WriteByte('\n')
	left.WriteString(theme.StyleAccent.Render("  + New session") + theme.StyleMuted.Render("  n") + "\n")
	if len(m.Sessions) == 0 {
		left.WriteString(theme.StyleMuted.Render("  no live sessions yet") + "\n")
	}
	for i, s := range m.Sessions {
		marker := "  "
		label := s.SessionID
		if s.Task != "" {
			label = s.Task + " · " + s.SessionID
		}
		if i == m.Cursor {
			marker = theme.StyleAccent.Render("▸ ")
			label = theme.StyleAccent.Render(label)
		}
		fmt.Fprintf(&left, "%s%s\n", marker, label)
		meta := theme.StyleMuted.Render("    " + statusGlyph(s.State) + " " + s.State)
		if b := driverBadge(s.Driver); b != "" {
			meta += " " + b
		}
		left.WriteString(meta + "\n")
	}
	left.WriteByte('\n')
	left.WriteString(theme.StyleSectionTitle.Render("Agent types"))
	left.WriteByte('\n')
	rows := m.filteredTypes()
	if len(rows) == 0 {
		left.WriteString(theme.StyleMuted.Render("  none — author agents/<name>.md, then `orun agent import`") + "\n")
	}
	for i, t := range rows {
		marker := "  "
		name := t.Name
		if i+len(m.Sessions) == m.Cursor {
			marker = theme.StyleAccent.Render("▸ ")
			name = theme.StyleAccent.Render(name)
		}
		fmt.Fprintf(&left, "%s%s\n", marker, name)
		left.WriteString(theme.StyleMuted.Render("    "+t.Harness+" · "+t.Model) + "\n")
	}

	var right strings.Builder
	if s := m.SelectedSession(); s != nil {
		right.WriteString(theme.StyleSectionTitle.Render("Session "+s.SessionID) + "\n")
		fmt.Fprintf(&right, "state    %s\n", s.State)
		fmt.Fprintf(&right, "type     %s\n", orDashView(s.AgentType))
		fmt.Fprintf(&right, "task     %s\n", orDashView(s.Task))
		fmt.Fprintf(&right, "driver   %s\n", orDashView(s.Driver))
		right.WriteByte('\n')
		right.WriteString(theme.StyleAccent.Render("enter to attach") + "\n")
	} else if sel := m.Selected(); sel != nil {
		right.WriteString(theme.StyleSectionTitle.Render(sel.Name) + "\n")
		fmt.Fprintf(&right, "harness   %s\n", sel.Harness)
		fmt.Fprintf(&right, "model     %s\n", sel.Model)
		fmt.Fprintf(&right, "owner     %s\n", orDashView(sel.Owner))
		if sel.Autonomy != "" {
			fmt.Fprintf(&right, "autonomy  %s\n", sel.Autonomy)
		}
		if len(sel.MayAffect) > 0 {
			fmt.Fprintf(&right, "affects   %s\n", strings.Join(sel.MayAffect, ", "))
		}
		if p := strings.TrimSpace(sel.Persona); p != "" {
			right.WriteByte('\n')
			right.WriteString(theme.StyleMuted.Render(firstLines(p, 6)) + "\n")
		}
	}
	if m.status != "" {
		right.WriteString("\n" + theme.StyleMuted.Render(m.status) + "\n")
	}
	return joinColumns(left.String(), right.String(), m.Width)
}

func (m AgentModel) viewConversation() string {
	var b strings.Builder
	title := "Session"
	if m.info.SessionID != "" {
		title = "Session " + m.info.SessionID
	}
	head := theme.StyleSectionTitle.Render(title)
	if b := driverBadge(m.info.Harness); b != "" {
		head += "  " + b
	}
	if m.live {
		head += "  " + theme.StyleAccent.Render("● live")
	}
	b.WriteString(head + "\n\n")

	for _, it := range m.items {
		b.WriteString(renderItem(it) + "\n")
	}
	if m.activity != "" {
		b.WriteString(theme.StyleMuted.Render("  "+m.activity+" …") + "\n")
	}

	// Sticky approval cards above the composer — impossible to scroll off.
	if len(m.pending) > 0 {
		b.WriteByte('\n')
		for _, a := range m.pending {
			b.WriteString(theme.StylePillWarn.Render(" approval ") +
				" " + theme.StyleAccent.Render(a.tool) +
				theme.StyleMuted.Render("  ctrl+y approve · ctrl+n deny  ("+a.requestID+")") + "\n")
		}
	}

	b.WriteByte('\n')
	if m.composerOn {
		b.WriteString(m.composer.View() + "\n")
	}
	if m.status != "" {
		b.WriteString(theme.StyleMuted.Render(m.status))
	}
	return b.String()
}

func renderItem(it convItem) string {
	switch it.kind {
	case itemAgent:
		return theme.StyleAccent.Render("agent") + "  " + it.text
	case itemUser:
		who := orDashView(it.principal)
		return theme.StyleSectionTitle.Render(who) + "  " + it.text
	case itemTool:
		glyph := "⚙"
		return theme.StyleMuted.Render("  " + glyph + " " + it.text + " (" + it.detail + ")")
	default:
		s := "  " + it.text
		if it.detail != "" {
			s += " " + it.detail
		}
		return theme.StyleMuted.Render(s)
	}
}

// driverBadge renders an honest at-a-glance marker for a session's driver:
// `claude` for a real delegated run, a `stub` warning pill so a deterministic
// fixture session is never mistaken for real work. An unknown driver is shown
// verbatim; an empty driver renders nothing.
func driverBadge(driver string) string {
	switch driver {
	case "":
		return ""
	case "claude-code":
		return theme.StyleChipAccent.Render(" claude ")
	case "stub":
		return theme.StylePillWarn.Render(" stub ")
	default:
		return theme.StyleDim.Render(" " + driver + " ")
	}
}

func statusGlyph(state string) string {
	switch state {
	case "running":
		return "●"
	case "completed":
		return "✓"
	case "failed", "canceled", "expired":
		return "✕"
	default:
		return "·"
	}
}

func orDashView(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = append(lines[:n], "…")
	}
	return strings.Join(lines, "\n")
}

// joinColumns lays two text blocks side by side with a gutter, falling back to
// stacked output on narrow terminals.
func joinColumns(left, right string, width int) string {
	if width < 80 || strings.TrimSpace(right) == "" {
		if strings.TrimSpace(right) == "" {
			return left
		}
		return left + "\n" + right
	}
	leftLines := strings.Split(left, "\n")
	rightLines := strings.Split(right, "\n")
	colW := 34
	n := len(leftLines)
	if len(rightLines) > n {
		n = len(rightLines)
	}
	var b strings.Builder
	for i := 0; i < n; i++ {
		l, r := "", ""
		if i < len(leftLines) {
			l = leftLines[i]
		}
		if i < len(rightLines) {
			r = rightLines[i]
		}
		pad := colW - lipglossWidth(l)
		if pad < 1 {
			pad = 1
		}
		b.WriteString(l)
		b.WriteString(strings.Repeat(" ", pad))
		b.WriteString(r)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// lipglossWidth is the visible width of a possibly-styled string.
func lipglossWidth(s string) int {
	w, inEsc := 0, false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEsc = true
		case inEsc && r == 'm':
			inEsc = false
		case inEsc:
			// skip
		default:
			w++
		}
	}
	return w
}
