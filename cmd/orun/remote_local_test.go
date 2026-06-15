package main

import (
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func TestLocalFlagForcesLocalState(t *testing.T) {
	// Save/restore the package-global flags the run command reads.
	origRemote, origLocal := runRemoteState, runLocal
	defer func() { runRemoteState, runLocal = origRemote, origLocal }()

	remoteIntent := &model.Intent{}
	remoteIntent.Execution.State.Mode = "remote"

	// Baseline: remote requested via flag, and via intent → active.
	runLocal = false
	runRemoteState = true
	if !isRemoteStateActive(nil) {
		t.Fatal("expected remote active with --remote-state")
	}
	runRemoteState = false
	if !isRemoteStateActive(remoteIntent) {
		t.Fatal("expected remote active with intent mode=remote")
	}

	// --local forces local even when remote is requested by flag or intent.
	runLocal = true
	runRemoteState = true
	if isRemoteStateActive(nil) {
		t.Error("--local must override --remote-state (force local)")
	}
	runRemoteState = false
	if isRemoteStateActive(remoteIntent) {
		t.Error("--local must override intent mode=remote (force local)")
	}
	// remoteStateRequested still reports the underlying request (so the bypass
	// note can fire).
	if !remoteStateRequested(remoteIntent) {
		t.Error("remoteStateRequested should ignore --local and report the request")
	}
}

func TestErrBackendUnreachableSuggestsLocal(t *testing.T) {
	err := errBackendUnreachable("https://api.example", errExampleCause{})
	msg := err.Error()
	for _, want := range []string{"unreachable", "https://api.example", "--local", "never silently"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing %q", msg, want)
		}
	}
}

type errExampleCause struct{}

func (errExampleCause) Error() string { return "dial tcp: no such host" }
