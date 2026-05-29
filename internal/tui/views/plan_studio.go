package views

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/tui/services"

	"github.com/sourceplane/orun/internal/tui/theme"
)

// PlanStudioState enumerates the lifecycle states of the Plan Studio view.
//
//	Idle        — no plan generated yet, awaiting `g`.
//	Configuring — (reserved for Phase 2.1 form) currently aliased to Idle.
//	Generating  — async GeneratePlan in flight.
//	Review      — plan rendered, user can browse jobs / save.
//	Saved       — plan persisted via NamedPlan path.
//	Error       — last generate/save attempt failed; view shows the error.
//
// Transitions:
//
//	Idle/Review/Saved/Error -- g  --> Generating
//	Generating              -- ok --> Review
//	Generating              -- err --> Error
//	Review                  -- s  --> Saved (requires named plan)
//	any                     -- esc/clear --> Idle (only when not Generating)
type PlanStudioState int

const (
	PlanStudioIdle PlanStudioState = iota
	PlanStudioConfiguring
	PlanStudioGenerating
	PlanStudioReview
	PlanStudioSaved
	PlanStudioError
)

func (s PlanStudioState) String() string {
	switch s {
	case PlanStudioIdle:
		return "idle"
	case PlanStudioConfiguring:
		return "configuring"
	case PlanStudioGenerating:
		return "generating"
	case PlanStudioReview:
		return "review"
	case PlanStudioSaved:
		return "saved"
	case PlanStudioError:
		return "error"
	}
	return "unknown"
}

// PlanStudioKeyMap is the local keymap active when Plan Studio is the
// focused view.
type PlanStudioKeyMap struct {
	Generate    key.Binding
	Regenerate  key.Binding
	Save        key.Binding
	DryRun      key.Binding
	RealRun     key.Binding
	Up          key.Binding
	Down        key.Binding
	Clear       key.Binding
	CycleEnv    key.Binding
	CycleTrig   key.Binding
	ToggleScope key.Binding
	Back        key.Binding
}

// DefaultPlanStudioKeyMap returns the canonical Plan Studio bindings.
func DefaultPlanStudioKeyMap() PlanStudioKeyMap {
	return PlanStudioKeyMap{
		Generate:    key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "generate plan")),
		Regenerate:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "regenerate")),
		Save:        key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "save plan")),
		DryRun:      key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "dry-run")),
		RealRun:     key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "real run")),
		Up:          key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "prev job")),
		Down:        key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "next job")),
		Clear:       key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "clear plan")),
		CycleEnv:    key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "cycle env")),
		CycleTrig:   key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "cycle trigger")),
		ToggleScope: key.NewBinding(key.WithKeys("C"), key.WithHelp("C", "toggle changed")),
		Back:        key.NewBinding(key.WithKeys("esc", "backspace"), key.WithHelp("esc", "back")),
	}
}

// PlanStudioLevel enumerates the drilldown stack inside Plan Studio.
//
//	StudioLevelJobs  — flat list of jobs (default after review).
//	StudioLevelSteps — drilled into a job, viewing its step list.
//	StudioLevelStep  — drilled into a single step, viewing its detail body.
type PlanStudioLevel int

const (
	StudioLevelJobs PlanStudioLevel = iota
	StudioLevelSteps
	StudioLevelStep
)

// PlanStudioModel renders the Plan Studio view: a state machine that owns
// plan generation lifecycle, DAG preview, and save/persist actions.
//
// The model is intentionally injection-free for the request side: the
// root model assembles a services.PlanRequest and a tea.Cmd that calls
// svc.GeneratePlan, then routes the resulting services.PlanGeneratedMsg
// back through PlanStudioModel.Update.
type PlanStudioModel struct {
	State    PlanStudioState
	Request  services.PlanRequest
	Result   *services.PlanResult
	Err      error
	Cursor   int // job cursor at StudioLevelJobs / StudioLevelSteps
	Width    int
	Height   int
	keys     PlanStudioKeyMap
	saveName string // user-provided named-plan label (default: "tui-draft")

	// Drilldown
	Level      PlanStudioLevel
	stepCursor int // step cursor at StudioLevelSteps / StudioLevelStep
	jobCursor  int // remembers which job we drilled in from

	// Customization helpers — populated by the root model from the
	// WorkspaceSnapshot so the `e`/`t` keys can cycle through real
	// workspace values without poking back through services.
	Envs     []string
	Triggers []string
}

