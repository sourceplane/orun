package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/state"
)

func TestTuiCommand_RegisteredOnRoot(t *testing.T) {
	for _, sub := range rootCmd.Commands() {
		if sub.Name() == "tui" {
			return
		}
	}
	t.Fatal("expected `tui` subcommand to be registered on rootCmd")
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

	store := state.NewStore(t.TempDir())
	b, cleanup, err := resolveTUIBackend(store)
	if err != nil {
		t.Fatalf("local backend: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil local backend")
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

	store := state.NewStore(t.TempDir())
	_, _, err := resolveTUIBackend(store)
	if err == nil {
		t.Fatal("expected error when --remote-state is set without backend URL")
	}
	if !strings.Contains(err.Error(), "remote-state") {
		t.Errorf("error should mention --remote-state; got: %v", err)
	}
}
