package views

// Activity surface — drilldown stack edition.
//
// The Activity mode is a 4-level navigable stack inside the cockpit's
// single main pane, mirroring the Components→Component Studio grammar:
//
//   LevelIndex  →  LevelRun  →  LevelJob  →  LevelStep
//   (run list)     (job list)   (step list)  (logs view)
//
// Enter drills in. Esc pops back one level (only falls through to the
// global mode-back stack when popped past LevelIndex). The inspector on
// the right always reflects the row under the cursor at the current
// level; an optional bottom panel (toggled at the root model with `b`)
// spans main+inspector to surface metrics for the current level.
//
// The previous fixed 3-pane layout (runs | DAG | logs) is gone. The DAG
// is reachable on the Run page via `v` (list ⇄ graph toggle) so we keep
// the existing renderer.

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/tui/services"
	"github.com/sourceplane/orun/internal/tui/theme"
)

// ActivityLevel enumerates the drilldown stack positions.
type ActivityLevel int

const (
	LevelIndex ActivityLevel = iota // list of runs
	LevelRun                        // jobs of selected run
	LevelJob                        // steps of selected job
	LevelStep                       // logs of selected step
)

// String returns a short label suitable for the breadcrumb / status bar.
func (l ActivityLevel) String() string {
	switch l {
	case LevelIndex:
		return "runs"
	case LevelRun:
		return "run"
	case LevelJob:
		return "job"
	case LevelStep:
		return "step"
	}
	return ""
}

// RunPageMode toggles between the RunPage's list and graph presentations.
type RunPageMode int

const (
	RunPageList RunPageMode = iota
	RunPageGraph
)

// Backward-compatible aliases so older tests/callers that reference the
// pre-drilldown enum still compile. New code should use ActivityLevel.
type ActivityFocus = ActivityLevel

const (
	ActivityFocusRuns = LevelIndex
	ActivityFocusDAG  = LevelRun
	ActivityFocusLogs = LevelStep
)

// ActivityRun is one entry in the merged (live + history) run list.
type ActivityRun struct {
	ExecID    string
	PlanName  string
	Status    string // running | completed | failed | …
	StartedAt string // pre-formatted "ago"
	Duration  string
	Live      bool

	Plan       *model.Plan
	Statuses   map[string]string            // jobID -> status
	StepInfo   map[string]services.StepInfo // "<jobID>\x00<stepID>" -> step record
	Components []string                     // fallback when Plan is nil
}

// ActivityModel hosts the drilldown stack.
type ActivityModel struct {
	Runs   []ActivityRun
	Width  int
	Height int

	// Drilldown state.
	Level      ActivityLevel
	Cursor     int         // run cursor at LevelIndex (also exposed for legacy tests)
	jobCursor  int         // job cursor at LevelRun
	stepCursor int         // step cursor at LevelJob
	runMode    RunPageMode // list / graph at LevelRun

	// Embedded log viewer used only at LevelStep.
	Logs LogExplorerModel

	// Memo of the last (exec, job, step) we requested a tail for, so we
	// don't churn the channel when the user just moves the cursor.
	attachedExec, attachedJob, attachedStep string
}

// NewActivityModel constructs a blank Activity surface.
func NewActivityModel() ActivityModel {
	return ActivityModel{Logs: NewLogExplorerModel()}
}

// SetRuns merges the provided history with an optional in-flight live
// run (pinned to the top).
func (m ActivityModel) SetRuns(live *ActivityRun, history []services.RunSummary) ActivityModel {
	out := []ActivityRun{}
	if live != nil {
		out = append(out, *live)
	}
	for _, r := range history {
		if live != nil && r.ExecID == live.ExecID {
			continue
		}
		out = append(out, ActivityRun{
			ExecID:     r.ExecID,
			PlanName:   r.PlanName,
			Status:     r.Status,
			StartedAt:  humanAgo(r.StartedAt),
			Duration:   humanDur(r.Duration),
			Components: append([]string(nil), r.Components...),
		})
	}
	m.Runs = out
	if m.Cursor >= len(out) {
		m.Cursor = 0
	}
	return m
}

// FocusRun positions the drilldown on the run with the given exec id and opens
// it at LevelRun (the job list), returning ok=false when no such run is loaded.
// Used by the component page to hand off into the run→job→logs drilldown for a
// specific execution (consumers.md §3). Resets the inner cursors.
func (m ActivityModel) FocusRun(execID string) (ActivityModel, bool) {
	for i, r := range m.Runs {
		if r.ExecID == execID {
			m.Cursor = i
			m.jobCursor = 0
			m.stepCursor = 0
			m.Level = LevelRun
			return m, true
		}
	}
	return m, false
}

// ActivityLoadRunDetailMsg asks the root model to load a historical run's
// compiled plan + per-job statuses (via GetRunDetail) and feed them back through
// SetRunDetail. Emitted when drilling into a run that arrived plan-less from the
// History summary, so its steps can be enumerated and its logs tailed.
type ActivityLoadRunDetailMsg struct {
	ExecID string
}

// loadDetailCmd requests a run's plan + statuses the first time it is opened.
// No-op for live runs (they already carry m.livePlan), already-hydrated runs,
// and runs with no exec id.
func (m ActivityModel) loadDetailCmd(r *ActivityRun) tea.Cmd {
	if r == nil || r.Live || r.Plan != nil || r.ExecID == "" {
		return nil
	}
	exec := r.ExecID
	return func() tea.Msg { return ActivityLoadRunDetailMsg{ExecID: exec} }
}

// LoadDetailCmd returns a command that loads the currently-selected run's
// detail if needed. Used by callers that position the cursor without going
// through drillIn (the component page's run hand-off via FocusRun).
func (m ActivityModel) LoadDetailCmd() tea.Cmd {
	return m.loadDetailCmd(m.SelectedRun())
}

