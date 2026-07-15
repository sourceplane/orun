package scaffold

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sourceplane/orun/internal/workflowbackend"
)

// hookRunner executes declared hooks after placement, outside the template
// sandbox (design §12). It runs two kinds:
//   - argv hooks: an explicit argv, no shell, run in the output directory (the
//     shipped minimal audited executor).
//   - workflow hooks: a torkflow workflow run through the workflow backend
//     (orun-workflows Surface B, WF4), with the blueprint's secret inputs
//     injected in-memory and the pinned digest re-verified before it runs.
//
// All hooks run AFTER the atomic write of the gated tree + provenance, so a hook
// failure leaves a valid tree in place and is re-runnable (orun-workflows §8).
type hookRunner struct {
	outDir  string
	baseDir string // blueprint dir — workflow hook references resolve against it
	// engine runs workflow hooks; resolved lazily from the environment when a
	// workflow hook is reached and none was injected.
	engine workflowbackend.Engine
	// credentials are the blueprint's secret inputs, injected in-memory (§6).
	credentials map[string]any
	// digests pins hookID → content digest (from provenance) for re-verification.
	digests map[string]string
}

// run executes a list of hooks in order, returning the ids that ran.
func (hr *hookRunner) run(ctx context.Context, hooks []Hook) ([]string, error) {
	var ran []string
	for _, h := range hooks {
		if h.IsWorkflow() {
			if err := hr.runWorkflow(ctx, h); err != nil {
				return ran, err
			}
		} else {
			if err := hr.runArgv(h); err != nil {
				return ran, err
			}
		}
		ran = append(ran, h.ID)
	}
	return ran, nil
}

// runArgv execs a hook's argv directly — no shell, so nothing is interpreted.
func (hr *hookRunner) runArgv(h Hook) error {
	if len(h.Run) == 0 {
		return fmt.Errorf("hook %q: empty run argv", h.ID)
	}
	cmd := exec.Command(h.Run[0], h.Run[1:]...) //nolint:gosec // declared argv, no shell, opt-in
	cmd.Dir = hr.outDir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("hook %q (%s): %w", h.ID, strings.Join(h.Run, " "), err)
	}
	return nil
}

// runWorkflow runs a workflow hook through the workflow backend, verifying the
// pinned digest first and failing on a non-success result (§8).
func (hr *hookRunner) runWorkflow(ctx context.Context, h Hook) error {
	eng, err := hr.workflowEngine()
	if err != nil {
		return fmt.Errorf("hook %q: %w", h.ID, err)
	}
	path := h.Workflow
	if !filepath.IsAbs(path) {
		path = filepath.Join(hr.baseDir, h.Workflow)
	}
	res, err := workflowbackend.RunStep(ctx, eng, workflowbackend.StepSpec{
		WorkflowPath:   path,
		ExpectedDigest: hr.digests[h.ID],
		With:           h.With,
		Credentials:    hr.credentials,
		Metadata:       map[string]any{"hook": h.ID, "workflowRef": h.Workflow},
	})
	if err != nil {
		return fmt.Errorf("hook %q: %w", h.ID, err)
	}
	if !res.Succeeded() {
		msg := res.Error
		if msg == "" {
			msg = "workflow reported status " + res.Status
		}
		// The gated tree is already written; report precisely and stay re-runnable.
		return fmt.Errorf("hook %q: scaffold succeeded but the hook workflow failed: %s", h.ID, msg)
	}
	return nil
}

// hookDigestMap indexes the provenance's pinned hook digests by hook id, for
// re-verification at execution time.
func hookDigestMap(prov Provenance) map[string]string {
	if len(prov.Hooks) == 0 {
		return nil
	}
	m := make(map[string]string, len(prov.Hooks))
	for _, h := range prov.Hooks {
		m[h.ID] = h.Digest
	}
	return m
}

func (hr *hookRunner) workflowEngine() (workflowbackend.Engine, error) {
	if hr.engine != nil {
		return hr.engine, nil
	}
	eng, err := workflowbackend.ResolveEngine(workflowbackend.EngineOptions{})
	if err != nil {
		return nil, err
	}
	hr.engine = eng
	return eng, nil
}
