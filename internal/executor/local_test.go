package executor

import (
	"strings"
	"testing"

	"github.com/sourceplane/arx/internal/model"
)

func TestLocalExecutorRunStepRejectsGitHubActionUseSteps(t *testing.T) {
	executor := &localExecutor{}
	_, err := executor.RunStep(ExecContext{}, model.PlanJob{}, model.PlanStep{
		Name: "setup-demo",
		Use:  "azure/setup-helm@v4.3.0",
	})
	if err == nil {
		t.Fatal("expected local executor to reject use steps")
	}
	if !strings.Contains(err.Error(), "--gha") {
		t.Fatalf("error = %q, want message to mention --gha", err)
	}
}