// NewPlanStudioModel constructs an empty Plan Studio model in the Idle state.
func NewPlanStudioModel() PlanStudioModel {
	return PlanStudioModel{
		State:    PlanStudioIdle,
		keys:     DefaultPlanStudioKeyMap(),
		saveName: "tui-draft",
	}
}

// KeyMap exposes the local key bindings so the root model can include them
// in the global help overlay when Plan Studio is focused.
func (m PlanStudioModel) KeyMap() PlanStudioKeyMap { return m.keys }

// SetRequest replaces the staged PlanRequest (e.g. when the workspace
// snapshot loads and we learn the intent file path).
func (m PlanStudioModel) SetRequest(req services.PlanRequest) PlanStudioModel {
	m.Request = req
	return m
}

// SetSaveName updates the label used for NamedPlan when the user triggers Save.
func (m PlanStudioModel) SetSaveName(name string) PlanStudioModel {
	if name != "" {
		m.saveName = name
	}
	return m
}

// SetSize stores the rendered content size so the review list can lay
// itself out against the actual main-pane width (preventing the wrap
// glitch that happened when the renderer defaulted to 80 cols).
func (m PlanStudioModel) SetSize(w, h int) PlanStudioModel {
	if w > 0 {
		m.Width = w
	}
	if h > 0 {
		m.Height = h
	}
	return m
}

// AtRoot reports whether we are at the top of the drilldown (jobs list).
// Used by the root model to decide whether Esc should pop a level or
// fall through to global navBack.
func (m PlanStudioModel) AtRoot() bool { return m.Level == StudioLevelJobs }

// Init satisfies tea.Model; nothing to load at view construction time.
func (m PlanStudioModel) Init() tea.Cmd { return nil }

// Update processes service messages and (when focused) local key input.
// Generation/save commands are returned for the root model to execute via
// the OrunService — this view never imports the service directly to make
// it trivially testable.
func (m PlanStudioModel) Update(msg tea.Msg) (PlanStudioModel, tea.Cmd) {
	switch msg := msg.(type) {

	case services.PlanGeneratedMsg:
		if msg.Err != nil {
			m.State = PlanStudioError
			m.Err = msg.Err
			return m, nil
		}
		m.State = PlanStudioReview
		m.Result = msg.Result
		m.Err = nil
		m.Cursor = 0
		return m, nil

	case services.ErrMsg:
		// Surfaces save failures from the root model.
		m.State = PlanStudioError
		m.Err = msg.Err
		return m, nil

	case tea.KeyMsg:
		// Block input while a generate is in flight to keep state
		// transitions deterministic.
		if m.State == PlanStudioGenerating {
			return m, nil
		}
		switch {
		case key.Matches(msg, m.keys.Generate), key.Matches(msg, m.keys.Regenerate):
			m.State = PlanStudioGenerating
			m.Err = nil
			return m, nil
		case key.Matches(msg, m.keys.Save):
			if m.State != PlanStudioReview || m.Result == nil {
				return m, nil
			}
			// Caller (root model) intercepts the marker and dispatches.
			return m, func() tea.Msg {
				return PlanStudioSaveRequestedMsg{Name: m.saveName}
			}
		case key.Matches(msg, m.keys.DryRun):
			if m.State != PlanStudioReview || m.Result == nil || m.Result.Plan == nil {
				return m, nil
			}
			return m, func() tea.Msg {
				return PlanStudioDryRunRequestedMsg{Plan: m.Result.Plan}
			}
		case key.Matches(msg, m.keys.RealRun):
			if m.State != PlanStudioReview || m.Result == nil || m.Result.Plan == nil {
				return m, nil
			}
			return m, func() tea.Msg {
				return PlanStudioRealRunRequestedMsg{Plan: m.Result.Plan}
			}
		case key.Matches(msg, m.keys.CycleEnv):
			if len(m.Envs) > 0 {
				m.Request.Environment = cycleString(m.Envs, m.Request.Environment)
			}
			return m, nil
		case key.Matches(msg, m.keys.CycleTrig):
			if len(m.Triggers) > 0 {
				m.Request.TriggerName = cycleString(m.Triggers, m.Request.TriggerName)
			}
			return m, nil
		case key.Matches(msg, m.keys.ToggleScope):
			m.Request.ChangedOnly = !m.Request.ChangedOnly
			return m, nil
		case key.Matches(msg, m.keys.Clear):
			if m.State == PlanStudioGenerating {
				return m, nil
			}
			m.State = PlanStudioIdle
			m.Result = nil
			m.Err = nil
			m.Cursor = 0
			m.Level = StudioLevelJobs
			m.stepCursor = 0
			m.jobCursor = 0
			return m, nil
		case key.Matches(msg, m.keys.Back):
			// Pop one drilldown level; root model decides what to do when
			// already at jobs (it will fall through to global navBack).
			switch m.Level {
			case StudioLevelStep:
				m.Level = StudioLevelSteps
				return m, nil
			case StudioLevelSteps:
				m.Level = StudioLevelJobs
				m.Cursor = m.jobCursor
				return m, nil
			}
			return m, nil
		case msg.String() == "enter":
			// Drill in: jobs → steps → step detail.
			if m.State != PlanStudioReview && m.State != PlanStudioSaved {
				return m, nil
			}
			switch m.Level {
			case StudioLevelJobs:
				if j := m.Selected(); j != nil && len(j.Steps) > 0 {
					m.jobCursor = m.Cursor
					m.Level = StudioLevelSteps
					m.stepCursor = 0
				}
				return m, nil
			case StudioLevelSteps:
				if m.SelectedStep() != nil {
					m.Level = StudioLevelStep
				}
				return m, nil
			}
			return m, nil
		case key.Matches(msg, m.keys.Down):
			switch m.Level {
			case StudioLevelJobs:
				if m.Result != nil && m.Cursor+1 < len(m.Result.Plan.Jobs) {
					m.Cursor++
				}
			case StudioLevelSteps:
				if j := m.jobAtJobCursor(); j != nil && m.stepCursor+1 < len(j.Steps) {
					m.stepCursor++
				}
			}
			return m, nil
		case key.Matches(msg, m.keys.Up):
			switch m.Level {
			case StudioLevelJobs:
				if m.Cursor > 0 {
					m.Cursor--
				}
			case StudioLevelSteps:
				if m.stepCursor > 0 {
					m.stepCursor--
				}
			}
			return m, nil
		}
	}
	return m, nil
}

