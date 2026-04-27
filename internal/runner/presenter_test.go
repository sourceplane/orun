package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/gluon/internal/model"
	"github.com/sourceplane/gluon/internal/state"
)

func TestSummarizeUseOutputPrefersInstalledAndCacheMessages(t *testing.T) {
	t.Parallel()

	lines := []string{
		"Restoring 'v4.1.4' from cache",
		"Helm tool version 'v4.1.4' has been cached at /Users/test/.gluon/tool-cache/helm/4.1.4/arm64/darwin-arm64/helm",
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

	longPath := filepath.Join(homeDir, ".gluon", "tool-cache", "helm", "4.1.4", "arm64", "darwin-arm64", "helm")
	lines := splitDisplayLines(fmt.Sprintf("%s\n", longPath))
	if len(lines) != 1 {
		t.Fatalf("len(lines) = %d, want 1", len(lines))
	}
	if got, want := lines[0], filepath.Join("~", ".gluon", "tool-cache", "helm", "4.1.4")+string(filepath.Separator)+"..."+string(filepath.Separator)+"helm"; got != want {
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