// SetRunDetail merges a lazily-loaded plan, per-job statuses, and per-step
// execution records into the matching run so the drilldown can enumerate its
// steps, show per-step status/timing, and tail its logs. Idempotent and safe to
// re-apply after the run list is rebuilt.
func (m ActivityModel) SetRunDetail(execID string, plan *model.Plan, statuses map[string]string, steps map[string]services.StepInfo) ActivityModel {
	if execID == "" {
		return m
	}
	for i := range m.Runs {
		if m.Runs[i].ExecID != execID {
			continue
		}
		if plan != nil {
			m.Runs[i].Plan = plan
		}
		if len(statuses) > 0 {
			cp := make(map[string]string, len(statuses))
			for k, v := range statuses {
				cp[k] = v
			}
			m.Runs[i].Statuses = cp
		}
		if len(steps) > 0 {
			cp := make(map[string]services.StepInfo, len(steps))
			for k, v := range steps {
				cp[k] = v
			}
			m.Runs[i].StepInfo = cp
		}
	}
	return m
}

// UpdateLiveStatuses refreshes the live run's per-job status map.
func (m ActivityModel) UpdateLiveStatuses(execID string, statuses map[string]string, done bool) ActivityModel {
	for i, r := range m.Runs {
		if r.ExecID == execID || (r.Live && execID == "") {
			cp := map[string]string{}
			for k, v := range statuses {
				cp[k] = v
			}
			m.Runs[i].Statuses = cp
			if done {
				m.Runs[i].Live = false
				if r.Status == "" || r.Status == "running" {
					anyFailed := false
					for _, s := range cp {
						if s == "failed" {
							anyFailed = true
							break
						}
					}
					if anyFailed {
						m.Runs[i].Status = "failed"
					} else {
						m.Runs[i].Status = "completed"
					}
				}
			}
		}
	}
	return m
}

// SetSize stores the outer dimensions and propagates inner content size
// to the embedded log viewer.
func (m ActivityModel) SetSize(w, h int) ActivityModel {
	m.Width = w
	m.Height = h
	// LogExplorer renders its own header/filter/footer; reserve nothing.
	logW := w - 2
	logH := h - 2
	if logW < 10 {
		logW = 10
	}
	if logH < 4 {
		logH = 4
	}
	m.Logs = m.Logs.SetSize(logW, logH)
	return m
}

// --- Selectors -------------------------------------------------------------

// SelectedRun returns the run under the cursor (or nil).
func (m ActivityModel) SelectedRun() *ActivityRun {
	if m.Cursor < 0 || m.Cursor >= len(m.Runs) {
		return nil
	}
	r := m.Runs[m.Cursor]
	return &r
}

// dagNodes returns DAG nodes for the selected run.
func (m ActivityModel) dagNodes() []DAGNode {
	r := m.SelectedRun()
	if r == nil {
		return nil
	}
	if r.Plan != nil {
		return PlanToDAGNodes(r.Plan, r.Statuses)
	}
	out := make([]DAGNode, 0, len(r.Components))
	for _, c := range r.Components {
		st := "completed"
		if strings.EqualFold(r.Status, "failed") {
			st = "failed"
		}
		out = append(out, DAGNode{ID: c, Label: c, Status: st})
	}
	return out
}

// SelectedJobID returns the job under jobCursor (or "").
func (m ActivityModel) SelectedJobID() string {
	nodes := m.dagNodes()
	if m.jobCursor < 0 || m.jobCursor >= len(nodes) {
		return ""
	}
	return nodes[m.jobCursor].ID
}

// SelectedJobNode returns the DAG node currently under the jobCursor.
func (m ActivityModel) SelectedJobNode() *DAGNode {
	nodes := m.dagNodes()
	if m.jobCursor < 0 || m.jobCursor >= len(nodes) {
		return nil
	}
	return &nodes[m.jobCursor]
}

// SelectedJob returns the PlanJob for the selected run+cursor (or nil).
func (m ActivityModel) SelectedJob() *model.PlanJob {
	r := m.SelectedRun()
	if r == nil || r.Plan == nil {
		return nil
	}
	jobID := m.SelectedJobID()
	if jobID == "" {
		return nil
	}
	for i := range r.Plan.Jobs {
		if r.Plan.Jobs[i].ID == jobID {
			return &r.Plan.Jobs[i]
		}
	}
	return nil
}

// selectedSteps returns the step list for the selected job (or nil).
func (m ActivityModel) selectedSteps() []model.PlanStep {
	j := m.SelectedJob()
	if j == nil {
		return nil
	}
	return j.Steps
}

// SelectedStep returns the PlanStep under the stepCursor (or nil).
func (m ActivityModel) SelectedStep() *model.PlanStep {
	steps := m.selectedSteps()
	if m.stepCursor < 0 || m.stepCursor >= len(steps) {
		return nil
	}
	return &steps[m.stepCursor]
}

func stepDisplayID(s model.PlanStep, idx int) string {
	if s.ID != "" {
		return s.ID
	}
	if s.Name != "" {
		return s.Name
	}
	return fmt.Sprintf("step-%d", idx+1)
}

func stepDisplayName(s model.PlanStep, idx int) string {
	if s.Name != "" {
		return s.Name
	}
	if s.ID != "" {
		return s.ID
	}
	return fmt.Sprintf("step %d", idx+1)
}

// --- Log attachment --------------------------------------------------------

// AttachLogs installs a log channel from the root model. stepID may be "".
func (m ActivityModel) AttachLogs(ch <-chan services.LogEvent, jobID, stepID string) (ActivityModel, tea.Cmd) {
	r := m.SelectedRun()
	exec := ""
	if r != nil {
		exec = r.ExecID
	}
	m.attachedExec = exec
	m.attachedJob = jobID
	m.attachedStep = stepID
	var cmd tea.Cmd
	m.Logs, cmd = m.Logs.Attach(ch, jobID, stepID, true)
	return m, cmd
}

