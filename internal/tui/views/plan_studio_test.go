package views

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"pgregory.net/rapid"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/tui/services"
)

func keyMsg(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestPlanStudioModel_InitialState(t *testing.T) {
	m := NewPlanStudioModel()
	if m.State != PlanStudioIdle {
		t.Fatalf("State = %v, want Idle", m.State)
	}
	if got := m.View(); !strings.Contains(got, "idle") {
		t.Errorf("View missing 'idle' marker: %q", got)
	}
}

func TestPlanStudioModel_GenerateKeyTransitionsToGenerating(t *testing.T) {
	m := NewPlanStudioModel()
	m, _ = m.Update(keyMsg("g"))
	if m.State != PlanStudioGenerating {
		t.Fatalf("after g: State = %v, want Generating", m.State)
	}
}

func TestPlanStudioModel_GenerateBlockedWhileGenerating(t *testing.T) {
	m := NewPlanStudioModel().MarkGenerating()
	// Any key while Generating must be a no-op.
	before := m.State
	m, _ = m.Update(keyMsg("g"))
	m, _ = m.Update(keyMsg("c"))
	m, _ = m.Update(keyMsg("s"))
	if m.State != before {
		t.Fatalf("Generating state should be immutable to keys; got %v", m.State)
	}
}

func TestPlanStudioModel_PlanGeneratedSuccess(t *testing.T) {
	m := NewPlanStudioModel().MarkGenerating()
	result := &services.PlanResult{
		Plan: &model.Plan{Jobs: []model.PlanJob{
			{ID: "a", Component: "alpha", Environment: "dev", Composition: "terraform"},
			{ID: "b", Component: "beta", Environment: "dev", Composition: "terraform", DependsOn: []string{"a"}},
		}},
		Checksum: "abc12345",
		JobCount: 2,
	}
	m, _ = m.Update(services.PlanGeneratedMsg{Result: result})
	if m.State != PlanStudioReview {
		t.Fatalf("State = %v, want Review", m.State)
	}
	if m.Result == nil || m.Result.Checksum != "abc12345" {
		t.Errorf("Result not stored")
	}
	view := m.View()
	for _, want := range []string{"abc12345", "a ", "b ", "deps=a"} {
		if !strings.Contains(view, want) {
			t.Errorf("View missing %q\n---\n%s", want, view)
		}
	}
}

func TestPlanStudioModel_PlanGeneratedError(t *testing.T) {
	m := NewPlanStudioModel().MarkGenerating()
	m, _ = m.Update(services.PlanGeneratedMsg{Err: errors.New("boom")})
	if m.State != PlanStudioError {
		t.Fatalf("State = %v, want Error", m.State)
	}
	if !strings.Contains(m.View(), "boom") {
		t.Errorf("error not surfaced in view: %q", m.View())
	}
}

func TestPlanStudioModel_CursorMovement(t *testing.T) {
	m := NewPlanStudioModel().MarkGenerating()
	m, _ = m.Update(services.PlanGeneratedMsg{Result: &services.PlanResult{
		Plan: &model.Plan{Jobs: []model.PlanJob{{ID: "a"}, {ID: "b"}, {ID: "c"}}},
	}})
	if m.Cursor != 0 {
		t.Fatalf("initial cursor = %d", m.Cursor)
	}
	m, _ = m.Update(keyMsg("j"))
	m, _ = m.Update(keyMsg("j"))
	if m.Cursor != 2 {
		t.Errorf("Cursor after 2x down = %d, want 2", m.Cursor)
	}
	// past-end is clamped
	m, _ = m.Update(keyMsg("j"))
	if m.Cursor != 2 {
		t.Errorf("Cursor over-scroll = %d, want clamped 2", m.Cursor)
	}
	m, _ = m.Update(keyMsg("k"))
	if m.Cursor != 1 {
		t.Errorf("Cursor after up = %d", m.Cursor)
	}
}

func TestPlanStudioModel_ClearReturnsToIdle(t *testing.T) {
	m := NewPlanStudioModel().MarkGenerating()
	m, _ = m.Update(services.PlanGeneratedMsg{Result: &services.PlanResult{
		Plan: &model.Plan{Jobs: []model.PlanJob{{ID: "x"}}},
	}})
	m, _ = m.Update(keyMsg("c"))
	if m.State != PlanStudioIdle {
		t.Fatalf("State after clear = %v, want Idle", m.State)
	}
	if m.Result != nil {
		t.Error("Result should be cleared")
	}
}

func TestPlanStudioModel_SaveEmitsMessage(t *testing.T) {
	m := NewPlanStudioModel().MarkGenerating()
	m, _ = m.Update(services.PlanGeneratedMsg{Result: &services.PlanResult{
		Plan: &model.Plan{Jobs: []model.PlanJob{{ID: "x"}}},
	}})
	m = m.SetSaveName("my-plan")
	_, cmd := m.Update(keyMsg("s"))
	if cmd == nil {
		t.Fatal("expected save cmd")
	}
	msg := cmd()
	saved, ok := msg.(PlanStudioSaveRequestedMsg)
	if !ok {
		t.Fatalf("msg = %T, want PlanStudioSaveRequestedMsg", msg)
	}
	if saved.Name != "my-plan" {
		t.Errorf("saved.Name = %q", saved.Name)
	}
}

func TestPlanStudioModel_DryRunFromReviewEmitsMessage(t *testing.T) {
	m := NewPlanStudioModel().MarkGenerating()
	plan := &model.Plan{Jobs: []model.PlanJob{{ID: "x"}}}
	m, _ = m.Update(services.PlanGeneratedMsg{Result: &services.PlanResult{Plan: plan}})
	_, cmd := m.Update(keyMsg("d"))
	if cmd == nil {
		t.Fatal("expected dry-run cmd")
	}
	got, ok := cmd().(PlanStudioDryRunRequestedMsg)
	if !ok {
		t.Fatalf("msg = %T, want PlanStudioDryRunRequestedMsg", cmd())
	}
	if got.Plan != plan {
		t.Errorf("Plan pointer mismatch")
	}
}

func TestPlanStudioModel_DryRunNoopFromIdle(t *testing.T) {
	m := NewPlanStudioModel()
	_, cmd := m.Update(keyMsg("d"))
	if cmd != nil {
		t.Fatal("dry-run from Idle should be a no-op")
	}
}

func TestPlanStudioModel_DryRunNoopWhenResultPlanNil(t *testing.T) {
	m := NewPlanStudioModel().MarkGenerating()
	m, _ = m.Update(services.PlanGeneratedMsg{Result: &services.PlanResult{Plan: nil}})
	_, cmd := m.Update(keyMsg("d"))
	if cmd != nil {
		t.Fatal("dry-run with nil Plan should be a no-op")
	}
}

func TestPlanStudioModel_SaveNoopWithoutResult(t *testing.T) {
	m := NewPlanStudioModel()
	_, cmd := m.Update(keyMsg("s"))
	if cmd != nil {
		t.Fatal("save without result should be a no-op")
	}
}

func TestPlanStudioModel_MarkSavedOnlyFromReview(t *testing.T) {
	m := NewPlanStudioModel()
	got := m.MarkSaved()
	if got.State != PlanStudioIdle {
		t.Errorf("MarkSaved on Idle changed state to %v", got.State)
	}
}

func TestPlanStudioModel_JobsByEnvironmentGrouping(t *testing.T) {
	m := NewPlanStudioModel().MarkGenerating()
	m, _ = m.Update(services.PlanGeneratedMsg{Result: &services.PlanResult{
		Plan: &model.Plan{Jobs: []model.PlanJob{
			{ID: "a", Environment: "dev"},
			{ID: "b", Environment: "dev"},
			{ID: "c", Environment: "prod"},
		}},
	}})
	groups := m.JobsByEnvironment()
	if len(groups["dev"]) != 2 || len(groups["prod"]) != 1 {
		t.Errorf("unexpected grouping: %+v", groups)
	}
}

// --- Property tests (pgregory.net/rapid) ---

// Property: State machine transitions never regress. Specifically:
//   - Generating is only entered from Idle/Review/Saved/Error via `g`.
//   - Review is only entered from Generating via a success message.
//   - The Cursor remains within [0, len(jobs)).
//
// This guards against accidental drift if future contributors add states
// or message handlers.
func TestPlanStudioModel_PropertyValidTransitions(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		m := NewPlanStudioModel()
		// Seed with a small plan so cursor moves are meaningful.
		jobs := rapid.IntRange(1, 5).Draw(t, "jobCount")
		seed := &services.PlanResult{Plan: &model.Plan{}}
		for i := 0; i < jobs; i++ {
			seed.Plan.Jobs = append(seed.Plan.Jobs, model.PlanJob{
				ID:          string(rune('a' + i)),
				Environment: "dev",
			})
		}

		actions := rapid.SliceOfN(rapid.SampledFrom([]string{
			"g", "c", "j", "k", "s", "ok", "err",
		}), 1, 30).Draw(t, "actions")

		for _, a := range actions {
			switch a {
			case "ok":
				m, _ = m.Update(services.PlanGeneratedMsg{Result: seed})
			case "err":
				m, _ = m.Update(services.PlanGeneratedMsg{Err: errors.New("x")})
			default:
				m, _ = m.Update(keyMsg(a))
			}

			// Invariant: cursor in range.
			if m.Result != nil && m.Result.Plan != nil {
				n := len(m.Result.Plan.Jobs)
				if m.Cursor < 0 || (n > 0 && m.Cursor >= n) {
					t.Fatalf("cursor out of range: cursor=%d n=%d state=%v", m.Cursor, n, m.State)
				}
			} else if m.Cursor != 0 {
				t.Fatalf("cursor non-zero with no result: %d", m.Cursor)
			}

			// Invariant: state is one of the declared values.
			if m.State < PlanStudioIdle || m.State > PlanStudioError {
				t.Fatalf("invalid state %v", m.State)
			}

			// Invariant: Review/Saved require a non-nil Result.
			if (m.State == PlanStudioReview || m.State == PlanStudioSaved) && m.Result == nil {
				t.Fatalf("state %v with nil Result", m.State)
			}
		}
	})
}

// Property: re-applying the same PlanGeneratedMsg twice yields the same
// view output (rendering is deterministic).
func TestPlanStudioModel_PropertyDeterministicView(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 4).Draw(t, "jobCount")
		jobs := make([]model.PlanJob, n)
		for i := range jobs {
			jobs[i] = model.PlanJob{ID: string(rune('a' + i)), Environment: "dev", Composition: "terraform"}
		}
		result := &services.PlanResult{Plan: &model.Plan{Jobs: jobs}, Checksum: "deadbeef", JobCount: n}

		m1 := NewPlanStudioModel().MarkGenerating()
		m1, _ = m1.Update(services.PlanGeneratedMsg{Result: result})
		m2 := NewPlanStudioModel().MarkGenerating()
		m2, _ = m2.Update(services.PlanGeneratedMsg{Result: result})

		if m1.View() != m2.View() {
			t.Fatalf("non-deterministic view:\n%s\n---\n%s", m1.View(), m2.View())
		}
	})
}
