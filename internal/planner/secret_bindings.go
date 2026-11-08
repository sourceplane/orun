package planner

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/scoperef"
)

// resolveProfileBindings returns the composition secretBindings declared by the
// instance's execution profile, in stable (key-sorted) order. A component with
// no profile / no bindings yields nil (specs/orun-secrets/data-model.md §2.2).
func (jp *JobPlanner) resolveProfileBindings(compInst *model.ComponentInstance, info *CompositionInfo) []model.ResolvedSecretBinding {
	if info == nil || len(info.ExecutionProfiles) == 0 {
		return nil
	}
	if compInst.ProfileSource == "" || compInst.ProfileSource == "legacy-none" {
		return nil
	}
	profile, exists := info.ExecutionProfiles[compInst.ProfileName]
	if !exists || len(profile.SecretBindings) == 0 {
		return nil
	}

	keys := make([]string, 0, len(profile.SecretBindings))
	for k := range profile.SecretBindings {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]model.ResolvedSecretBinding, 0, len(keys))
	for _, key := range keys {
		b := profile.SecretBindings[key]
		asEnv := b.AsEnv
		if asEnv == "" {
			asEnv = key
		}
		out = append(out, model.ResolvedSecretBinding{Key: key, AsEnv: asEnv, Required: b.Required})
	}
	return out
}

// resolveProfileMaterialize returns the profile's materialize block for the
// instance, or nil when the component has no profile / no materialize
// (specs/orun-secrets/data-model.md §2.3).
func (jp *JobPlanner) resolveProfileMaterialize(compInst *model.ComponentInstance, info *CompositionInfo) *model.MaterializeSpec {
	if info == nil || len(info.ExecutionProfiles) == 0 {
		return nil
	}
	if compInst.ProfileSource == "" || compInst.ProfileSource == "legacy-none" {
		return nil
	}
	profile, exists := info.ExecutionProfiles[compInst.ProfileName]
	if !exists {
		return nil
	}
	return profile.Materialize
}

// resolveMaterialize compile-checks a profile's materialize block and resolves
// it onto a job (specs/orun-secrets/data-model.md §2.3, Part 1). Every key in
// materialize.secrets MUST be one of the profile's secretBindings or the
// component's secretEnv — else a clear compile error. Each authored key is
// translated to the env var name (AsEnv) the resolved value is keyed under, so
// the runner delivers by that name and writes it as the target's secret name.
// The result's Secrets are sorted for a stable, content-addressed plan.
func resolveMaterialize(spec *model.MaterializeSpec, bindings []model.ResolvedSecretBinding, secretEnv map[string]string) (*model.ResolvedMaterialize, error) {
	if spec == nil {
		return nil, nil
	}
	if strings.TrimSpace(spec.Target) == "" {
		return nil, fmt.Errorf("materialize block declares no target")
	}

	bindingAsEnv := make(map[string]string, len(bindings))
	for _, b := range bindings {
		bindingAsEnv[b.Key] = b.AsEnv
	}

	out := &model.ResolvedMaterialize{Target: spec.Target}
	for _, key := range spec.Secrets {
		if asEnv, ok := bindingAsEnv[key]; ok {
			out.Secrets = append(out.Secrets, asEnv)
			continue
		}
		if _, ok := secretEnv[key]; ok {
			out.Secrets = append(out.Secrets, key)
			continue
		}
		return nil, fmt.Errorf("materialize.secrets key %q is not one of the profile's secretBindings or the component's secretEnv", key)
	}
	sort.Strings(out.Secrets)
	return out, nil
}

// mergeBindingRefs folds the composition secretBindings into the job's secret
// references. Each binding synthesizes secret://<workspace>/<project>/<env>/<KEY>
// for the resolving scope and injects under its AsEnv. Rules
// (specs/orun-secrets/data-model.md §2.2, §5):
//
//   - A binding whose AsEnv already names a secretEnv reference is fine when the
//     references are identical; a disagreement is a compile error.
//   - A required binding that cannot be mapped (no workspace/project scope
//     resolvable at plan time) is a compile error naming the binding.
//   - An unmappable optional binding is dropped (best-effort).
//
// The returned map is a fresh copy — the caller's secretEnv map is never mutated
// (it is shared across a component's jobs).
func mergeBindingRefs(secretEnv map[string]string, bindings []model.ResolvedSecretBinding, workspace, project, env string) (map[string]string, error) {
	if len(secretEnv) == 0 && len(bindings) == 0 {
		return nil, nil
	}

	merged := make(map[string]string, len(secretEnv)+len(bindings))
	for k, v := range secretEnv {
		bound, ok, err := bindContext(v, workspace, project, env)
		if err != nil {
			return nil, fmt.Errorf("secretEnv %s: %w", k, err)
		}
		if !ok {
			// No scope to bind from (an unlinked repo planned offline): keep the
			// reference exactly as authored, as every CLI before SG3 did.
			bound = v
		}
		merged[k] = bound
	}

	for _, b := range bindings {
		if workspace == "" || project == "" {
			if b.Required {
				return nil, fmt.Errorf("secretBinding %q is required but cannot be mapped: no workspace/project scope is resolvable at plan time (declare execution.state.workspace/project or link the repo)", b.Key)
			}
			// Optional and unmappable: nothing to emit.
			continue
		}
		ref := scoperef.Ref{Scheme: scoperef.SchemeSecret, Workspace: workspace, Project: project, Env: env, Key: b.Key}
		refStr := ref.String()
		// Defensive: a malformed scope/key must not silently emit a bad ref.
		if _, err := scoperef.Parse(refStr); err != nil {
			if b.Required {
				return nil, fmt.Errorf("secretBinding %q is required but its reference is invalid: %w", b.Key, err)
			}
			continue
		}
		// Compare CANONICAL forms, not raw strings: after SG1 the deprecated
		// positional spelling and the named spelling denote the same reference,
		// and reporting them as a conflict would break every manifest that
		// pins a binding it also declares in secretEnv.
		if existing, ok := merged[b.AsEnv]; ok {
			if !sameRef(existing, refStr) {
				return nil, fmt.Errorf("secretBinding %q and secretEnv both bind %q to different references", b.Key, b.AsEnv)
			}
			// They agree. Keep the AUTHORED spelling rather than overwriting it
			// with the canonical one: plan.json is content-addressed, and a
			// manifest that did not change must not get a new digest just
			// because the binding path now renders references differently.
			continue
		}
		merged[b.AsEnv] = refStr
	}

	if len(merged) == 0 {
		return nil, nil
	}
	return merged, nil
}

