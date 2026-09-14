package scaffold

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// hookRunner executes declared hooks after placement, outside the template
// sandbox (design §12). It runs two kinds:
//   - argv hooks: an explicit argv, no shell, run in the output directory (the
//     ecosystem escape).
//   - action hooks: a typed orun action resolved in-process against the closed
//     registry (orun-bootstrap-engine BE-O1), yielding outputs a later hook in
//     the same phase can read.
//
// All hooks run AFTER the atomic write of the gated tree + provenance, so a hook
// failure leaves a valid tree in place and is re-runnable (orun-workflows §8).
type hookRunner struct {
	outDir  string
	baseDir string // blueprint dir, for resolving relative references
	// actions runs a typed action. Injected so a test can substitute a
	// recording registry and assert the sequence a phase performs without a
	// network — which is also what a baseline's phase-simulation CI tier needs.
	actions ActionRunner
	// outputs accumulates each action hook's results, keyed by hook id, for
	// `{{ .hooks.<id>.outputs.<key> }}` in a later hook's `with:` block. Scoped
	// to one phase: see resetOutputs.
	outputs map[string]map[string]string
	// inputs are the blueprint's collected input values, SECRET-FREE — a
	// secret field reads as the literal "<secret>". A hook that needs a
	// credential gets it from the platform at resolve time (the brokered
	// path), never from an input rendered into an argv or a parameter.
	inputs map[string]any
	// phase names the phase whose hooks are running, for `{{ .phase.name }}`.
	// Empty outside a phase (the global postInstantiate list).
	phase Phase
}

// run executes a list of hooks in order, returning the ids that ran.
func (hr *hookRunner) run(ctx context.Context, hooks []Hook) ([]string, error) {
	var ran []string
	for _, h := range hooks {
		switch {
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
	out, err := hr.actions.Run(ctx, h.Uses, ActionInput{Dir: hr.outDir, BaseDir: hr.baseDir, Params: params})
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
	scope := hr.scope()
	out := make(map[string]any, len(h.With))
	for name, value := range h.With {
		rendered, err := hr.renderValue("hook."+h.ID+"."+name, value, scope)
		if err != nil {
			return nil, fmt.Errorf("hook %q parameter %q: %w", h.ID, name, err)
		}
		out[name] = rendered
	}
	return out, nil
}

// renderValue renders one parameter value. Strings carry expressions; LISTS OF
// STRINGS carry them element by element.
//
// The list case is not a generalization for its own sake. Every parameter a
// baseline needs to template is a list: the URLs a phase probes, the secret
// keys it requires. Those are the values that depend on what the operator
// answered — a product's health endpoint is its own repo name and its own
// workers subdomain — so a stringList that passed through unrendered would put
// the literal text `{{ .inputs.repoName }}` into an HTTP request and report it
// as a failed probe.
//
// Anything else passes through untouched: only text can carry an expression,
// and a number that looked like one would be a different bug.
func (hr *hookRunner) renderValue(where string, value any, scope map[string]any) (any, error) {
	switch v := value.(type) {
	case string:
		if !strings.Contains(v, "{{") {
			return value, nil
		}
		rendered, err := Render(where, v, scope)
		if err != nil {
			return nil, err
		}
		return string(rendered), nil
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			r, err := hr.renderValue(fmt.Sprintf("%s[%d]", where, i), item, scope)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	default:
		return value, nil
	}
}

// scope is what a hook's `with:` block can see.
//
// Three things, and the omission is as deliberate as the inclusions:
//
//   - `.inputs.<name>` — what the operator answered. Secret-free (a secret
//     field reads as "<secret>"), so a blueprint cannot route a credential
//     into a parameter by writing an expression.
//   - `.phase.name` / `.phase.title` — which phase is running, so a hook can
//     name it in a branch, a title or a milestone without the blueprint
//     repeating the phase name on every line.
//   - `.hooks.<id>.outputs.<key>` — what an earlier hook IN THIS PHASE
//     produced, also reachable as `.phase.hooks.<id>.outputs.<key>`.
//
// What is NOT here: the placed file set, the provenance lock, anything about
// an earlier phase. Templates run under missingkey=error, so reaching for one
// of those is a failure at the line that reached, not an empty string that
// travels into a parameter and means something else.
func (hr *hookRunner) scope() map[string]any {
	hooks := hookScope(hr.outputs)
	inputs := hr.inputs
	if inputs == nil {
		inputs = map[string]any{}
	}
	return map[string]any{
		"inputs": inputs,
		"hooks":  hooks,
		"phase": map[string]any{
			"name":  hr.phase.Name,
			"title": phaseTitle(&hr.phase, hr.phase.Name),
			"hooks": hooks,
		},
	}
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
