package planner

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/secretref"
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
		merged[k] = v
	}

	for _, b := range bindings {
		if workspace == "" || project == "" {
			if b.Required {
				return nil, fmt.Errorf("secretBinding %q is required but cannot be mapped: no workspace/project scope is resolvable at plan time (declare execution.state.workspace/project or link the repo)", b.Key)
			}
			// Optional and unmappable: nothing to emit.
			continue
		}
		ref := secretref.Ref{Workspace: workspace, Project: project, Env: env, Key: b.Key}
		refStr := ref.String()
		// Defensive: a malformed scope/key must not silently emit a bad ref.
		if _, err := secretref.Parse(refStr); err != nil {
			if b.Required {
				return nil, fmt.Errorf("secretBinding %q is required but its reference is invalid: %w", b.Key, err)
			}
			continue
		}
		if existing, ok := merged[b.AsEnv]; ok && existing != refStr {
			return nil, fmt.Errorf("secretBinding %q and secretEnv both bind %q to different references", b.Key, b.AsEnv)
		}
		merged[b.AsEnv] = refStr
	}

	if len(merged) == 0 {
		return nil, nil
	}
	return merged, nil
}