// ActivityTailLogsMsg asks the root model to dispatch TailLogs. Live is
// true when the selected run is still in flight, which the root model uses
// to decide between follow-mode tailing and a one-shot historical read.
type ActivityTailLogsMsg struct {
	ExecID string
	JobID  string
	StepID string
	Live   bool
}

// AutoAttachCmd issues a tail-logs request for the current selection
// without modifying the drilldown state. Used when entering Activity
// mode so logs start streaming as soon as the user reaches LevelStep.
// At Index/Run/Job levels this is a no-op — we only attach when the
// user actually drills into a step.
func (m ActivityModel) AutoAttachCmd() (ActivityModel, tea.Cmd) {
	if m.Level != LevelStep {
		return m, nil
	}
	return m.requestTailForSelection()
}

func (m ActivityModel) requestTailForSelection() (ActivityModel, tea.Cmd) {
	r := m.SelectedRun()
	if r == nil {
		return m, nil
	}
	job := m.SelectedJobID()
	if job == "" {
		nodes := m.dagNodes()
		if len(nodes) > 0 {
			job = nodes[0].ID
			m.jobCursor = 0
		}
	}
	if job == "" {
		return m, nil
	}
	step := ""
	if m.Level == LevelStep {
		if s := m.SelectedStep(); s != nil {
			step = stepDisplayID(*s, m.stepCursor)
		}
	}
	if r.ExecID == m.attachedExec && job == m.attachedJob && step == m.attachedStep {
		return m, nil
	}
	exec := r.ExecID
	live := r.Live
	return m, func() tea.Msg {
		return ActivityTailLogsMsg{ExecID: exec, JobID: job, StepID: step, Live: live}
	}
}

// --- Drilldown stack -------------------------------------------------------

func (m ActivityModel) drillIn() (ActivityModel, tea.Cmd) {
	switch m.Level {
	case LevelIndex:
		r := m.SelectedRun()
		if r == nil {
			return m, nil
		}
		m.Level = LevelRun
		m.jobCursor = 0
		// Historical runs arrive plan-less from the History summary; pull their
		// plan + statuses now so the job→step→logs drilldown has something to
		// enumerate.
		return m, m.loadDetailCmd(r)
	case LevelRun:
		if m.SelectedJobNode() == nil {
			return m, nil
		}
		m.Level = LevelJob
		m.stepCursor = 0
		return m, nil
	case LevelJob:
		// Only drill into steps when we have a plan to enumerate.
		if len(m.selectedSteps()) == 0 {
			return m, nil
		}
		m.Level = LevelStep
		return m.requestTailForSelection()
	}
	return m, nil
}

// drillOut pops one level. Returns (model, popped) where popped=false
// means we were already at LevelIndex — the caller can route Esc to the
// global mode-back stack in that case.
func (m ActivityModel) drillOut() (ActivityModel, bool) {
	if m.Level == LevelIndex {
		return m, false
	}
	m.Level--
	if m.Level < LevelStep {
		// Detach the log stream so we don't keep churning when the
		// user wandered away from the logs surface.
		m.Logs = m.Logs.Detach()
		m.attachedExec, m.attachedJob, m.attachedStep = "", "", ""
	}
	return m, true
}

// AtRoot reports whether the activity surface is at LevelIndex.
func (m ActivityModel) AtRoot() bool { return m.Level == LevelIndex }

// --- Update ---------------------------------------------------------------

// Init satisfies tea.Model.
func (m ActivityModel) Init() tea.Cmd { return nil }

// Update handles key + log-stream messages.
func (m ActivityModel) Update(msg tea.Msg) (ActivityModel, tea.Cmd) {
	switch msg := msg.(type) {
	case services.LogEventMsg:
		var cmd tea.Cmd
		m.Logs, cmd = m.Logs.Update(msg)
		return m, cmd
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			// The root model gets first crack at esc; it routes back
			// here when we still have levels to pop. Otherwise we let
			// it fall through to global back-navigation.
			mm, popped := m.drillOut()
			if popped {
				return mm, nil
			}
			return m, nil
		case "enter":
			return m.drillIn()
		}

		switch m.Level {
		case LevelIndex:
			return m.updateIndex(msg)
		case LevelRun:
			return m.updateRun(msg)
		case LevelJob:
			return m.updateJob(msg)
		case LevelStep:
			var cmd tea.Cmd
			m.Logs, cmd = m.Logs.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m ActivityModel) updateIndex(msg tea.KeyMsg) (ActivityModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.Cursor > 0 {
			m.Cursor--
			m.jobCursor = 0
		}
	case "down", "j":
		if m.Cursor+1 < len(m.Runs) {
			m.Cursor++
			m.jobCursor = 0
		}
	case "home", "g":
		m.Cursor = 0
		m.jobCursor = 0
	case "end", "G":
		if len(m.Runs) > 0 {
			m.Cursor = len(m.Runs) - 1
			m.jobCursor = 0
		}
	}
	return m, nil
}

func (m ActivityModel) updateRun(msg tea.KeyMsg) (ActivityModel, tea.Cmd) {
	nodes := m.dagNodes()
	switch msg.String() {
	case "up", "k":
		if m.jobCursor > 0 {
			m.jobCursor--
		}
	case "down", "j":
		if m.jobCursor+1 < len(nodes) {
			m.jobCursor++
		}
	case "home", "g":
		m.jobCursor = 0
	case "end", "G":
		if len(nodes) > 0 {
			m.jobCursor = len(nodes) - 1
		}
	case "v":
		if m.runMode == RunPageList {
			m.runMode = RunPageGraph
		} else {
			m.runMode = RunPageList
		}
	}
	return m, nil
}