// View renders the Compose surface for a component.
func (m PlanStudioModel) View() string {
	var b strings.Builder
	title := "Compose"
	if len(m.Request.Components) == 1 {
		title = "Component · " + m.Request.Components[0]
	}
	header := theme.StyleSectionTitle.Render(title) + "  " +
		theme.StyleChipAccent.Render(m.State.String())
	b.WriteString(header)
	b.WriteString("\n\n")

	// Form-style request summary
	b.WriteString(formRow("intent", emptyDash(m.Request.IntentFile)))
	b.WriteString(formRow("env", emptyDash(m.Request.Environment)))
	if m.Request.ChangedOnly {
		b.WriteString(formRow("scope",
			fmt.Sprintf("changed-only (base=%s head=%s)",
				emptyDash(m.Request.BaseBranch), emptyDash(m.Request.HeadRef))))
	} else {
		b.WriteString(formRow("scope", "all components"))
	}
	b.WriteString("\n")

	switch m.State {
	case PlanStudioIdle, PlanStudioConfiguring:
		b.WriteString(theme.StyleDim.Render("press `g` to generate a plan from the current intent.\n"))
	case PlanStudioGenerating:
		b.WriteString(theme.StyleAccent.Render("◐ generating plan…\n"))
	case PlanStudioError:
		fmt.Fprintf(&b, "%s %s\n",
			theme.StylePillError.Render("error:"), errString(m.Err))
		b.WriteString(theme.StyleDim.Render("press `g` to retry, `c` to clear.\n"))
	case PlanStudioReview, PlanStudioSaved:
		b.WriteString(m.renderReview())
	}
	return b.String()
}

func formRow(label, value string) string {
	return theme.StyleLabel.Render(fmt.Sprintf("%-8s", label)) + " : " +
		theme.StyleValue.Render(value) + "\n"
}

func emptyDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func (m PlanStudioModel) renderReview() string {
	if m.Result == nil || m.Result.Plan == nil {
		return "(no plan)\n"
	}
	switch m.Level {
	case StudioLevelSteps:
		return m.renderStepsList()
	case StudioLevelStep:
		return m.renderStepDetail()
	}
	return m.renderJobsList()
}

func (m PlanStudioModel) contentWidth() int {
	if m.Width > 0 {
		return m.Width
	}
	return 80
}

