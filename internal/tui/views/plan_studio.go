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
	Generate key.Binding
	Save     key.Binding
	DryRun   key.Binding
	Up       key.Binding
	Down     key.Binding
	Clear    key.Binding
}

// DefaultPlanStudioKeyMap returns the canonical Plan Studio bindings.
func DefaultPlanStudioKeyMap() PlanStudioKeyMap {
	return PlanStudioKeyMap{
		Generate: key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "generate plan")),
		Save:     key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "save plan")),
		DryRun:   key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "dry-run")),
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "prev job")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "next job")),
		Clear:    key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "clear plan")),
	}
}

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
	Cursor   int
	Width    int
	Height   int
	keys     PlanStudioKeyMap
	saveName string // user-provided named-plan label (default: "tui-draft")
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
		case key.Matches(msg, m.keys.Generate):
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
			// Only valid from Review with a generated plan. The root
			// model intercepts the emitted marker, dispatches RunPlan
			// against m.Result.Plan with DryRun=true, and transitions
			// to ModeRunDashboard only after the service returns a
			// non-nil event channel.
			if m.State != PlanStudioReview || m.Result == nil || m.Result.Plan == nil {
				return m, nil
			}
			return m, func() tea.Msg {
				return PlanStudioDryRunRequestedMsg{Plan: m.Result.Plan}
			}
		case key.Matches(msg, m.keys.Clear):
			if m.State == PlanStudioGenerating {
				return m, nil
			}
			m.State = PlanStudioIdle
			m.Result = nil
			m.Err = nil
			m.Cursor = 0
			return m, nil
		case key.Matches(msg, m.keys.Down):
			if m.Result != nil && m.Cursor+1 < len(m.Result.Plan.Jobs) {
				m.Cursor++
			}
			return m, nil
		case key.Matches(msg, m.keys.Up):
			if m.Cursor > 0 {
				m.Cursor--
			}
			return m, nil
		}
	}
	return m, nil
}

// View renders the Plan Studio main panel.
func (m PlanStudioModel) View() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Plan Studio — %s\n", m.State.String())
	if m.Request.IntentFile != "" {
		fmt.Fprintf(&b, "intent: %s\n", m.Request.IntentFile)
	}
	if m.Request.Environment != "" {
		fmt.Fprintf(&b, "env: %s\n", m.Request.Environment)
	}
	if m.Request.ChangedOnly {
		fmt.Fprintf(&b, "scope: changed-only (base=%q head=%q)\n",
			m.Request.BaseBranch, m.Request.HeadRef)
	}
	b.WriteString("\n")

	switch m.State {
	case PlanStudioIdle, PlanStudioConfiguring:
		b.WriteString("press `g` to generate a plan from the current intent.\n")
	case PlanStudioGenerating:
		b.WriteString("generating plan…\n")
	case PlanStudioError:
		fmt.Fprintf(&b, "error: %s\n", errString(m.Err))
		b.WriteString("press `g` to retry, `c` to clear.\n")
	case PlanStudioReview, PlanStudioSaved:
		b.WriteString(m.renderReview())
	}
	return b.String()
}

func (m PlanStudioModel) renderReview() string {
	if m.Result == nil || m.Result.Plan == nil {
		return "(no plan)\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "checksum: %s   jobs: %d   components: %d\n",
		m.Result.Checksum, m.Result.JobCount, len(m.Result.Components))
	if len(m.Result.Warnings) > 0 {
		for _, w := range m.Result.Warnings {
			fmt.Fprintf(&b, "warn: %s\n", w)
		}
	}
	b.WriteString("\nJobs (DAG order):\n")
	for i, job := range m.Result.Plan.Jobs {
		marker := "  "
		if i == m.Cursor {
			marker = "› "
		}
		deps := "-"
		if len(job.DependsOn) > 0 {
			ds := append([]string(nil), job.DependsOn...)
			sort.Strings(ds)
			deps = strings.Join(ds, ",")
		}
		fmt.Fprintf(&b, "%s%-32s [%s] env=%s deps=%s\n",
			marker, job.ID, shortComp(job.Composition), job.Environment, deps)
	}
	if m.State == PlanStudioSaved {
		fmt.Fprintf(&b, "\nsaved as: %s\n", m.saveName)
	} else {
		fmt.Fprintf(&b, "\n`s` to save as %q  •  `c` to clear  •  `g` to regenerate\n", m.saveName)
	}
	return b.String()
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
