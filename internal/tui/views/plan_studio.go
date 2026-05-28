package views

import tea "github.com/charmbracelet/bubbletea"

// PlanStudioModel is a stub for Phase 2.
type PlanStudioModel struct{}

func NewPlanStudioModel() PlanStudioModel { return PlanStudioModel{} }

func (m PlanStudioModel) Init() tea.Cmd { return nil }
func (m PlanStudioModel) Update(msg tea.Msg) (PlanStudioModel, tea.Cmd) {
	return m, nil
}
func (m PlanStudioModel) View() string {
	return "Plan Studio (Phase 2 — not yet implemented)"
}
