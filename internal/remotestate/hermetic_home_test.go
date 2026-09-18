package remotestate

import (
	"os"
	"testing"
)

// Every test in this package runs with HOME in a throwaway directory, so the
// FILE credential store and ~/.orun/config.yaml are the test's own, not the
// developer's. (The keychain is kept out separately: cliauth refuses it to
// any test binary.) A test that needs a particular HOME still sets its own.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "orun-test-home-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	os.Setenv("XDG_CONFIG_HOME", home)
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}