func (m ActivityModel) updateJob(msg tea.KeyMsg) (ActivityModel, tea.Cmd) {
	steps := m.selectedSteps()
	switch msg.String() {
	case "up", "k":
		if m.stepCursor > 0 {
			m.stepCursor--
		}
	case "down", "j":
		if m.stepCursor+1 < len(steps) {
			m.stepCursor++
		}
	case "home", "g":
		m.stepCursor = 0
	case "end", "G":
		if len(steps) > 0 {
			m.stepCursor = len(steps) - 1
		}
	}
	return m, nil
}

// --- Inspector --------------------------------------------------------------

// InspectorDesc returns a ResourceDescription appropriate for the
// current drilldown level.
func (m ActivityModel) InspectorDesc() *services.ResourceDescription {
	r := m.SelectedRun()
	if r == nil {
		return nil
	}
	switch m.Level {
	case LevelIndex:
		return activityRunDesc(r)
	case LevelRun:
		if node := m.SelectedJobNode(); node != nil {
			return jobDescFromNode(r, node, m.SelectedJob())
		}
		return activityRunDesc(r)
	case LevelJob:
		if s := m.SelectedStep(); s != nil {
			return stepDesc(r, m.SelectedJob(), s, m.stepCursor)
		}
		if j := m.SelectedJob(); j != nil {
			return jobDescFromNode(r, m.SelectedJobNode(), j)
		}
	case LevelStep:
		if s := m.SelectedStep(); s != nil {
			return stepDesc(r, m.SelectedJob(), s, m.stepCursor)
		}
	}
	return activityRunDesc(r)
}

func jobDescFromNode(r *ActivityRun, n *DAGNode, j *model.PlanJob) *services.ResourceDescription {
	status := ""
	if n != nil {
		status = n.Status
	}
	if status == "" {
		status = "waiting"
	}
	fields := []services.DescField{
		{Label: "status", Value: status},
	}
	if n != nil {
		fields = append(fields, services.DescField{Label: "job id", Value: n.ID})
	}
	if j != nil {
		if j.Component != "" {
			fields = append(fields, services.DescField{Label: "component", Value: j.Component})
		}
		if j.Environment != "" {
			fields = append(fields, services.DescField{Label: "env", Value: j.Environment})
		}
		if j.Profile != "" {
			fields = append(fields, services.DescField{Label: "profile", Value: j.Profile})
		}
		if j.RunsOn != "" {
			fields = append(fields, services.DescField{Label: "runs-on", Value: j.RunsOn})
		}
		if j.Path != "" {
			fields = append(fields, services.DescField{Label: "path", Value: j.Path})
		}
		if len(j.DependsOn) > 0 {
			fields = append(fields, services.DescField{
				Label: "depends-on",
				Value: strings.Join(j.DependsOn, ","),
			})
		}
		if j.Timeout != "" {
			fields = append(fields, services.DescField{Label: "timeout", Value: j.Timeout})
		}
		if j.Retries > 0 {
			fields = append(fields, services.DescField{Label: "retries", Value: fmt.Sprintf("%d", j.Retries)})
		}
		if len(j.Steps) > 0 {
			fields = append(fields, services.DescField{
				Label: fmt.Sprintf("steps (%d)", len(j.Steps)),
				Value: stepListSummary(j.Steps),
			})
		}
	} else if r.Plan == nil {
		fields = append(fields, services.DescField{
			Label: "note",
			Value: "historical run · plan snapshot unavailable",
		})
	}
	fields = append(fields, services.DescField{Label: "run", Value: shortID(r.ExecID)})
	label := ""
	if n != nil {
		label = n.Label
	}
	summary := label
	if r.PlanName != "" {
		summary = label + " · " + r.PlanName
	}
	return &services.ResourceDescription{
		Kind:    "job",
		Name:    label,
		Summary: summary,
		Fields:  fields,
		Actions: []string{"⏎ open steps", "esc back to run"},
	}
}

func stepListSummary(steps []model.PlanStep) string {
	names := make([]string, 0, len(steps))
	for i, s := range steps {
		names = append(names, stepDisplayName(s, i))
	}
	return strings.Join(names, "\n")
}

func stepDesc(r *ActivityRun, j *model.PlanJob, s *model.PlanStep, idx int) *services.ResourceDescription {
	fields := []services.DescField{
		{Label: "step id", Value: stepDisplayID(*s, idx)},
	}
	// Status + timing from the execution record, when available.
	if r != nil && j != nil && r.StepInfo != nil {
		if info, ok := r.StepInfo[services.StepDetailKey(j.ID, stepDisplayID(*s, idx))]; ok {
			_, _, chip := stepStatusChip(info.Status)
			fields = append(fields, services.DescField{Label: "status", Value: chip})
			if d := stepDurationText(info); d != "—" {
				fields = append(fields, services.DescField{Label: "duration", Value: d})
			}
			if info.Status == "failed" && info.ExitCode != 0 {
				fields = append(fields, services.DescField{Label: "exit code", Value: fmt.Sprintf("%d", info.ExitCode)})
			}
		}
	}
	if s.Phase != "" {
		fields = append(fields, services.DescField{Label: "phase", Value: s.Phase})
	}
	if s.Use != "" {
		fields = append(fields, services.DescField{Label: "use", Value: s.Use})
	}
	if s.Run != "" {
		fields = append(fields, services.DescField{Label: "run", Value: truncateMultiline(s.Run, 120)})
	}
	if s.Shell != "" {
		fields = append(fields, services.DescField{Label: "shell", Value: s.Shell})
	}
	if s.WorkingDirectory != "" {
		fields = append(fields, services.DescField{Label: "working-dir", Value: s.WorkingDirectory})
	}
	if s.Timeout != "" {
		fields = append(fields, services.DescField{Label: "timeout", Value: s.Timeout})
	}
	if s.Retry > 0 {
		fields = append(fields, services.DescField{Label: "retry", Value: fmt.Sprintf("%d", s.Retry)})
	}
	if s.OnFailure != "" {
		fields = append(fields, services.DescField{Label: "on-failure", Value: s.OnFailure})
	}
	if j != nil {
		fields = append(fields, services.DescField{Label: "job", Value: j.ID})
	}
	fields = append(fields, services.DescField{Label: "run", Value: shortID(r.ExecID)})

	// Jump-back chips — the inspector is the user's spatial memory of
	// where they came from. Keep them clickable in spirit (esc pops one
	// level, esc-esc pops two) and visually distinct from data fields.
	jumpChips := theme.StyleChipDim.Render(" ◀ esc · back to job ") + "  " +
		theme.StyleChipDim.Render(" ◀◀ esc esc · back to run ")
	fields = append(fields, services.DescField{
		Label: "jump back",
		Value: jumpChips,
	})

	return &services.ResourceDescription{
		Kind:    "step",
		Name:    stepDisplayName(*s, idx),
		Summary: jobLabel(j),
		Fields:  fields,
		Actions: []string{"⏎ tail logs", "esc back to job"},
	}
}

