package scaffold

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sourceplane/orun/internal/flow"
)

// hookRunner executes declared hooks after placement, outside the template
// sandbox (design §12). It runs three kinds:
//   - argv hooks: an explicit argv, no shell, run in the output directory (the
//     ecosystem escape).
//   - action hooks: a typed orun action resolved in-process against the closed
//     registry (orun-bootstrap-engine BE-O1), yielding outputs a later hook in
//     the same phase can read.
//   - workflow hooks: an orun workflow run through the in-process flow engine
//     (orun-workflows-v3 WA5 — no external engine), with the blueprint's secret
//     inputs injected in-memory and the pinned digest re-verified before it runs.
//     Retired by BE-O8.
//
// All hooks run AFTER the atomic write of the gated tree + provenance, so a hook
// failure leaves a valid tree in place and is re-runnable (orun-workflows §8).
type hookRunner struct {
	outDir  string
	baseDir string // blueprint dir — workflow hook references resolve against it
	// actions runs a typed action. Injected so a test can substitute a
	// recording registry and assert the sequence a phase performs without a
	// network — which is also what a baseline's phase-simulation CI tier needs.
	actions ActionRunner
	// outputs accumulates each action hook's results, keyed by hook id, for
	// `{{ .hooks.<id>.outputs.<key> }}` in a later hook's `with:` block. Scoped
	// to one phase: see resetOutputs.
	outputs map[string]map[string]string
	// secrets are the blueprint's secret inputs (name → value), the pool the
	// per-hook connections grant draws from. Only granted inputs are injected
	// (design §9); the pool itself never crosses wholesale.
	secrets map[string]any
	// digests pins hookID → content digest (from provenance) for re-verification.
	digests map[string]string
}

// run executes a list of hooks in order, returning the ids that ran.
func (hr *hookRunner) run(ctx context.Context, hooks []Hook) ([]string, error) {
	var ran []string
	for _, h := range hooks {
		switch {
		case h.IsWorkflow():
			if err := hr.runWorkflow(ctx, h); err != nil {
				return ran, err
			}
		case h.IsAction():
			if err := hr.runAction(ctx, h); err != nil {
				return ran, err
			}
		default:
			if err := hr.runArgv(h); err != nil {
				return ran, err
			}
		}
		ran = append(ran, h.ID)
	}
	return ran, nil
}

// resetOutputs clears the per-phase output scope.
//
// Outputs are readable within a phase and not across phases, deliberately. A
// phase is the unit that can be run alone, months later, in a fresh container
// (BE-O2); a hook that could read a previous phase's output would be reaching
// for something that, on a resumed run, was produced by a process that no
// longer exists. Cross-phase facts belong in the product repo or the platform,
// which is where the derivation rule already puts them.
func (hr *hookRunner) resetOutputs() {
	hr.outputs = map[string]map[string]string{}
}

// runAction resolves a hook's parameters — rendering any template expression
// against the outputs already recorded in this phase — and runs the action.
func (hr *hookRunner) runAction(ctx context.Context, h Hook) error {
	params, err := hr.resolveWith(h)
	if err != nil {
		return err
	}
	if hr.actions == nil {
		return fmt.Errorf("hook %q: no action runner configured", h.ID)
	}
	out, err := hr.actions.Run(ctx, h.Uses, ActionInput{Dir: hr.outDir, Params: params})
	if err != nil {
		return fmt.Errorf("hook %q (%s): %w", h.ID, h.Uses, err)
	}
	if hr.outputs == nil {
		hr.resetOutputs()
	}
	hr.outputs[h.ID] = out
	return nil
}

// resolveWith renders every string parameter through the constrained template
// engine, with the phase's hook outputs in scope. Non-string values pass
// through untouched — only text can carry an expression.
func (hr *hookRunner) resolveWith(h Hook) (map[string]any, error) {
	if len(h.With) == 0 {
		return nil, nil
	}
	scope := map[string]any{"hooks": hookScope(hr.outputs)}
	out := make(map[string]any, len(h.With))
	for name, value := range h.With {
		s, ok := value.(string)
		if !ok || !strings.Contains(s, "{{") {
			out[name] = value
			continue
		}
		rendered, err := Render("hook."+h.ID+"."+name, s, scope)
		if err != nil {
			return nil, fmt.Errorf("hook %q parameter %q: %w", h.ID, name, err)
		}
		out[name] = string(rendered)
	}
	return out, nil
}

