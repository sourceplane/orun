package remotestate_test

import (
	"os"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/remotestate"
)

func TestDeriveRunID_Explicit(t *testing.T) {
	id := remotestate.DeriveRunID("my-explicit-id")
	if id != "my-explicit-id" {
		t.Errorf("expected my-explicit-id, got %q", id)
	}
}

func TestDeriveRunID_GitHubActions(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_RUN_ID", "99887766")
	t.Setenv("GITHUB_RUN_ATTEMPT", "2")

	id := remotestate.DeriveRunID("")
	expected := "gha_99887766_2"
	if id != expected {
		t.Errorf("expected %q, got %q", expected, id)
	}
}

func TestDeriveRunID_GitHubActionsDefaultAttempt(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_RUN_ID", "12345")
	os.Unsetenv("GITHUB_RUN_ATTEMPT")

	id := remotestate.DeriveRunID("")
	expected := "gha_12345_1"
	if id != expected {
		t.Errorf("expected %q, got %q", expected, id)
	}
}

func TestDeriveRunID_LocalFallback(t *testing.T) {
	os.Unsetenv("GITHUB_ACTIONS")
	os.Unsetenv("GITHUB_RUN_ID")

	id := remotestate.DeriveRunID("")
	if !strings.HasPrefix(id, "local_") {
		t.Errorf("expected local_<ts>_<hex>, got %q", id)
	}
	parts := strings.SplitN(id, "_", 3)
	if len(parts) != 3 {
		t.Errorf("expected 3 underscore-separated parts, got %q", id)
	}
}
