// Package approval implements human-in-the-loop gates for workflow steps
// (specs/orun-workflows-v2 WX7, design §9). A pause is a RUN FACT: the pending
// request and the decision are files under the workspace's .orun/approvals
// tree, sealed into the run record — never plan content. A plan with an
// approval declaration is byte-identical whether it is later approved or
// rejected (S-9).
package approval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Root is the approvals tree under the workspace .orun directory.
const Root = ".orun/approvals"

// ErrTimeout reports that no decision arrived within the declared window.
var ErrTimeout = errors.New("approval timed out")

// Request is a pending approval, written when a gated step pauses.
type Request struct {
	Prompt      string    `json:"prompt"`
	ExecID      string    `json:"execId"`
	JobID       string    `json:"jobId"`
	StepID      string    `json:"stepId"`
	RequestedAt time.Time `json:"requestedAt"`
}

// Decision resolves a pending approval.
type Decision struct {
	Approved  bool      `json:"approved"`
	By        string    `json:"by,omitempty"`
	DecidedAt time.Time `json:"decidedAt"`
	// OnTimeout records that the declared timeout policy decided, not a human.
	OnTimeout bool `json:"onTimeout,omitempty"`
}

func gateDir(workspace, execID, jobID, stepID string) string {
	return filepath.Join(workspace, filepath.FromSlash(Root), execID, sanitize(jobID), sanitize(stepID))
}

// Ask records a pending approval and returns its directory.
func Ask(workspace string, req Request) (string, error) {
	dir := gateDir(workspace, req.ExecID, req.JobID, req.StepID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return "", err
	}
	return dir, os.WriteFile(filepath.Join(dir, "pending.json"), data, 0o644)
}

// Await polls for a decision until timeout. On timeout it returns ErrTimeout —
// the caller applies the step's declared onTimeout policy; the policy verdict
// is then sealed via Seal like any human decision.
func Await(ctx context.Context, gateDirPath string, timeout, poll time.Duration) (Decision, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	deadline := time.Now().Add(timeout)
	for {
		if dec, ok := readDecision(gateDirPath); ok {
			return dec, nil
		}
		if time.Now().After(deadline) {
			return Decision{}, ErrTimeout
		}
		select {
		case <-ctx.Done():
			return Decision{}, ctx.Err()
		case <-time.After(poll):
		}
	}
}

// Seal writes the final decision (human or timeout-policy) next to the pending
// request, making the verdict part of the run's durable record.
func Seal(gateDirPath string, dec Decision) error {
	data, err := json.MarshalIndent(dec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(gateDirPath, "decision.json"), data, 0o644)
}

func readDecision(dir string) (Decision, bool) {
	data, err := os.ReadFile(filepath.Join(dir, "decision.json"))
	if err != nil {
		return Decision{}, false
	}
	var dec Decision
	if json.Unmarshal(data, &dec) != nil {
		return Decision{}, false
	}
	return dec, true
}

// Pending lists undecided requests under the workspace, newest first.
func Pending(workspace string) ([]Request, error) {
	root := filepath.Join(workspace, filepath.FromSlash(Root))
	var out []Request
	err := filepath.Walk(root, func(path string, info os.FileInfo, werr error) error {
		if werr != nil || info.IsDir() || filepath.Base(path) != "pending.json" {
			return nil //nolint:nilerr // absent tree = no pending approvals
		}
		if _, decided := readDecision(filepath.Dir(path)); decided {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		var req Request
		if json.Unmarshal(data, &req) == nil {
			out = append(out, req)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RequestedAt.After(out[j].RequestedAt) })
	return out, nil
}

// Decide resolves the newest pending approval matching jobID/stepID (across
// executions) — the `orun approve` path.
func Decide(workspace, jobID, stepID string, approved bool, by string) error {
	pending, err := Pending(workspace)
	if err != nil {
		return err
	}
	for _, req := range pending {
		if req.JobID == jobID && req.StepID == stepID {
			return Seal(gateDir(workspace, req.ExecID, req.JobID, req.StepID), Decision{
				Approved: approved, By: by, DecidedAt: time.Now().UTC(),
			})
		}
	}
	return fmt.Errorf("no pending approval for job %q step %q", jobID, stepID)
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.', r == '@':
			return r
		default:
			return '-'
		}
	}, s)
}
