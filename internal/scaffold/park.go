package scaffold

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sourceplane/orun/internal/actions"
)

// Parking a phase on a wait (orun-bootstrap-engine BE-O4).
//
// `Phase.Hooks.Await` is where a phase waits: for a convergence to finish, for
// a provider connection to be made. An action there may report `pending`, which
// is neither success nor failure, and the phase PARKS — the tree it placed
// stays valid and a later `--resume` picks up from here.
//
// This is what `internal/scaffold/blueprint.go` promised: "Approval gates +
// resumable pausing are a planned follow-on."

// RunStateRelPath is the parked-run cache. It is inside .orun, which every
// product gitignores, and **deleting it must change nothing** — a resume
// re-runs the await hook and learns the same answer from the world. It exists
// so a person can ask "what is this waiting on?" without re-running anything.
const RunStateRelPath = ".orun/run.state"

// RunState is the cache's content.
type RunState struct {
	Phase      string `json:"phase"`
	Hook       string `json:"hook"`
	Reason     string `json:"reason,omitempty"`
	RetryAfter string `json:"retryAfter,omitempty"`
	At         string `json:"at"`
}

// ParkedError reports a phase waiting rather than failing. It is deliberately
// NOT an ExitError with code 1: a parked bootstrap has not failed validation,
// and a caller that treats "waiting" as "broken" will tear down a product that
// is merely mid-deploy.
type ParkedError struct {
	Phase      string
	Hook       string
	Reason     string
	RetryAfter time.Duration
}

func (e *ParkedError) Error() string {
	msg := fmt.Sprintf("phase %q is waiting on %q", e.Phase, e.Hook)
	if e.Reason != "" {
		msg += ": " + e.Reason
	}
	if e.RetryAfter > 0 {
		msg += fmt.Sprintf(" (suggested retry in %s)", e.RetryAfter)
	}
	return msg + " — re-run with --resume to continue"
}

// ExitCode is EX_TEMPFAIL: try again later, nothing is wrong.
func (e *ParkedError) ExitCode() int { return 75 }

// runAwait runs a phase's await hooks, returning a ParkedError on the first one
// that reports pending.
func runAwait(ctx context.Context, hr *hookRunner, phase string, hooks []Hook) ([]string, error) {
	var ran []string
	for _, h := range hooks {
		err := hr.runAction(ctx, h)
		if err == nil {
			ran = append(ran, h.ID)
			continue
		}
		if pe, ok := actions.IsPending(err); ok {
			return ran, &ParkedError{Phase: phase, Hook: h.ID, Reason: pe.Reason, RetryAfter: pe.RetryAfter}
		}
		return ran, err
	}
	return ran, nil
}

// writeRunState records what a parked run is waiting on. Best effort: the cache
// is a convenience, and failing to write it must never turn a wait into a
// failure.
func writeRunState(outDir string, e *ParkedError) {
	st := RunState{Phase: e.Phase, Hook: e.Hook, Reason: e.Reason, At: time.Now().UTC().Format(time.RFC3339)}
	if e.RetryAfter > 0 {
		st.RetryAfter = e.RetryAfter.String()
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	path := filepath.Join(outDir, filepath.FromSlash(RunStateRelPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// clearRunState removes the cache once a phase is no longer waiting.
func clearRunState(outDir string) {
	_ = os.Remove(filepath.Join(outDir, filepath.FromSlash(RunStateRelPath)))
}

// ReadRunState returns what a parked run was waiting on, if anything. A missing
// or unreadable cache is "nothing parked" — never an error, because the cache
// is not the truth.
func ReadRunState(outDir string) (*RunState, bool) {
	data, err := os.ReadFile(filepath.Join(outDir, filepath.FromSlash(RunStateRelPath)))
	if err != nil {
		return nil, false
	}
	var st RunState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, false
	}
	return &st, true
}
