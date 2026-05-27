package main

import (
	"strings"
	"testing"
)

func TestPlanCommandAcceptsPositionalComponentArg(t *testing.T) {
	if planCmd.Args == nil {
		t.Fatal("expected plan command to have an Args validator")
	}
	if err := planCmd.Args(planCmd, []string{"my-component"}); err != nil {
		t.Fatalf("expected single positional arg to be accepted: %v", err)
	}
}

func TestPlanCommandRejectsTooManyArgs(t *testing.T) {
	if err := planCmd.Args(planCmd, []string{"comp-a", "comp-b"}); err == nil {
		t.Fatal("expected two positional args to be rejected")
	}
}

func TestPlanCommandUseSyntaxIncludesComponent(t *testing.T) {
	if !strings.Contains(planCmd.Use, "[component]") {
		t.Fatalf("expected planCmd.Use to contain [component], got %q", planCmd.Use)
	}
}

func TestPlanCommandRegistersComponentFlag(t *testing.T) {
	if planCmd.Flags().Lookup("component") == nil {
		t.Fatal("expected plan command to register --component flag")
	}
}

func TestPlanCommandRegistersArtifactFlag(t *testing.T) {
	f := planCmd.Flags().Lookup("artifact")
	if f == nil {
		t.Fatal("expected plan command to register --artifact flag")
	}
	if f.DefValue != "" {
		t.Errorf("default --artifact = %q, want empty", f.DefValue)
	}
}

func TestPlanCommandRegistersGithubOutputFlag(t *testing.T) {
	f := planCmd.Flags().Lookup("github-output")
	if f == nil {
		t.Fatal("expected plan command to register --github-output flag")
	}
	if f.DefValue != "false" {
		t.Errorf("default --github-output = %q, want false", f.DefValue)
	}
}

func TestPlanArtifactFlagAcceptsGithub(t *testing.T) {
	if err := planCmd.Flags().Set("artifact", "github"); err != nil {
		t.Fatalf("failed to set --artifact=github: %v", err)
	}
	if artifactBackend != "github" {
		t.Errorf("artifactBackend = %q, want %q", artifactBackend, "github")
	}
}

func TestPlanGithubOutputFlag(t *testing.T) {
	if err := planCmd.Flags().Set("github-output", "true"); err != nil {
		t.Fatalf("failed to set --github-output=true: %v", err)
	}
	if !githubOutput {
		t.Error("githubOutput should be true after setting --github-output=true")
	}
}