// renderJobsList — clean, github-actions-style flat list. No DAG art.
// Layout (single line per job):
//
//	▌ ●  job-id-as-in-github                            env  · component
//
// Status icon, full job ID (so users can copy/paste it), env chip,
// component as a dim suffix. Dependencies + steps live in the inspector.
func (m PlanStudioModel) renderJobsList() string {
	var b strings.Builder
	width := m.contentWidth()
	b.WriteString(m.renderHeader("Jobs"))

	// Build status map from PlanCheck statuses if we ever attach them;
	// today the studio is pre-execution so everything renders as "waiting".
	statuses := map[string]string{}
	for _, j := range m.Result.Plan.Jobs {
		statuses[j.ID] = "waiting"
	}
	b.WriteString(DAGSummary(PlanToDAGNodes(m.Result.Plan, statuses)) + "\n\n")

	rightPad := 2
	cursorCol := 2
	envColW := 14
	for i, j := range m.Result.Plan.Jobs {
		selected := i == m.Cursor
		cursor := strings.Repeat(" ", cursorCol)
		if selected {
			cursor = theme.StyleCursorBar.Render("▌") + " "
		}
		icon := dagStatusIcon(statuses[j.ID])
		env := j.Environment
		if env == "" {
			env = "—"
		}
		envChip := theme.StyleChipDim.Render(" " + truncateOneLineStudio(env, envColW-2) + " ")
		comp := ""
		if j.Component != "" {
			comp = "· " + j.Component
		}
		// Compute room for the name column = width - cursor(2) - icon(2) - env(envColW+2) - comp - padding
		nameCol := width - cursorCol - 2 - (envColW + 2) - len(comp) - rightPad
		if nameCol < 12 {
			nameCol = 12
		}
		name := j.ID
		nameStyled := dagLabelStyle(statuses[j.ID], selected).Render(padRight(name, nameCol))
		row := cursor + icon + " " + nameStyled + " " + envChip + " " + theme.StyleDim.Render(comp)

		if selected {
			b.WriteString(theme.StyleTableRowSelected.Render(row))
		} else if i%2 == 1 {
			b.WriteString(theme.StyleTableRowAlt.Render(row))
		} else {
			b.WriteString(theme.StyleTableRow.Render(row))
		}
		b.WriteString("\n")
	}
	b.WriteString(m.renderFooter())
	return b.String()
}

