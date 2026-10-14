package planner

import (
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func TestValidateLaneScopedStepsLead(t *testing.T) {
	ok := []model.RenderedStep{{Name: "install", Scope: "lane"}, {Name: "warm", Scope: "lane"}, {Name: "build"}}
	if err := validateLaneScopedStepsLead(ok); err != nil {
		t.Fatal(err)
	}
	bad := []model.RenderedStep{{Name: "setup"}, {Name: "install", Scope: "lane"}}
	if err := validateLaneScopedStepsLead(bad); err == nil || !strings.Contains(err.Error(), "must come before") {
		t.Fatalf("a lane step after a job step is refused: %v", err)
	}
	// Setup actions may precede the install they set up for.
	afterActions := []model.RenderedStep{{Name: "setup-node", Use: "actions/setup-node@v6"}, {Name: "setup-pnpm", Use: "pnpm/action-setup@v4"}, {Name: "install", Scope: "lane"}, {Name: "build"}}
	if err := validateLaneScopedStepsLead(afterActions); err != nil {
		t.Fatalf("a lane step after use: steps is allowed: %v", err)
	}
	afterRun := []model.RenderedStep{{Name: "setup-node", Use: "actions/setup-node@v6"}, {Name: "prep", Run: "true"}, {Name: "install", Scope: "lane"}}
	if err := validateLaneScopedStepsLead(afterRun); err == nil || !strings.Contains(err.Error(), `after "prep"`) {
		t.Fatalf("a lane step after a run step is still refused: %v", err)
	}
	if laneScope("Lane") != "lane" || laneScope("") != "" || laneScope("job") != "" {
		t.Fatal("laneScope normalises")
	}
}