// mergeOptionalRefs prepares a job's best-effort references from the
// component's optionalSecretEnv. The expander already leak-guarded the
// references and blocked component-level collisions; the one conflict only
// visible here is an env var name the HARD map (secretEnv ∪ profile
// bindings) also claims — one name cannot be fail-closed and best-effort at
// once, so that is a compile error naming the key.
func mergeOptionalRefs(optionalSecretEnv, hardRefs map[string]string, workspace, project, env string) (map[string]string, error) {
	if len(optionalSecretEnv) == 0 {
		return nil, nil
	}
	merged := make(map[string]string, len(optionalSecretEnv))
	for k, v := range optionalSecretEnv {
		if _, taken := hardRefs[k]; taken {
			return nil, fmt.Errorf("key %s is bound as both a required reference (secretEnv or a profile secretBinding) and an optionalSecretEnv reference — one env var cannot be fail-closed and best-effort at once", k)
		}
		bound, ok, err := bindContext(v, workspace, project, env)
		if err != nil {
			return nil, fmt.Errorf("optionalSecretEnv %s: %w", k, err)
		}
		if !ok {
			bound = v
		}
		merged[k] = bound
	}
	return merged, nil
}

// bindContext fills a secret reference's omitted workspace, project and env
// from the job's own scope (specs/orun-scope-engine SG3, design.md § 2), so one
// manifest line — `secret://DATABASE_URL`, or `secret://env:prod/WIRING_X` for
// a job that reads another environment's value — works in every workspace,
// repository and environment it is planned into.
//
// It never overrides an authored dimension and never widens: the server still
// receives a fully pinned reference and still verifies its workspace and
// project against the run. A reference that is already pinned is returned in
// its AUTHORED spelling, so a manifest that did not change keeps its plan
// digest. The component dimension is left as authored: the server derives it
// from the lease, and an emitted segment is only an assertion.
//
// ok is false when a dimension is omitted and the plan has none to give; the
// caller then keeps the reference as authored, exactly as before SG3, so a plan
// that compiles today never stops compiling. A value that is not a secret:// reference, or does not parse, is
// returned unchanged for the expander's guards to report.
func bindContext(raw, workspace, project, env string) (string, bool, error) {
	if !scoperef.IsSchemeRef(raw, scoperef.SchemeSecret) {
		return raw, true, nil
	}
	ref, err := scoperef.Parse(raw)
	if err != nil {
		return raw, true, nil
	}
	if ref.Workspace != "" && ref.Project != "" && ref.Env != "" {
		return raw, true, nil
	}
	if ref.Workspace == "" {
		ref.Workspace = workspace
	}
	if ref.Project == "" {
		ref.Project = project
	}
	if ref.Env == "" {
		ref.Env = env
	}
	if ref.Workspace == "" || ref.Project == "" || ref.Env == "" {
		return "", false, nil
	}
	bound := ref.String()
	if _, err := scoperef.Parse(bound); err != nil {
		return "", false, fmt.Errorf("binding the reference to the run's scope produced an invalid reference: %w", err)
	}
	return bound, true, nil
}

// sameRef reports whether two reference spellings denote the same reference.
// An unparseable side falls back to exact string equality — the caller has
// already validated the binding's own reference, and a malformed secretEnv
// entry is reported by the expander's leak guard, not here.
func sameRef(a, b string) bool {
	if a == b {
		return true
	}
	ca, err := scoperef.Canonicalize(a)
	if err != nil {
		return false
	}
	cb, err := scoperef.Canonicalize(b)
	if err != nil {
		return false
	}
	return ca == cb
}