func jobLabel(j *model.PlanJob) string {
	if j == nil {
		return ""
	}
	if j.Component != "" {
		if j.Environment != "" {
			return j.Component + " · " + j.Environment
		}
		return j.Component
	}
	return j.ID
}

func activityRunDesc(r *ActivityRun) *services.ResourceDescription {
	pill := r.Status
	if r.Live {
		pill = "live"
	}
	fields := []services.DescField{
		{Label: "status", Value: pill},
		{Label: "exec id", Value: r.ExecID},
	}
	if r.PlanName != "" {
		fields = append(fields, services.DescField{Label: "plan", Value: r.PlanName})
	}
	if r.StartedAt != "" {
		fields = append(fields, services.DescField{Label: "started", Value: r.StartedAt})
	}
	if r.Duration != "" {
		fields = append(fields, services.DescField{Label: "duration", Value: r.Duration})
	}
	if r.Plan != nil {
		done, running, failed, waiting := tallyStatuses(r.Plan, r.Statuses)
		fields = append(fields, services.DescField{
			Label: "jobs",
			Value: fmt.Sprintf("%d total · %d ✓  %d ●  %d ✗  %d ·",
				len(r.Plan.Jobs), done, running, failed, waiting),
		})
	} else if len(r.Components) > 0 {
		fields = append(fields, services.DescField{
			Label: "components",
			Value: strings.Join(r.Components, ","),
		})
	}
	return &services.ResourceDescription{
		Kind:    "run",
		Name:    shortID(r.ExecID),
		Summary: r.PlanName,
		Fields:  fields,
		Actions: []string{"⏎ open jobs", "esc back"},
	}
}

