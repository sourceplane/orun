package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/runner"
	"github.com/sourceplane/orun/internal/secretref"
	"github.com/sourceplane/orun/internal/statebackend"
	"github.com/sourceplane/orun/internal/ui"
)

// remoteSecretResolver returns the ResolveJobSecrets hook for remote runs:
// the lease-bound resolve against the backend (contract §4 v3), mapping the
// response's KEY-keyed values back onto each ref's asEnv name and surfacing
// personal-overlay serves so local behavior is never silently different
// (specs/orun-secrets/runner-integration.md §1). It also records the value-free
// resolve provenance onto the runner so the sealed run captures which secret
// versions/decisions each job used (Invariant 6).
func remoteSecretResolver(ctx context.Context, r *runner.Runner, client *remotestate.Client, backend statebackend.Backend, runID, runnerID string, stderr *os.File, color bool) func(string, []model.PlanSecretRef) (map[string]string, error) {
	return func(jobID string, refs []model.PlanSecretRef) (map[string]string, error) {
		refStrings := make([]string, 0, len(refs))
		for _, ref := range refs {
			refStrings = append(refStrings, ref.Ref)
		}
		// The lease epoch is the conditional key from this job's claim (0 on
		// the relational path, which verifies runner_id + expiry instead).
		epoch := 0
		if cb, ok := backend.(*statebackend.CoordBackend); ok {
			epoch = cb.LeaseEpoch(jobID)
		}
		resolved, err := client.ResolveRunSecrets(ctx, runID, jobID, runnerID, epoch, refStrings)
		if err != nil {
			return nil, err
		}

		out := make(map[string]string, len(refs))
		for _, ref := range refs {
			parsed, perr := secretref.Parse(ref.Ref)
			if perr != nil {
				return nil, perr
			}
			value, ok := resolved.Secrets[parsed.Key]
			if !ok {
				return nil, fmt.Errorf("backend returned no value for %s", ref.AsEnv)
			}
			out[ref.AsEnv] = value
		}

		// Record value-free provenance for the seal (never a value): key, version,
		// serving scope, and the audit decision id.
		if r != nil && len(resolved.Resolved) > 0 {
			prov := make([]execmodel.SecretResolution, 0, len(resolved.Resolved))
			for _, meta := range resolved.Resolved {
				prov = append(prov, execmodel.SecretResolution{
					Key:        meta.Key,
					Version:    meta.Version,
					Scope:      meta.Scope,
					DecisionID: meta.DecisionID,
				})
			}
			r.RecordSecretProvenance(jobID, prov)
		}

		var personal []string
		for _, meta := range resolved.Resolved {
			if meta.Personal {
				personal = append(personal, meta.Key)
			}
		}
		if len(personal) > 0 {
			sort.Strings(personal)
			fmt.Fprintf(stderr, "  %s %d secret(s) personally overridden: %s\n",
				ui.Yellow(color, "⚑"), len(personal), strings.Join(personal, ", "))
		}
		return out, nil
	}
}

// attachLocalSecretResolver wires the local-run fallback (orun-secrets Q-1):
// with no backend, a secret reference resolves ONLY from an explicit
// ORUN_SECRET_<KEY> environment override on the developer's machine.
// Fail-closed — any reference without an override fails the job before its
// first step, naming exactly what is missing.
func attachLocalSecretResolver(r *runner.Runner) {
	if r.Hooks == nil {
		r.Hooks = &runner.RunnerHooks{}
	}
	r.Hooks.ResolveJobSecrets = func(jobID string, refs []model.PlanSecretRef) (map[string]string, error) {
		out := make(map[string]string, len(refs))
		var missing []string
		for _, ref := range refs {
			parsed, err := secretref.Parse(ref.Ref)
			if err != nil {
				return nil, err
			}
			override := "ORUN_SECRET_" + parsed.Key
			if value, ok := os.LookupEnv(override); ok {
				out[ref.AsEnv] = value
				continue
			}
			missing = append(missing, override)
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			return nil, fmt.Errorf("local runs resolve secrets only from ORUN_SECRET_<KEY> overrides; missing: %s (or run against Orun Cloud: orun auth login)", strings.Join(missing, ", "))
		}
		return out, nil
	}
}
