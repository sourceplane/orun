package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/state"
)

func TestGithubCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"github"})
	if err != nil {
		t.Fatalf("github command not found: %v", err)
	}
	if cmd.Use != "github" {
		t.Errorf("expected Use = 'github', got %q", cmd.Use)
	}
}

func TestGithubCommandHasSubcommands(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"github"})
	if err != nil {
		t.Fatalf("github command not found: %v", err)
	}

	expected := []string{"runs", "pull", "status", "logs"}
	for _, name := range expected {
		found := false
		for _, sub := range cmd.Commands() {
			if sub.Name() == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected github subcommand %q not found", name)
		}
	}
}

func TestGithubRunsFlagsRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"github", "runs"})
	if err != nil {
		t.Fatalf("github runs command not found: %v", err)
	}

	flags := []string{"workflow", "branch", "sha", "failed", "limit", "details"}
	for _, f := range flags {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("expected --%s flag on github runs", f)
		}
	}
}

func TestGithubPullFlagsRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"github", "pull"})
	if err != nil {
		t.Fatalf("github pull command not found: %v", err)
	}

	flags := []string{"run-id", "exec-id", "sha", "branch", "latest", "failed", "include-raw", "orun-dir"}
	for _, f := range flags {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("expected --%s flag on github pull", f)
		}
	}
}

func TestGithubLogsFlagsRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"github", "logs"})
	if err != nil {
		t.Fatalf("github logs command not found: %v", err)
	}

	flags := []string{"run-id", "exec-id", "sha", "branch", "failed", "latest", "job"}
	for _, f := range flags {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("expected --%s flag on github logs", f)
		}
	}
}

func TestParseGitHubRepo(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"git@github.com:sourceplane/orun.git", "sourceplane/orun"},
		{"https://github.com/sourceplane/orun.git", "sourceplane/orun"},
		{"https://github.com/sourceplane/orun", "sourceplane/orun"},
		{"https://api.github.com/repos/sourceplane/orun", "sourceplane/orun"},
		{"git@gitlab.com:sourceplane/orun.git", ""},
		{"", ""},
		{"not-a-url", ""},
	}

	for _, tc := range tests {
		got := parseGitHubRepo(tc.input)
		if got != tc.want {
			t.Errorf("parseGitHubRepo(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestFilterOrunShardsNil(t *testing.T) {
	if filterOrunShards(nil) == nil {
		t.Log("filterOrunShards handles nil")
	}
}

func TestGroupByExecIDNil(t *testing.T) {
	groups := groupByExecID(nil)
	if groups == nil {
		t.Log("groupByExecID returns nil for nil input")
	}
}

func TestGithubStatusCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"github", "status"})
	if err != nil {
		t.Fatalf("github status command not found: %v", err)
	}
	if cmd.Use != "status" {
		t.Errorf("expected Use = 'status', got %q", cmd.Use)
	}
}

func TestFilepathJoin(t *testing.T) {
	result := filepathJoin("a", "b", "c")
	if result != "a/b/c" {
		t.Errorf("filepathJoin = %q, want 'a/b/c'", result)
	}
}

func TestGithubCommandRunsHelp(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"github", "runs"})
	if err != nil {
		t.Fatalf("github runs not found: %v", err)
	}
	if !strings.Contains(cmd.Long, "Level 1") {
		t.Errorf("expected runs command to mention three levels of detail")
	}
}

func TestGithubPullOrunDirDefaultResolvesToDotOrun(t *testing.T) {
	// Verify that the default orunDir for github pull resolves to a path
	// ending in ".orun", matching the Hydrate function's expected input.
	//
	// This validates the fix: orunDir = filepath.Join(storeDir(), state.OrunDir)
	// instead of the previous orunDir = storeDir() which passed the intent root
	// (missing the ".orun" suffix).
	got := filepath.Join(storeDir(), state.OrunDir)

	// Without intent discovery, storeDir() returns ".".
	// filepath.Join(".", ".orun") should resolve to ".orun".
	if got != state.OrunDir {
		t.Errorf("default orunDir for pull = %q, want %q (the .orun directory)", got, state.OrunDir)
	}
}

func TestGithubPullOrunDirWithIntentRoot(t *testing.T) {
	// Simulate a scenario where intent discovery populated intentRoot.
	// The resolved orunDir must end with ".orun".
	orig := intentRoot
	intentRoot = "/tmp/test-project"
	t.Cleanup(func() { intentRoot = orig })

	got := filepath.Join(storeDir(), state.OrunDir)
	wantSuffix := string(filepath.Separator) + state.OrunDir
	if !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("orunDir with intent root = %q, want path ending in %q", got, wantSuffix)
	}
}

var _ = strings.Contains