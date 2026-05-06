package runner

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/state"
	"github.com/sourceplane/orun/internal/ui"
)

func TestSummarizeUseOutputPrefersInstalledAndCacheMessages(t *testing.T) {
	t.Parallel()

	lines := []string{
		"Restoring 'v4.1.4' from cache",
		"Helm tool version 'v4.1.4' has been cached at /Users/test/.orun/tool-cache/helm/4.1.4/arm64/darwin-arm64/helm",
	}

	summary := summarizeUseOutput(lines)
	if len(summary) != 2 {
		t.Fatalf("len(summary) = %d, want 2", len(summary))
	}
	if summary[0] != "Installed helm v4.1.4" {
		t.Fatalf("summary[0] = %q, want %q", summary[0], "Installed helm v4.1.4")
	}
	if summary[1] != "Cached locally" {
		t.Fatalf("summary[1] = %q, want %q", summary[1], "Cached locally")
	}
}

func TestSplitDisplayLinesShortensAbsolutePaths(t *testing.T) {
	t.Parallel()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir() error = %v", err)
	}

	longPath := filepath.Join(homeDir, ".orun", "tool-cache", "helm", "4.1.4", "arm64", "darwin-arm64", "helm")
	lines := splitDisplayLines(fmt.Sprintf("%s\n", longPath))
	if len(lines) != 1 {
		t.Fatalf("len(lines) = %d, want 1", len(lines))
	}
	if got, want := lines[0], filepath.Join("~", ".orun", "tool-cache", "helm", "4.1.4")+string(filepath.Separator)+"..."+string(filepath.Separator)+"helm"; got != want {
		t.Fatalf("lines[0] = %q, want %q", got, want)
	}
}

func TestFormatCommandPreviewSplitsMultilineScripts(t *testing.T) {
	t.Parallel()

	lines := formatCommandPreview("cat $GITHUB_PATH\nhelm version --short\nwhich helm\n")
	if len(lines) != 3 {
		t.Fatalf("len(lines) = %d, want 3", len(lines))
	}
	if lines[1] != "helm version --short" {
		t.Fatalf("lines[1] = %q, want %q", lines[1], "helm version --short")
	}
}

func TestAnalyzeStepOutputPromotesLinksAndHeadline(t *testing.T) {
	t.Parallel()

	view := analyzeStepOutput(model.PlanStep{
		Name: "build",
		Run:  "printf 'Built web app\\nPreview URL: https://preview.example.dev\\n'",
	}, "Built web app\nPreview URL: https://preview.example.dev\n")

	if got, want := view.headline, "Built web app"; got != want {
		t.Fatalf("view.headline = %q, want %q", got, want)
	}
	if len(view.links) != 1 {
		t.Fatalf("len(view.links) = %d, want 1", len(view.links))
	}
	if got, want := view.links[0].Label, "Preview URL"; got != want {
		t.Fatalf("view.links[0].Label = %q, want %q", got, want)
	}
	if got, want := view.links[0].URL, "https://preview.example.dev"; got != want {
		t.Fatalf("view.links[0].URL = %q, want %q", got, want)
	}
}

func TestNewRunSummaryDedupesLinks(t *testing.T) {
	t.Parallel()

	summary := newRunSummary()
	summary.addLinks([]state.ExecutionLink{{Label: "Preview URL", URL: "https://preview.example.dev"}})
	summary.addLinks([]state.ExecutionLink{{Label: "Preview URL", URL: "https://preview.example.dev"}})

	snap := summary.snapshot()
	if len(snap.links) != 1 {
		t.Fatalf("len(snap.links) = %d, want 1", len(snap.links))
	}
}

func TestGHAJobHeaderMatrixModeDefaultsDepToCompleted(t *testing.T) {
	t.Parallel()

	var sink bytes.Buffer
	r := &Runner{
		gha:                 ui.NewGHARenderer(&sink),
		SkipLocalDepsForJob: true, // GHA matrix mode
	}

	job := model.PlanJob{
		ID:        "app@production.deploy",
		Name:      "deploy",
		DependsOn: []string{"infra@production.apply"},
		Steps:     []model.PlanStep{{}},
	}
	// Remote state has not propagated the dep's completed status yet.
	execState := &state.ExecState{Jobs: map[string]*state.JobState{}}

	r.ghaPrintJobHeader(job, execState)
	r.gha.FlushJob(job.ID)
	out := sink.String()

	if !strings.Contains(out, "✓ 1/1 ready") {
		t.Fatalf("expected dep to show completed in matrix mode, got:\n%s", out)
	}
	if strings.Contains(out, "● waiting") {
		t.Fatalf("unexpected 'waiting' in matrix mode header:\n%s", out)
	}
}

func TestGHAEmitStepSuccessGroupTitle(t *testing.T) {
	t.Parallel()

	var sink bytes.Buffer
	r := &Runner{gha: ui.NewGHARenderer(&sink)}

	job := model.PlanJob{ID: "api@prod.build", Name: "build"}
	r.ghaEmitStep(job, "compile", 1, "output line\n", true, 1500*time.Millisecond, nil, "compiled ok")
	out := sink.String()

	// Group title must include result icon, step number, name, duration, summary.
	if !strings.Contains(out, "::group::✓  01 compile  1.5s  ·  compiled ok") {
		t.Fatalf("expected result in group title, got:\n%s", out)
	}
	// Raw output inside the group.
	if !strings.Contains(out, "output line") {
		t.Fatalf("expected raw output inside group, got:\n%s", out)
	}
	// Group must close.
	if !strings.Contains(out, "::endgroup::") {
		t.Fatalf("expected endgroup marker, got:\n%s", out)
	}
	// No separate result line after the group.
	if strings.Contains(out, "✓ compile") {
		t.Fatalf("unexpected duplicate result line after group:\n%s", out)
	}
}

