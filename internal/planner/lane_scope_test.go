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
	if laneScope("Lane") != "lane" || laneScope("") != "" || laneScope("job") != "" {
		t.Fatal("laneScope normalises")
	}
}
