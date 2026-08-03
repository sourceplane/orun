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
	// The run is addressed on the wire by its contract run ULID, which the
	// backend derived from the CLI exec id (RunULID) and stored InitRun under.
	// Every coordination verb maps exec id → ULID via wireRunID before the wire
	// call; the lease-bound resolve route is ULID-gated identically, so apply the
	// SAME mapping here. Skipping it sends the raw exec id and the state-worker's
	// isRunUlid guard rejects the path as "Route not found".
	wireRunID := remotestate.RunULID(runID)
	return func(jobID string, refs []model.PlanSecretRef) (map[string]string, error) {
		refStrings := make([]string, 0, len(refs))
		optionalStrings := make([]string, 0)
		for _, ref := range refs {
			if ref.Optional {
				optionalStrings = append(optionalStrings, ref.Ref)
				continue
			}
			refStrings = append(refStrings, ref.Ref)
		}
		// The lease epoch is the conditional key from this job's claim (0 on
		// the relational path, which verifies runner_id + expiry instead).
		epoch := 0
		if cb, ok := backend.(*statebackend.CoordBackend); ok {
			epoch = cb.LeaseEpoch(jobID)
		}
		resolved, err := client.ResolveRunSecrets(ctx, wireRunID, jobID, runnerID, epoch, refStrings, optionalStrings)
		if err != nil {
			return nil, err
		}

		out := make(map[string]string, len(refs))
		for _, ref := range refs {
			parsed, perr := secretref.Parse(ref.Ref)
			if perr != nil {
				return nil, perr
			}
			// SE5-multi: prefer the env-grouped shape — the same key may be
			// served for several environments in one resolve (BF6 wiring), and
			// only the grouped map is collision-free. Fall back to the legacy
			// flat map for older backends.
			value, ok := "", false
			if byEnv := resolved.SecretsByEnv[parsed.Env]; byEnv != nil {
				value, ok = byEnv[parsed.Key]
			}
			if !ok {
				value, ok = resolved.Secrets[parsed.Key]
			}
			if !ok {
				// A best-effort reference whose key is not stored (yet) is
				// simply skipped — the wire-now-seed-later shape.
				if ref.Optional {
					continue
				}
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
			// Optional references skip silently when no override exists —
			// locally the same wire-now-seed-later posture as the backend.
			if ref.Optional {
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

// remoteJobOutputPublisher wires the SEC-JOB publish hook: a successful job's
// declared output secrets go over the run's lease-bound channel; the platform
// derives the target scope (project/env rung) from the leased job itself.
// Addressing mirrors remoteSecretResolver (exec id → contract run ULID).
func remoteJobOutputPublisher(ctx context.Context, client *remotestate.Client, backend statebackend.Backend, runID, runnerID string) func(string, map[string]string) error {
	wireRunID := remotestate.RunULID(runID)
	return func(jobID string, secrets map[string]string) error {
		epoch := 0
		if cb, ok := backend.(*statebackend.CoordBackend); ok {
			epoch = cb.LeaseEpoch(jobID)
		}
		return client.PublishJobOutputSecrets(ctx, wireRunID, jobID, runnerID, epoch, secrets)
	}
}
