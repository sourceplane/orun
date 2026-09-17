package actions

import (
	"testing"

	"github.com/sourceplane/orun/internal/remotestate"
)

// THE PARAMETER WINS. A blueprint that names its backend is naming it for a
// reason — a self-hosted platform, a stage pin — and neither the environment
// nor a default may quietly redirect it.
func TestBackendURLPrefersTheParameter(t *testing.T) {
	t.Setenv("ORUN_BACKEND_URL", "https://env.example.com")
	in := Input{Params: map[string]any{"backendUrl": "https://declared.example.com"}}
	if got := backendURL(in); got != "https://declared.example.com" {
		t.Fatalf("backendURL = %q, want the declared parameter", got)
	}
}

func TestBackendURLThenTheEnvironment(t *testing.T) {
	t.Setenv("ORUN_BACKEND_URL", "https://env.example.com")
	if got := backendURL(Input{}); got != "https://env.example.com" {
		t.Fatalf("backendURL = %q, want the environment", got)
	}
}

// THE CASE THAT BROKE A BOOTSTRAP. The command layer grew a default backend
// and the actions kept refusing, so `orun baseline new` reached the platform
// and the hooks it then ran did not:
//
//	✕ phase "03-infrastructure" precondition "providers" is not met: hook
//	  "providers" (orun.doctor/check@v1): no backend URL: pass `backendUrl`
//	  or set ORUN_BACKEND_URL
//
// A hook started by a CLI that reached the platform must reach the same one.
func TestBackendURLFallsToTheSameDefaultTheCLIDials(t *testing.T) {
	t.Setenv("ORUN_BACKEND_URL", "")
	got := backendURL(Input{})
	if got != remotestate.DefaultCloudURL {
		t.Fatalf("backendURL = %q, want the shared default %q", got, remotestate.DefaultCloudURL)
	}
	if got == "" {
		t.Fatal("the chain has no last rung")
	}
}

// Exported but blank is not a backend — it must not shadow the default.
func TestBackendURLIgnoresBlanks(t *testing.T) {
	t.Setenv("ORUN_BACKEND_URL", "   ")
	if got := backendURL(Input{Params: map[string]any{"backendUrl": "  "}}); got != remotestate.DefaultCloudURL {
		t.Fatalf("a blank shadowed the default: %q", got)
	}
}
