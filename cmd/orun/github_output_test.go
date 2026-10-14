package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

// githubOutputHelperEnv, when set, turns TestGithubOutputHelper into a
// one-shot `orun plan --github-output` in the current directory.
const githubOutputHelperEnv = "ORUN_TEST_GITHUB_OUTPUT_HELPER"

// TestGithubOutputHelper is not a real test: the TestGithubOutput_* tests
// re-execute the test binary to run it, so every plan gets a fresh process
// and fresh CLI globals, as repeated CLI calls do.
func TestGithubOutputHelper(t *testing.T) {
	if os.Getenv(githubOutputHelperEnv) == "" {
		t.Skip("helper process for TestGithubOutput_*")
	}
	rootCmd.SetArgs([]string{"plan", "--github-output"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("orun plan: %v", err)
	}
}

// planExamplesWithGithubOutput runs `orun plan --github-output` on a copy of
// the examples/ fixture in a fresh process whose environment carries no
// GITHUB_* variable but those in env (NAME=VALUE), and returns the process
// output.
func planExamplesWithGithubOutput(t *testing.T, env ...string) string {
	t.Helper()
	src, err := filepath.Abs(filepath.Join("..", "..", "examples"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(src)); err != nil {
		t.Fatalf("copy examples fixture: %v", err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestGithubOutputHelper$", "-test.count=1")
	cmd.Dir = dir
	// The outputs must need nothing of GitHub Actions but $GITHUB_OUTPUT, so
	// whatever Actions set in this process is dropped and only env added.
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GITHUB_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, githubOutputHelperEnv+"=1")
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("orun plan --github-output: %v\n%s", err, out)
	}
	return string(out)
}

// --github-output writes its outputs on its own. A workflow whose run jobs
// take the plan from the platform passes no --artifact (the recipes in
// concepts/lanes.md), and the outputs used to be written only next to a plan
// artifact upload, so such a workflow's plan step produced none.
func TestGithubOutput_WrittenWithoutArtifactBackend(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "github_output")
	planExamplesWithGithubOutput(t, "GITHUB_OUTPUT="+outPath, "GITHUB_RUN_ID=42", "GITHUB_RUN_ATTEMPT=2")
	b, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("nothing written to $GITHUB_OUTPUT: %v", err)
	}
	got := string(b)
	for _, want := range []string{
		"matrix<<EOF\n{\"include\":[{",
		"lane-matrix<<EOF\n{\"include\":[{",
		"lane-count=",
		"lane-matrix-0<<EOF\n",
		"lane-matrix-levels<<EOF\n",
		"lane-level-count=",
		"plan_id=",
		"exec_id=gh-42-2-",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("$GITHUB_OUTPUT lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "plan_id=\n") || strings.Contains(got, "lane-count=0\n") {
		t.Errorf("the outputs are empty:\n%s", got)
	}
}

// With no $GITHUB_OUTPUT to write to, the plan succeeds and says so instead
// of staying silent.
func TestGithubOutput_WarnsWhenUnset(t *testing.T) {
	out := planExamplesWithGithubOutput(t)
	if !strings.Contains(out, "--github-output: $GITHUB_OUTPUT is not set") {
		t.Fatalf("no warning about $GITHUB_OUTPUT:\n%s", out)
	}
	if !strings.Contains(out, "✓ Plan") {
		t.Fatalf("the plan did not succeed:\n%s", out)
	}
}

// The outputs for a plan and for no plan (a CI event that matched no
// trigger) carry the same keys, so a workflow's guards read one shape.
func TestGithubOutputLines_SameKeysWithAndWithoutPlan(t *testing.T) {
	keys := func(lines []string) []string {
		var out []string
		for _, l := range lines {
			name, _, _ := strings.Cut(l, "<<EOF")
			if !strings.Contains(l, "<<EOF") {
				name, _, _ = strings.Cut(l, "=")
			}
			if !strings.HasPrefix(name, "lane-matrix-") || name == "lane-matrix-levels" {
				out = append(out, name)
			}
		}
		return out
	}
	none := keys(githubOutputLines(nil, "", ""))
	some := keys(githubOutputLines(&model.Plan{Jobs: []model.PlanJob{{ID: "a.dev", Component: "a", Environment: "dev"}}}, "abc", "gh-1-1-abc"))
	if strings.Join(none, ",") != strings.Join(some, ",") {
		t.Fatalf("keys differ:\n  no plan: %v\n  plan:    %v", none, some)
	}
}