func tallyStatuses(plan *model.Plan, st map[string]string) (done, running, failed, waiting int) {
	for _, j := range plan.Jobs {
		switch st[j.ID] {
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
	return
}

// FocusLabel returns the current drilldown level as a short label for
// the status bar / breadcrumb.
func (m ActivityModel) FocusLabel() string { return m.Level.String() }

// Breadcrumb returns segments describing the current drilldown path.
// Each segment is a short label suitable for joining with " › ".
func (m ActivityModel) Breadcrumb() []string {
	out := []string{"activity"}
	if m.Level == LevelIndex {
		return out
	}
	r := m.SelectedRun()
	if r != nil {
		out = append(out, "run "+shortID(r.ExecID))
	}
	if m.Level >= LevelJob {
		if j := m.SelectedJobNode(); j != nil {
			out = append(out, "job "+j.Label)
		}
	}
	if m.Level >= LevelStep {
		if s := m.SelectedStep(); s != nil {
			out = append(out, "step "+stepDisplayName(*s, m.stepCursor))
		}
	}
	return out
}

// --- View -----------------------------------------------------------------

func (m ActivityModel) View() string {
	if m.Width <= 0 || m.Height <= 0 {
		return ""
	}
	switch m.Level {
	case LevelIndex:
		return m.renderIndex()
	case LevelRun:
		return m.renderRunPage()
	case LevelJob:
		return m.renderJobPage()
	case LevelStep:
		return m.renderStepPage()
	}
	return ""
}

// ── LevelIndex: list of runs ──────────────────────────────────────────────
func (m ActivityModel) renderIndex() string {
	var b strings.Builder
	b.WriteString(theme.StyleSectionTitle.Render("Activity"))
	b.WriteString(theme.StyleDim.Render(fmt.Sprintf("  ·  %d runs", len(m.Runs))))
	live := 0
	for _, r := range m.Runs {
		if r.Live {
			live++
		}
	}
	if live > 0 {
		b.WriteString("  " + theme.StylePillRunning.Render(fmt.Sprintf("● %d live", live)))
	}
	b.WriteString("\n\n")

	if len(m.Runs) == 0 {
		b.WriteString(centerCard(m.Width, m.Height-4,
			"no runs yet — dispatch from Components (1)."))
		return b.String()
	}

	// Columns.
	idW := clamp(m.Width*12/100, 10, 14)
	statW := clamp(m.Width*12/100, 10, 14)
	planW := clamp(m.Width*40/100, 16, 56)
	agoW := 10
	durW := 8

	header := theme.StyleTableHeader.Render(fmt.Sprintf(" %s %s %s %s %s",
		pad("EXEC", idW), pad("STATUS", statW),
		pad("PLAN", planW), pad("STARTED", agoW), pad("DURATION", durW)))
	b.WriteString(" " + header + "\n")

	maxRows := m.Height - 6
	if maxRows < 3 {
		maxRows = 3
	}
	start, end := viewportWindow(m.Cursor, len(m.Runs), maxRows)

	for i := start; i < end; i++ {
		r := m.Runs[i]
		exec := r.ExecID
		if len(exec) > idW-1 {
			exec = exec[:idW-1]
		}
		pill := runStatusPill(r.Status)
		if r.Live {
			pill = theme.StylePillRunning.Render(pulseGlyph() + " LIVE")
		}
		line := fmt.Sprintf(" %s %s %s %s %s",
			pad(exec, idW), pad(pill, statW+8),
			pad(zoa(r.PlanName), planW), pad(r.StartedAt, agoW), pad(r.Duration, durW))
		if i == m.Cursor {
			b.WriteString(theme.StyleCursorBar.Render("▌") +
				theme.StyleTableRowSelected.Render(line))
		} else if i%2 == 1 {
			b.WriteString(" " + theme.StyleTableRowAlt.Render(line))
		} else {
			b.WriteString(" " + theme.StyleTableRow.Render(line))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(theme.StyleDim.Render(
		"↑↓ select · ⏎ open run · esc back · b bottom panel"))
	return b.String()
}

// ── LevelRun: jobs of selected run ─────────────────────────────────────────
func (m ActivityModel) renderRunPage() string {
	r := m.SelectedRun()
	if r == nil {
		return centerCard(m.Width, m.Height, "no run selected")
	}
	var b strings.Builder
	pill := runStatusPill(r.Status)
	if r.Live {
		pill = theme.StylePillRunning.Render(pulseGlyph() + " LIVE")
	}
	title := theme.StyleSectionTitle.Render("Run") + "  " +
		theme.StyleChipAccent.Render(shortID(r.ExecID)) + "  " + pill
	if r.PlanName != "" {
		title += "  " + theme.StyleDim.Render(r.PlanName)
	}
	b.WriteString(title + "\n")
	nodes := m.dagNodes()
	b.WriteString(DAGSummary(nodes) + "\n\n")

	if len(nodes) == 0 {
		b.WriteString(theme.StyleDim.Render("  (no jobs)"))
		return b.String()
	}

	if m.runMode == RunPageGraph {
		b.WriteString(RenderDAG(nodes, m.SelectedJobID(), m.Width-2))
	} else {
		b.WriteString(m.renderJobList(nodes, r))
	}

	b.WriteString("\n")
	mode := "list"
	if m.runMode == RunPageGraph {
		mode = "graph"
	}
	b.WriteString(theme.StyleDim.Render(
		"↑↓ select · ⏎ open job · esc back · v toggle [" + mode + "]"))
	return b.String()
}

func (m ActivityModel) renderJobList(nodes []DAGNode, r *ActivityRun) string {
	var b strings.Builder
	nameW := clamp(m.Width*40/100, 18, 60)
	statW := 12

	header := theme.StyleTableHeader.Render(fmt.Sprintf(" %s  %s  %s",
		pad("JOB", nameW), pad("STATUS", statW), "DEPS"))
	b.WriteString(" " + header + "\n")

	for i, n := range nodes {
		st := n.Status
		if st == "" {
			st = "waiting"
		}
		statusPill := jobStatusPill(st)
		deps := "—"
		if len(n.DependsOn) > 0 {
			deps = strings.Join(n.DependsOn, ",")
		}
		icon := dagStatusIcon(st)
		line := fmt.Sprintf(" %s %s  %s  %s",
			icon, pad(n.Label, nameW-2), pad(statusPill, statW+8), deps)
		if i == m.jobCursor {
			b.WriteString(theme.StyleCursorBar.Render("▌") +
				theme.StyleTableRowSelected.Render(line))
		} else if i%2 == 1 {
			b.WriteString(" " + theme.StyleTableRowAlt.Render(line))
		} else {
			b.WriteString(" " + theme.StyleTableRow.Render(line))
		}
		b.WriteString("\n")
	}
	_ = r
	return b.String()
}

func jobStatusPill(s string) string {
	switch strings.ToLower(s) {
	case "completed", "done", "success":
		return theme.StylePillSuccess.Render("✓ " + s)
	case "failed", "error":
		return theme.StylePillError.Render("✗ " + s)
	case "running":
		return theme.StylePillRunning.Render(pulseGlyph() + " " + s)
	case "waiting", "pending", "queued":
		return theme.StylePillIdle.Render("· " + s)
	}
	return theme.StyleChipDim.Render(s)
}

// ── LevelJob: steps of selected job ────────────────────────────────────────
func (m ActivityModel) renderJobPage() string {
	r := m.SelectedRun()
	j := m.SelectedJob()
	node := m.SelectedJobNode()
	if r == nil {
		return centerCard(m.Width, m.Height, "no run selected")
	}
	var b strings.Builder
	label := jobLabel(j)
	if label == "" && node != nil {
		label = node.Label
	}
	if label == "" {
		label = "(job)"
	}
	st := ""
	if node != nil {
		st = node.Status
	}
	title := theme.StyleSectionTitle.Render("Job") + "  " +
		theme.StyleChipAccent.Render(label) + "  " + jobStatusPill(st)
	b.WriteString(title + "\n")

	if j == nil {
		hint := "historical run · step list unavailable (no plan snapshot)"
		b.WriteString(theme.StyleDim.Render(hint))
		return b.String()
	}
	steps := j.Steps
	jobID := m.SelectedJobID()
	stepStatus := func(s model.PlanStep, idx int) string {
		if r.StepInfo == nil {
			return ""
		}
		return r.StepInfo[services.StepDetailKey(jobID, stepDisplayID(s, idx))].Status
	}

	// Summary line: total + a per-status tally so the eye gets the shape of the
	// job before scanning rows.
	b.WriteString(theme.StyleDim.Render(fmt.Sprintf("%d steps", len(steps))) +
		stepStatusTally(steps, stepStatus) + "\n\n")
	if len(steps) == 0 {
		b.WriteString(theme.StyleDim.Render("  (no steps)"))
		return b.String()
	}

	// Columns: STATUS · STEP · PHASE · DURATION · COMMAND.
	statusW := 10
	nameW := clamp(m.Width*28/100, 16, 44)
	phaseW := 7
	durW := 9
	cmdW := m.Width - statusW - nameW - phaseW - durW - 12
	if cmdW < 12 {
		cmdW = 12
	}

	header := theme.StyleTableHeader.Render(fmt.Sprintf(" %s  %s  %s  %s  %s",
		pad("STATUS", statusW), pad("STEP", nameW), pad("PHASE", phaseW),
		fmt.Sprintf("%*s", durW, "DURATION"), "COMMAND"))
	b.WriteString(" " + header + "\n")

	// Viewport: clip rows to the available height and scroll with the cursor so
	// long step lists never overflow the stage.
	maxRows := m.Height - 6
	if maxRows < 3 {
		maxRows = 3
	}
	start, end := viewportWindow(m.stepCursor, len(steps), maxRows)

	for i := start; i < end; i++ {
		s := steps[i]
		info := services.StepInfo{}
		if r.StepInfo != nil {
			info = r.StepInfo[services.StepDetailKey(jobID, stepDisplayID(s, i))]
		}
		status := stepStatusCell(info.Status, statusW)
		name := stepDisplayName(s, i)
		phase := s.Phase
		if phase == "" {
			phase = "—"
		}
		dur := stepDurationText(info)
		cmd := s.Run
		if cmd == "" && s.Use != "" {
			cmd = "use: " + s.Use
		}
		cmd = truncate(strings.ReplaceAll(cmd, "\n", " ↵ "), cmdW)
		line := fmt.Sprintf(" %s  %s  %s  %s  %s",
			status, pad(name, nameW), pad(phase, phaseW),
			fmt.Sprintf("%*s", durW, dur), cmd)
		if i == m.stepCursor {
			b.WriteString(theme.StyleCursorBar.Render("▌") +
				theme.StyleTableRowSelected.Render(line))
		} else if i%2 == 1 {
			b.WriteString(" " + theme.StyleTableRowAlt.Render(line))
		} else {
			b.WriteString(" " + theme.StyleTableRow.Render(line))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(theme.StyleDim.Render(
		"↑↓ select · ⏎ tail logs · esc back to run"))
	return b.String()
}

// stepStatusCell renders a fixed-width, color-coded status chip (glyph + label)
// for a step, padded to visible width w. An empty status (no execution record
// yet) reads as pending.
func stepStatusCell(status string, w int) string {
	glyph, label, styled := stepStatusChip(status)
	return padStyled(styled, glyph+" "+label, w)
}

// stepStatusChip returns the glyph, plain label, and fully-styled chip for a
// step status. Vocabulary is the legacy runner set (completed/failed/running/
// pending) folded from the object model, plus skipped.
func stepStatusChip(status string) (glyph, label, styled string) {
	switch status {
	case "completed", "done", "success", "succeeded":
		glyph, label = "✓", "done"
		styled = theme.StylePillSuccess.Render(glyph + " " + label)
	case "failed", "error":
		glyph, label = "✗", "failed"
		styled = theme.StylePillError.Render(glyph + " " + label)
	case "running":
		glyph, label = pulseGlyph(), "running"
		styled = theme.StylePillRunning.Render(glyph + " " + label)
	case "skipped":
		glyph, label = "⊘", "skipped"
		styled = theme.StyleDim.Render(glyph + " " + label)
	default:
		glyph, label = "○", "pending"
		styled = theme.StyleDim.Render(glyph + " " + label)
	}
	return glyph, label, styled
}

// stepDurationText formats a step's duration column. Finished steps show their
// elapsed time; a running step shows a live marker; everything else is a dash.
func stepDurationText(info services.StepInfo) string {
	if info.Duration > 0 {
		return humanDur(info.Duration)
	}
	if info.Status == "running" {
		return "running…"
	}
	return "—"
}

// stepStatusTally renders the " · 3 done · 1 running" suffix on the step-count
// line. Empty when no execution records are available (plan-only view).
func stepStatusTally(steps []model.PlanStep, statusOf func(model.PlanStep, int) string) string {
	var done, running, failed, pending int
	any := false
	for i, s := range steps {
		switch statusOf(s, i) {
		case "completed", "done", "success", "succeeded":
			done++
			any = true
		case "running":
			running++
			any = true
		case "failed", "error":
			failed++
			any = true
		case "":
			pending++
		default:
			pending++
			any = true
		}
	}
	if !any {
		return ""
	}
	out := ""
	if done > 0 {
		out += "  " + theme.StylePillSuccess.Render(fmt.Sprintf("%d done", done))
	}
	if running > 0 {
		out += "  " + theme.StylePillRunning.Render(fmt.Sprintf("%d running", running))
	}
	if failed > 0 {
		out += "  " + theme.StylePillError.Render(fmt.Sprintf("%d failed", failed))
	}
	if pending > 0 {
		out += "  " + theme.StyleDim.Render(fmt.Sprintf("%d pending", pending))
	}
	return out
}

// ── LevelStep: logs ───────────────────────────────────────────────────────
func (m ActivityModel) renderStepPage() string {
	r := m.SelectedRun()
	j := m.SelectedJob()
	s := m.SelectedStep()
	var b strings.Builder
	label := "(step)"
	if s != nil {
		label = stepDisplayName(*s, m.stepCursor)
	}
	title := theme.StyleSectionTitle.Render("Step") + "  " +
		theme.StyleChipAccent.Render(label)
	if j != nil {
		title += "  " + theme.StyleDim.Render("· "+jobLabel(j))
	}
	if r != nil {
		title += "  " + theme.StyleDim.Render("· "+shortID(r.ExecID))
	}
	b.WriteString(title + "\n\n")
	b.WriteString(m.Logs.View())
	return b.String()
}

// --- Bottom panel ---------------------------------------------------------

// BottomPanelContent returns a per-level summary string suitable for
// rendering in the optional bottom panel. The root model controls
// visibility and sizing.
func (m ActivityModel) BottomPanelContent(width int) string {
	if width < 12 {
		width = 12
	}
	switch m.Level {
	case LevelIndex:
		return m.bottomIndex(width)
	case LevelRun:
		return m.bottomRun(width)
	case LevelJob:
		return m.bottomJob(width)
	case LevelStep:
		return m.bottomStep(width)
	}
	return ""
}

func (m ActivityModel) bottomIndex(width int) string {
	if len(m.Runs) == 0 {
		return theme.StyleDim.Render("no run history yet")
	}
	var ok, fail, live int
	for _, r := range m.Runs {
		if r.Live {
			live++
			continue
		}
		switch strings.ToLower(r.Status) {
		case "completed", "success", "done":
			ok++
		case "failed", "error":
			fail++
		}
	}
	spark := buildSparkline(m.Runs)
	chips := []string{
		theme.StylePillSuccess.Render(fmt.Sprintf("✓ %d", ok)),
		theme.StylePillError.Render(fmt.Sprintf("✗ %d", fail)),
		theme.StylePillRunning.Render(fmt.Sprintf("● %d live", live)),
	}
	return theme.StyleLabel.Render("OVERVIEW") + "  " + strings.Join(chips, "  ") +
		"\n" + theme.StyleDim.Render("recent runs  ") + spark
}

func (m ActivityModel) bottomRun(width int) string {
	r := m.SelectedRun()
	if r == nil {
		return ""
	}
	nodes := m.dagNodes()
	if len(nodes) == 0 {
		return theme.StyleDim.Render("(no jobs)")
	}
	done, running, failed, waiting := 0, 0, 0, 0
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
	bar := progressBar(done, len(nodes), width-20)
	return theme.StyleLabel.Render("RUN PROGRESS") + "  " +
		fmt.Sprintf("%d/%d done", done, len(nodes)) + "  " + bar + "\n" +
		theme.StyleDim.Render(fmt.Sprintf(
			"running %d · failed %d · waiting %d · plan %s",
			running, failed, waiting, shortID(planChecksum(r)),
		))
}

func planChecksum(r *ActivityRun) string {
	if r == nil || r.Plan == nil {
		return ""
	}
	return r.Plan.Metadata.Checksum
}

func (m ActivityModel) bottomJob(width int) string {
	j := m.SelectedJob()
	if j == nil {
		return theme.StyleDim.Render("(no job)")
	}
	return theme.StyleLabel.Render("JOB") + "  " +
		theme.StyleValue.Render(j.ID) + "\n" +
		theme.StyleDim.Render(fmt.Sprintf(
			"component %s · env %s · profile %s · %d steps",
			zoa(j.Component), zoa(j.Environment), zoa(j.Profile), len(j.Steps),
		))
}

func (m ActivityModel) bottomStep(width int) string {
	// Logs summary — lean on whatever has been buffered so far.
	total, errs, warns := m.Logs.severityCounts()
	chips := []string{
		theme.StyleDim.Render(fmt.Sprintf("%d lines", total)),
	}
	if errs > 0 {
		chips = append(chips, theme.StylePillError.Render(fmt.Sprintf("%d err", errs)))
	}
	if warns > 0 {
		chips = append(chips, theme.StylePillWarn.Render(fmt.Sprintf("%d warn", warns)))
	}
	return theme.StyleLabel.Render("LOGS") + "  " + strings.Join(chips, "  ") + "\n" +
		theme.StyleDim.Render("f follow · E errors-only · [ ] jump step · g/G top/bot · / filter")
}

// progressBar renders a unicode block progress bar.
func progressBar(done, total, width int) string {
	if width < 4 {
		width = 4
	}
	if total <= 0 {
		return strings.Repeat("·", width)
	}
	filled := done * width / total
	if filled > width {
		filled = width
	}
	return theme.StylePillSuccess.Render(strings.Repeat("█", filled)) +
		theme.StyleDim.Render(strings.Repeat("░", width-filled))
}

// buildSparkline maps run status history to a unicode sparkline,
// 1 char per run (newest right). Cap to 40 chars.
func buildSparkline(runs []ActivityRun) string {
	const max = 40
	n := len(runs)
	if n > max {
		runs = runs[:max]
		n = max
	}
	// Reverse so newest is on the right.
	out := make([]rune, n)
	for i := 0; i < n; i++ {
		r := runs[n-1-i]
		switch strings.ToLower(r.Status) {
		case "failed", "error":
			out[i] = '█'
		case "running":
			out[i] = '▆'
		case "completed", "done", "success":
			out[i] = '▃'
		default:
			out[i] = '·'
		}
	}
	return string(out)
}

// --- Helpers --------------------------------------------------------------

func truncateMultiline(s string, w int) string {
	s = strings.ReplaceAll(s, "\n", " ↵ ")
	return truncate(s, w)
}

func shortID(s string) string {
	if len(s) > 10 {
		return s[:10]
	}
	if s == "" {
		return "—"
	}
	return s
}

// SortRunsByRecency sorts in-place (newest first). Kept exported.
func SortRunsByRecency(runs []ActivityRun) {
	sort.SliceStable(runs, func(i, j int) bool {
		if runs[i].Live != runs[j].Live {
			return runs[i].Live
		}
		return runs[i].StartedAt > runs[j].StartedAt
	})
}
