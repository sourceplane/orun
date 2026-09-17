package main

import (
	"testing"

	"github.com/sourceplane/orun/internal/remotestate"
)

// The CLI and the hook actions must dial the SAME platform when nothing names
// one. They resolve through different code — resolveBackendURLWithConfig here,
// actions.backendURL there — so the only thing keeping them honest is that both
// end at one constant. A second copy would drift silently, and the symptom
// would be a bootstrap whose hooks talk to a different backend than the command
// that started them.
func TestTheCLIDefaultIsTheSharedOne(t *testing.T) {
	if defaultCloudURL != remotestate.DefaultCloudURL {
		t.Fatalf("defaultCloudURL = %q, but actions dial %q", defaultCloudURL, remotestate.DefaultCloudURL)
	}
}
