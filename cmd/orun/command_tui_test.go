package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestTuiCommand_RegisteredOnRoot(t *testing.T) {
	for _, sub := range rootCmd.Commands() {
		if sub.Name() == "tui" {
			return
		}
	}
	t.Fatal("expected `tui` subcommand to be registered on rootCmd")
}

func TestRootCommand_BareInvocationHasRunE(t *testing.T) {
	if rootCmd.RunE == nil {
		t.Fatal("rootCmd must have a RunE so a bare `orun` can open the TUI")
	}
}

func TestShouldLaunchDefaultTUI_RespectsNoTUIEnv(t *testing.T) {
	t.Setenv(noTUIEnvVar, "1")
	if shouldLaunchDefaultTUI() {
		t.Fatal("ORUN_NO_TUI=1 must suppress the default TUI")
	}
}

func TestShouldLaunchDefaultTUI_NonInteractiveFallsBackToHelp(t *testing.T) {
	t.Setenv(noTUIEnvVar, "")
	// `go test` stdout/stdin are not TTYs, so the bare invocation must NOT
	// launch the TUI — this is what keeps CI and pipes scriptable.
	if shouldLaunchDefaultTUI() {
		t.Fatal("non-interactive invocation must fall back to help, not the TUI")
	}
}

func TestRootCommand_BareInvocationPrintsHelpWhenSuppressed(t *testing.T) {
	t.Setenv(noTUIEnvVar, "1")
	buf := &bytes.Buffer{}
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	t.Cleanup(func() { rootCmd.SetOut(nil); rootCmd.SetErr(nil) })
	if err := rootCmd.RunE(rootCmd, []string{}); err != nil {
		t.Fatalf("bare RunE with TUI suppressed should print help, got: %v", err)
	}
	if !strings.Contains(buf.String(), cliName) {
		t.Errorf("expected help output to mention %q; got:\n%s", cliName, buf.String())
	}
}

func TestTuiCommand_HelpDoesNotLaunch(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"tui"})
	if err != nil {
		t.Fatalf("find tui: %v", err)
	}
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	if err := cmd.Help(); err != nil {
		t.Fatalf("help: %v", err)
	}
	if !strings.Contains(buf.String(), "Orun Cockpit") {
		t.Errorf("help missing Orun Cockpit description; got:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "remote-state") {
		t.Errorf("help missing --remote-state flag")
	}
}

func TestResolveTUIBackend_LocalDefault(t *testing.T) {
	prev := tuiRemoteState
	defer func() { tuiRemoteState = prev }()
	tuiRemoteState = false
	tuiBackendURL = ""

	// The local TUI reads the object graph directly: no state backend.
	b, cleanup, err := resolveTUIBackend()
	if err != nil {
		t.Fatalf("local backend: %v", err)
	}
	if b != nil {
		t.Fatal("expected nil backend for the local object-graph path")
	}
	if cleanup == nil {
		t.Fatal("expected non-nil cleanup")
	}
	cleanup()
}

func TestResolveTUIBackend_RemoteWithoutURL_Fails(t *testing.T) {
	prev := tuiRemoteState
	prevURL := tuiBackendURL
	prevEnv := os.Getenv(backendURLEnvVar)
	defer func() {
		tuiRemoteState = prev
		tuiBackendURL = prevURL
		os.Setenv(backendURLEnvVar, prevEnv)
	}()
	tuiRemoteState = true
	tuiBackendURL = ""
	os.Unsetenv(backendURLEnvVar)

	_, _, err := resolveTUIBackend()
	if err == nil {
		t.Fatal("expected error when --remote-state is set without backend URL")
	}
	if !strings.Contains(err.Error(), "remote-state") {
		t.Errorf("error should mention --remote-state; got: %v", err)
	}
}
