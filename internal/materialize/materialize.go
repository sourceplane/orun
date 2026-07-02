// Package materialize is the deploy-time last mile of orun-secrets: it delivers
// already-resolved secret values into a deployed application's native store
// (v1: a Cloudflare Worker's secret bindings) under the same policy decision
// the job's resolve already made (specs/orun-secrets/runner-integration.md §6,
// design.md §8.3, SD-13).
//
// Invariant: a secret value lives ONLY inside an Adapter.Put call and the
// runner's existing resolved cache — it is never logged, never returned, and
// never embedded in an error produced here.
package materialize

import (
	"context"
	"fmt"
	"sort"
)

// SyncRecord is one materialization-provenance row for the SM5
// …/config/secrets/syncs route (data-model.md §7e, Invariant 10). It is
// value-free — the secret key, the version served, the target adapter, the
// provisioned entity ref, and the deploy run id. No value field exists.
type SyncRecord struct {
	SecretKey string
	Version   int
	Target    string
	EntityRef string
	RunID     string
}

// SyncRecorder records a SyncRecord to the backend. It is best-effort: a
// failure is surfaced but never undoes an already-written value.
type SyncRecorder func(ctx context.Context, rec SyncRecord) error

// TargetBinding carries what an adapter needs to address the deployed entity.
// For the cloudflare-worker adapter this is the Worker script name. It is
// derived from the provisioned entity / the deploy job — never a free-form
// endpoint (the admission bar, runner-integration.md §6).
type TargetBinding struct {
	// ScriptName is the Cloudflare Worker script the secret is written to.
	ScriptName string
	// EntityRef is the provisioned catalog entity the target maps to, carried
	// for provenance. Not required by every adapter.
	EntityRef string
}

// Adapter writes one secret into a deployed application's native store. Keep
// the value in memory only; never log it.
type Adapter interface {
	// Name is the target id this adapter serves (e.g. "cloudflare-worker").
	Name() string
	// Put writes (or overwrites — idempotent by key) one secret on the target.
	Put(ctx context.Context, target TargetBinding, key, value string) error
}

// Registry maps a target id → Adapter. It is injectable: cmd/orun constructs
// the concrete adapters (with the credentials the deploy job already has) and
// the runner only calls Put, staying decoupled from any cloud specifics. A nil
// Registry is safe — Lookup reports not-found.
type Registry struct {
	adapters map[string]Adapter
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{adapters: map[string]Adapter{}}
}

// Register adds an adapter under its Name(). A later registration for the same
// target id replaces the earlier one.
func (r *Registry) Register(a Adapter) {
	if r == nil || a == nil {
		return
	}
	if r.adapters == nil {
		r.adapters = map[string]Adapter{}
	}
	r.adapters[a.Name()] = a
}

// Lookup returns the adapter for a target id (nil-safe).
func (r *Registry) Lookup(target string) (Adapter, bool) {
	if r == nil {
		return nil, false
	}
	a, ok := r.adapters[target]
	return a, ok
}

// Targets returns the registered target ids, sorted (for diagnostics).
func (r *Registry) Targets() []string {
	if r == nil {
		return nil
	}
	out := make([]string, 0, len(r.adapters))
	for id := range r.adapters {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// UnknownTargetError names a target with no registered adapter, listing what is
// available so the failure is actionable (materialization failure must be loud,
// design §8.3).
func (r *Registry) UnknownTargetError(target string) error {
	return fmt.Errorf("no materialize adapter registered for target %q (available: %v)", target, r.Targets())
}
