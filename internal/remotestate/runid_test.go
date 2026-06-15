package remotestate_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/remotestate"
)

// ulidRE mirrors the platform's run-ULID validator (isRunUlid): Crockford
// base32, 26 chars, uppercase.
var ulidRE = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

func TestRunULID_ContractValidAndDeterministic(t *testing.T) {
	for _, execID := range []string{
		"local_18f3a_ab12cd",
		"gha_99887766_2",
		"my-explicit-id",
		"", // empty still yields a valid id
	} {
		id := remotestate.RunULID(execID)
		if !ulidRE.MatchString(id) {
			t.Errorf("RunULID(%q)=%q is not a contract-valid ULID", execID, id)
		}
		// Deterministic: the same execId always maps to the same wire id, which
		// is what makes idempotent create / crash resume work.
		if again := remotestate.RunULID(execID); again != id {
			t.Errorf("RunULID(%q) not deterministic: %q vs %q", execID, id, again)
		}
	}
}

func TestRunULID_DistinctExecIDsDiffer(t *testing.T) {
	a := remotestate.RunULID("local_aaa_111")
	b := remotestate.RunULID("local_bbb_222")
	if a == b {
		t.Errorf("distinct execIds collided: both %q", a)
	}
}

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