// renderStepsList — list of steps for the selected job (cursor drilled in
// from the jobs list via Enter).
func (m PlanStudioModel) renderStepsList() string {
	var b strings.Builder
	width := m.contentWidth()
	j := m.jobAtJobCursor()
	if j == nil {
		return "(no job)\n"
	}
	b.WriteString(m.renderHeader("Steps · " + j.ID))
	b.WriteString(theme.StyleDim.Render(fmt.Sprintf("%d steps", len(j.Steps))) + "\n\n")

	if len(j.Steps) == 0 {
		b.WriteString(theme.StyleDim.Render("  (no steps)"))
		b.WriteString(m.renderFooter())
		return b.String()
	}

	cursorCol := 2
	rightPad := 2
	phaseColW := 12
	for i, s := range j.Steps {
		selected := i == m.stepCursor
		cursor := strings.Repeat(" ", cursorCol)
		if selected {
			cursor = theme.StyleCursorBar.Render("▌") + " "
		}
		name := stepDisplayName(s, i)
		phase := s.Phase
		if phase == "" {
			phase = "—"
		}
		phaseChip := theme.StyleChipDim.Render(" " + truncateOneLineStudio(phase, phaseColW-2) + " ")

		// Capability hint = use:NAME or run:firstline
		cap := ""
		switch {
		case s.Use != "":
			cap = "use " + s.Use
		case s.Run != "":
			cap = "run"
		}
		nameCol := width - cursorCol - 2 - (phaseColW + 2) - len(cap) - rightPad
		if nameCol < 12 {
			nameCol = 12
		}
		nameStyled := theme.StyleValue.Render(padRight(name, nameCol))
		if !selected {
			nameStyled = theme.StyleLabel.Render(padRight(name, nameCol))
		}
		icon := theme.StyleDim.Render("•")
		row := cursor + icon + " " + nameStyled + " " + phaseChip + " " + theme.StyleDim.Render(cap)
		if selected {
			b.WriteString(theme.StyleTableRowSelected.Render(row))
		} else if i%2 == 1 {
			b.WriteString(theme.StyleTableRowAlt.Render(row))
		} else {
			b.WriteString(theme.StyleTableRow.Render(row))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n" + theme.StyleDim.Render("↑/↓ select  •  ⏎ open step  •  esc back to jobs"))
	return b.String()
}

// renderStepDetail — the deepest level: a single step's full body.
func (m PlanStudioModel) renderStepDetail() string {
	var b strings.Builder
	j := m.jobAtJobCursor()
	s := m.SelectedStep()
	if j == nil || s == nil {
		return "(no step)\n"
	}
	name := stepDisplayName(*s, m.stepCursor)
	b.WriteString(m.renderHeader("Step · " + name))
	b.WriteString(theme.StyleDim.Render("job: "+j.ID) + "\n\n")

	if s.Phase != "" {
		b.WriteString(formRow("phase", s.Phase))
	}
	if s.Use != "" {
		b.WriteString(formRow("use", s.Use))
	}
	if s.Shell != "" {
		b.WriteString(formRow("shell", s.Shell))
	}
	if s.WorkingDirectory != "" {
		b.WriteString(formRow("workdir", s.WorkingDirectory))
	}
	if s.Timeout != "" {
		b.WriteString(formRow("timeout", s.Timeout))
	}
	if s.Retry > 0 {
		b.WriteString(formRow("retry", fmt.Sprintf("%d", s.Retry)))
	}
	if s.Run != "" {
		b.WriteString("\n" + theme.StyleLabel.Render("run") + ":\n")
		b.WriteString(indentBlock(s.Run, "  "))
		b.WriteString("\n")
	}
	if len(s.With) > 0 {
		b.WriteString("\n" + theme.StyleLabel.Render("with") + ":\n")
		for _, k := range sortedKeys(s.With) {
			b.WriteString(fmt.Sprintf("  %s: %v\n", k, s.With[k]))
		}
	}
	b.WriteString("\n" + theme.StyleDim.Render("esc · back to steps"))
	return b.String()
}

func (m PlanStudioModel) renderHeader(label string) string {
	var b strings.Builder
	b.WriteString(theme.StyleSectionTitle.Render(label))
	b.WriteString("   " + theme.StyleDim.Render(fmt.Sprintf(
		"checksum %s · %d jobs · %d components",
		m.Result.Checksum, m.Result.JobCount, len(m.Result.Components))))
	b.WriteString("\n")
	if len(m.Result.Warnings) > 0 {
		for _, w := range m.Result.Warnings {
			b.WriteString(theme.StyleDim.Render("warn: "+w) + "\n")
		}
	}
	b.WriteString("\n")
	return b.String()
}

func (m PlanStudioModel) renderFooter() string {
	if m.State == PlanStudioSaved {
		return "\n" + theme.StyleDim.Render(fmt.Sprintf("saved as: %s", m.saveName))
	}
	return "\n" + theme.StyleDim.Render(fmt.Sprintf(
		"↑/↓ select  •  ⏎ open job  •  s save as %q  •  d dry-run  •  R real run  •  g regenerate  •  c clear",
		m.saveName))
}

// padRight pads or truncates s to exactly w runes (best-effort byte-based;
// the studio is ascii-heavy so this is acceptable).
func padRight(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if len(s) > w {
		if w == 1 {
			return "…"
		}
		return s[:w-1] + "…"
	}
	return s + strings.Repeat(" ", w-len(s))
}

// truncateOneLineStudio is a local sibling of the model.go helper used in
// the inspector — we don't import the root model from the view, so the
// renderer keeps its own trimmer.
func truncateOneLineStudio(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if max > 0 && len(s) > max {
		if max <= 1 {
			return "…"
		}
		return s[:max-1] + "…"
	}
	return s
}

func indentBlock(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = prefix + ln
	}
	return strings.Join(lines, "\n")
}

func sortedKeys(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func shortComp(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// MarkSaved transitions a Review-state model to Saved once the root model
// confirms persistence succeeded.
func (m PlanStudioModel) MarkSaved() PlanStudioModel {
	if m.State == PlanStudioReview {
		m.State = PlanStudioSaved
		m.Err = nil
	}
	return m
}

// MarkGenerating is used by the root model to force the Generating state
// when it dispatches the GeneratePlan command — keeps state transitions
// observable in tests without exporting the field.
func (m PlanStudioModel) MarkGenerating() PlanStudioModel {
	m.State = PlanStudioGenerating
	m.Err = nil
	return m
}

// --- Messages dispatched by the view ---

// PlanStudioSaveRequestedMsg signals that the user pressed `s` and the
// root model should call svc.GeneratePlan with NamedPlan=Name (or invoke
// store.SavePlan directly on the current result).
type PlanStudioSaveRequestedMsg struct {
	Name string
}

// PlanStudioDryRunRequestedMsg signals that the user pressed `d` from
// Review. The root model is responsible for calling
// svc.RunPlan(ctx, RunRequest{Plan: Plan, DryRun: true}) and, on
// success, installing the returned event channel into the Run Dashboard
// before switching modes. Failure paths must surface an error banner
// and leave the active mode unchanged so the plan stays visible.
type PlanStudioDryRunRequestedMsg struct {
	Plan *model.Plan
}

// PlanStudioRealRunRequestedMsg signals that the user pressed `R` from
// Review. Same dispatch contract as the dry-run variant, but with
// DryRun=false.
type PlanStudioRealRunRequestedMsg struct {
	Plan *model.Plan
}

// GeneratePlanCmd is a helper the root model can use to invoke the
// service. It lives in views because it captures the exact PlanRequest
// the Plan Studio is showing, ensuring consistency between displayed
// configuration and the generated plan.
func GeneratePlanCmd(svc services.OrunService, req services.PlanRequest) tea.Cmd {
	return func() tea.Msg {
		res, err := svc.GeneratePlan(context.Background(), req)
		return services.PlanGeneratedMsg{Result: res, Err: err}
	}
}

// JobsByEnvironment returns the result's jobs grouped by environment. It
// is exported for tests asserting DAG grouping invariants.
func (m PlanStudioModel) JobsByEnvironment() map[string][]model.PlanJob {
	out := make(map[string][]model.PlanJob)
	if m.Result == nil || m.Result.Plan == nil {
		return out
	}
	for _, j := range m.Result.Plan.Jobs {
		out[j.Environment] = append(out[j.Environment], j)
	}
	return out
}

// Selected returns the PlanJob currently under the cursor, or nil if the
// view has no result. Used by the root model to drive the inspector.
func (m PlanStudioModel) Selected() *model.PlanJob {
	if m.Result == nil || m.Result.Plan == nil {
		return nil
	}
	if m.Cursor < 0 || m.Cursor >= len(m.Result.Plan.Jobs) {
		return nil
	}
	j := m.Result.Plan.Jobs[m.Cursor]
	return &j
}

// jobAtJobCursor returns the job we last drilled into (StudioLevelSteps /
// StudioLevelStep). Survives Cursor reassignments inside the steps level.
func (m PlanStudioModel) jobAtJobCursor() *model.PlanJob {
	if m.Result == nil || m.Result.Plan == nil {
		return nil
	}
	if m.jobCursor < 0 || m.jobCursor >= len(m.Result.Plan.Jobs) {
		return nil
	}
	j := m.Result.Plan.Jobs[m.jobCursor]
	return &j
}

// SelectedJob is what the inspector binds to: whichever job the user is
// currently looking at, regardless of drilldown level.
func (m PlanStudioModel) SelectedJob() *model.PlanJob {
	switch m.Level {
	case StudioLevelSteps, StudioLevelStep:
		return m.jobAtJobCursor()
	}
	return m.Selected()
}

// SelectedStep returns the currently-focused step (steps level or deeper).
func (m PlanStudioModel) SelectedStep() *model.PlanStep {
	j := m.jobAtJobCursor()
	if j == nil || m.Level == StudioLevelJobs {
		return nil
	}
	if m.stepCursor < 0 || m.stepCursor >= len(j.Steps) {
		return nil
	}
	s := j.Steps[m.stepCursor]
	return &s
}

// StepCursor exposes the step index for inspector helpers that need to
// label a step ("step 3 of N") without re-resolving the cursor.
func (m PlanStudioModel) StepCursor() int { return m.stepCursor }

// BottomPanelContent renders the optional info band that spans main +
// inspector at the foot of the studio. Visibility/sizing are controlled
// by the root model — the view only contributes per-level body text.
//
//	Jobs level  — counts of jobs / envs / phases, plan checksum.
//	Steps level — count + phase breakdown for the focused job.
//	Step level  — capability glance (use / shell / timeout / retry).
func (m PlanStudioModel) BottomPanelContent(width int) string {
	if width < 12 {
		width = 12
	}
	if m.Result == nil || m.Result.Plan == nil {
		return theme.StyleDim.Render("(no plan)")
	}
	switch m.Level {
	case StudioLevelSteps:
		return m.bottomSteps(width)
	case StudioLevelStep:
		return m.bottomStep(width)
	}
	return m.bottomJobs(width)
}

func (m PlanStudioModel) bottomJobs(width int) string {
	jobs := m.Result.Plan.Jobs
	envSet := map[string]struct{}{}
	compSet := map[string]struct{}{}
	for _, j := range jobs {
		if j.Environment != "" {
			envSet[j.Environment] = struct{}{}
		}
		if j.Component != "" {
			compSet[j.Component] = struct{}{}
		}
	}
	checksum := m.Result.Checksum
	if len(checksum) > 8 {
		checksum = checksum[:8]
	}
	chips := []string{
		theme.StyleChipAccent.Render(fmt.Sprintf(" %d jobs ", len(jobs))),
		theme.StyleChipDim.Render(fmt.Sprintf(" %d envs ", len(envSet))),
		theme.StyleChipDim.Render(fmt.Sprintf(" %d components ", len(compSet))),
	}
	hint := theme.StyleDim.Render(fmt.Sprintf("plan %s · ⏎ open job · b hide panel", checksum))
	return theme.StyleLabel.Render("OVERVIEW") + "  " + strings.Join(chips, " ") + "\n" + hint
}

func (m PlanStudioModel) bottomSteps(width int) string {
	j := m.jobAtJobCursor()
	if j == nil {
		return ""
	}
	phases := map[string]int{}
	uses, runs := 0, 0
	for _, s := range j.Steps {
		ph := s.Phase
		if ph == "" {
			ph = "—"
		}
		phases[ph]++
		switch {
		case s.Use != "":
			uses++
		case s.Run != "":
			runs++
		}
	}
	// Stable phase summary.
	keys := make([]string, 0, len(phases))
	for k := range phases {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s:%d", k, phases[k]))
	}
	chips := []string{
		theme.StyleChipAccent.Render(fmt.Sprintf(" %d steps ", len(j.Steps))),
		theme.StyleChipDim.Render(fmt.Sprintf(" %d use ", uses)),
		theme.StyleChipDim.Render(fmt.Sprintf(" %d run ", runs)),
	}
	line2 := theme.StyleDim.Render("phases  " + strings.Join(parts, "  "))
	return theme.StyleLabel.Render("JOB · "+truncateOneLineStudio(j.ID, 32)) + "  " +
		strings.Join(chips, " ") + "\n" + line2
}

func (m PlanStudioModel) bottomStep(width int) string {
	s := m.SelectedStep()
	if s == nil {
		return ""
	}
	cap := "·"
	switch {
	case s.Use != "":
		cap = "use " + s.Use
	case s.Run != "":
		cap = "run"
	}
	chips := []string{theme.StyleChipAccent.Render(" " + cap + " ")}
	if s.Phase != "" {
		chips = append(chips, theme.StyleChipDim.Render(" phase "+s.Phase+" "))
	}
	if s.Timeout != "" {
		chips = append(chips, theme.StyleChipDim.Render(" timeout "+s.Timeout+" "))
	}
	if s.Retry > 0 {
		chips = append(chips, theme.StyleChipDim.Render(fmt.Sprintf(" retry %d ", s.Retry)))
	}
	if s.Shell != "" {
		chips = append(chips, theme.StyleChipDim.Render(" "+s.Shell+" "))
	}
	hint := theme.StyleDim.Render("esc · back to steps")
	return theme.StyleLabel.Render("STEP · "+truncateOneLineStudio(stepDisplayName(*s, m.stepCursor), 32)) +
		"  " + strings.Join(chips, " ") + "\n" + hint
}

// SetCustomization wires in the workspace envs + the known trigger names
// so the cycle keys can rotate through real values.
func (m PlanStudioModel) SetCustomization(envs, triggers []string) PlanStudioModel {
	m.Envs = envs
	m.Triggers = triggers
	return m
}

func cycleString(opts []string, current string) string {
	if len(opts) == 0 {
		return current
	}
	for i, o := range opts {
		if o == current {
			return opts[(i+1)%len(opts)]
		}
	}
	return opts[0]
}