// hookScope shapes recorded outputs as `.hooks.<id>.outputs.<key>`. The extra
// `outputs` level is not ceremony: it leaves room for a hook to expose
// something other than outputs later without changing every blueprint that
// reads one.
func hookScope(outputs map[string]map[string]string) map[string]any {
	scope := make(map[string]any, len(outputs))
	for id, out := range outputs {
		vals := make(map[string]string, len(out))
		for k, v := range out {
			vals[k] = v
		}
		scope[id] = map[string]any{"outputs": vals}
	}
	return scope
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

// runWorkflow runs a workflow hook through the in-process flow engine,
// verifying the pinned digest first and failing on a non-success result (§8).
func (hr *hookRunner) runWorkflow(ctx context.Context, h Hook) error {
	path := h.Workflow
	if !filepath.IsAbs(path) {
		path = filepath.Join(hr.baseDir, h.Workflow)
	}
	// Digest re-verification against the provenance pin: the file must be the
	// one whose digest was sealed — fail-closed integrity (§8).
	pinned := hr.digests[h.ID]
	if pinned != "" {
		onDisk, err := flow.Digest(path)
		if err != nil {
			return fmt.Errorf("hook %q: %w", h.ID, err)
		}
		if onDisk != pinned {
			return fmt.Errorf("hook %q: workflow %s changed since it was pinned: on-disk %s != pinned %s", h.ID, path, onDisk, pinned)
		}
	}
	wf, err := flow.Load(path)
	if err != nil {
		return fmt.Errorf("hook %q: %w", h.ID, err)
	}
	// Materialize the hook's grant: only mapped secret inputs are injected,
	// keyed by the workflow's own connection names (design §9).
	connections, err := hr.connectionPayloads(h)
	if err != nil {
		return err
	}
	res, err := flow.Run(ctx, wf, flow.RunOptions{
		Dir:         filepath.Dir(path),
		Inputs:      h.With,
		Connections: connections,
		RunRoot:     filepath.Join(hr.outDir, ".orun", "wfruns"),
		Digest:      pinned,
	})
	if err != nil {
		return fmt.Errorf("hook %q: %w", h.ID, err)
	}
	if res.Status != "succeeded" {
		var reasons []string
		for name, st := range res.Steps {
			if st.Status == "failed" && st.Error != "" {
				reasons = append(reasons, fmt.Sprintf("%s: %s", name, st.Error))
			}
		}
		msg := strings.Join(reasons, "; ")
		if msg == "" {
			msg = "workflow reported status " + res.Status
		}
		// The gated tree is already written; report precisely and stay re-runnable.
		return fmt.Errorf("hook %q: scaffold succeeded but the hook workflow failed: %s", h.ID, msg)
	}
	return nil
}

// validateHookGrants enforces the connections grant for every workflow hook
// before placement (design §9): the workflow file must fully validate, its
// declared connections must be covered exactly, and every granted input must be
// a declared secret: true blueprint input.
func validateHookGrants(bp *Blueprint, baseDir string) error {
	check := func(h Hook) error {
		if !h.IsWorkflow() {
			return nil
		}
		path := h.Workflow
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, h.Workflow)
		}
		wf, err := flow.Load(path)
		if err != nil {
			return fmt.Errorf("hook %q: workflow %q: %w", h.ID, h.Workflow, err)
		}
		if err := flow.ValidateGrant("hook "+h.ID, wf.ConnectionNames(), h.Connections); err != nil {
			return err
		}
		for conn, fields := range h.Connections {
			for field, inputName := range fields {
				spec, ok := bp.Inputs[inputName]
				if !ok {
					return fmt.Errorf("hook %q connection %q field %q references undeclared input %q", h.ID, conn, field, inputName)
				}
				if !spec.Secret {
					return fmt.Errorf("hook %q connection %q field %q references input %q, which is not declared secret: true — credentials must come from secret inputs", h.ID, conn, field, inputName)
				}
			}
		}
		return nil
	}
	for _, ph := range bp.Phases {
		for _, h := range ph.Hooks {
			if err := check(h); err != nil {
				return err
			}
		}
	}
	for _, h := range bp.Hooks.PostInstantiate {
		if err := check(h); err != nil {
			return err
		}
	}
	return nil
}

// connectionPayloads materializes a hook's connections grant from the
// blueprint's secret inputs: payload[field] = the granted input's value. A grant
// naming an uncollected input is an error, fail-closed.
func (hr *hookRunner) connectionPayloads(h Hook) (map[string]map[string]string, error) {
	if len(h.Connections) == 0 {
		return nil, nil
	}
	out := make(map[string]map[string]string, len(h.Connections))
	for conn, fields := range h.Connections {
		payload := make(map[string]string, len(fields))
		for field, inputName := range fields {
			value, ok := hr.secrets[inputName]
			if !ok {
				return nil, fmt.Errorf("hook %q connection %q field %q references input %q, which is not a collected secret input", h.ID, conn, field, inputName)
			}
			payload[field] = fmt.Sprint(value)
		}
		out[conn] = payload
	}
	return out, nil
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
