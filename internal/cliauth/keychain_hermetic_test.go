package cliauth

import (
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// A test binary must never probe, read, write or clear the real keychain:
// it does not follow $HOME, so a test's temp HOME does not isolate it.
func TestATestBinaryNeverReachesTheRealKeychain(t *testing.T) {
	t.Setenv("ORUN_CREDENTIAL_STORE", "")
	var calls []string
	prevExec, prevLook := execCommand, lookPath
	t.Cleanup(func() { execCommand, lookPath = prevExec, prevLook })
	execCommand = func(name string, args ...string) *exec.Cmd {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return exec.Command("true")
	}
	lookPath = func(string) (string, error) { return "/usr/bin/security", nil }
	keychainUsable = struct {
		once sync.Once
		ok   bool
	}{}

	if (&keychainCredentialStore{}).available() {
		t.Error("the keychain was available to a test binary")
	}
	if len(calls) != 0 {
		t.Errorf("a test binary ran the keychain probe: %v", calls)
	}
}

// Naming the keychain is still an explicit, deliberate opt-in.
func TestATestMayStillAskForTheKeychainByName(t *testing.T) {
	t.Setenv("ORUN_CREDENTIAL_STORE", "keychain")
	if got := (&keychainCredentialStore{}).available(); got != (runtime.GOOS == "darwin") {
		t.Errorf("available() = %v with the keychain named explicitly", got)
	}
}