func TestGHAEmitStepFailureAnnotationOutsideGroup(t *testing.T) {
	t.Parallel()

	var sink bytes.Buffer
	r := &Runner{gha: ui.NewGHARenderer(&sink)}

	job := model.PlanJob{ID: "api@prod.build", Name: "build"}
	r.ghaEmitStep(job, "test", 2, "FAIL: assertion error\n", false, 800*time.Millisecond, fmt.Errorf("exit code 1"), "")
	out := sink.String()

	// Title contains failure icon and error.
	if !strings.Contains(out, "::group::✕  02 test") {
		t.Fatalf("expected failure group title, got:\n%s", out)
	}
	// Error annotation must appear AFTER ::endgroup:: (outside the group).
	endgroupIdx := strings.Index(out, "::endgroup::")
	annotationIdx := strings.Index(out, "::error::")
	if endgroupIdx < 0 || annotationIdx < 0 {
		t.Fatalf("missing endgroup or error annotation:\n%s", out)
	}
	if annotationIdx < endgroupIdx {
		t.Fatalf("::error:: annotation must appear after ::endgroup::, got:\n%s", out)
	}
}

func TestGHAJobHeaderShowsDependencyStatus(t *testing.T) {
	t.Parallel()

	var sink bytes.Buffer
	r := &Runner{gha: ui.NewGHARenderer(&sink)}

	job := model.PlanJob{
		ID:        "app@production.deploy",
		Name:      "deploy",
		Component: "app",
		DependsOn: []string{"network@production.apply", "iam@production.apply"},
		Steps:     []model.PlanStep{{}, {}},
	}
	execState := &state.ExecState{
		Jobs: map[string]*state.JobState{
			"network@production.apply": {Status: "completed"},
			"iam@production.apply":     {Status: "completed"},
		},
	}

	r.ghaPrintJobHeader(job, execState)
	r.gha.FlushJob(job.ID)
	out := sink.String()

	for _, want := range []string{
		"dependencies",
		"✓  network@production.apply",
		"✓  iam@production.apply",
		"✓ 2/2 ready",
		ghaSeparator,
		"steps",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in header output, got:\n%s", want, out)
		}
	}
}

func TestGHAJobHeaderShowsFailedDependency(t *testing.T) {
	t.Parallel()

	var sink bytes.Buffer
	r := &Runner{gha: ui.NewGHARenderer(&sink)}

	job := model.PlanJob{
		ID:        "app@production.deploy",
		Name:      "deploy",
		DependsOn: []string{"network@production.apply", "iam@production.apply"},
		Steps:     []model.PlanStep{{}},
	}
	execState := &state.ExecState{
		Jobs: map[string]*state.JobState{
			"network@production.apply": {Status: "completed"},
			"iam@production.apply":     {Status: "failed"},
		},
	}

	r.ghaPrintJobHeader(job, execState)
	r.gha.FlushJob(job.ID)
	out := sink.String()

	if !strings.Contains(out, "✕ dependency failed · iam@production.apply") {
		t.Fatalf("expected failed dep summary, got:\n%s", out)
	}
	if !strings.Contains(out, "✕  iam@production.apply") {
		t.Fatalf("expected failed dep entry, got:\n%s", out)
	}
}

func TestGHAJobHeaderNoDepsSection(t *testing.T) {
	t.Parallel()

	var sink bytes.Buffer
	r := &Runner{gha: ui.NewGHARenderer(&sink)}

	job := model.PlanJob{
		ID:    "standalone",
		Name:  "standalone",
		Steps: []model.PlanStep{{}},
	}

	r.ghaPrintJobHeader(job, &state.ExecState{Jobs: map[string]*state.JobState{}})
	r.gha.FlushJob(job.ID)
	out := sink.String()

	if strings.Contains(out, "dependencies") {
		t.Fatalf("expected no deps section for job without DependsOn, got:\n%s", out)
	}
}

func TestJobReportObserveStepDone(t *testing.T) {
	t.Parallel()

	jr := newJobReport(model.PlanJob{Steps: []model.PlanStep{{}, {}, {}, {}}}, false)

	jr.observeStepDone("init", true, false, 100*1e6)    // 100ms success
	jr.observeStepDone("build", true, false, 500*1e6)   // 500ms success (slowest)
	jr.observeStepDone("test", false, false, 200*1e6)   // 200ms failure
	jr.observeStepDone("publish", true, true, 0)        // skipped

	if jr.stepPassed != 2 {
		t.Fatalf("stepPassed = %d, want 2", jr.stepPassed)
	}
	if jr.stepFailed != 1 {
		t.Fatalf("stepFailed = %d, want 1", jr.stepFailed)
	}
	if jr.stepSkipped != 1 {
		t.Fatalf("stepSkipped = %d, want 1", jr.stepSkipped)
	}
	if jr.failedStep != "test" {
		t.Fatalf("failedStep = %q, want %q", jr.failedStep, "test")
	}
	if jr.slowestStep != "build" {
		t.Fatalf("slowestStep = %q, want %q", jr.slowestStep, "build")
	}
}
