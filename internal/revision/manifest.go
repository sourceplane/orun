package revision

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sourceplane/orun/internal/statestore"
	"github.com/sourceplane/orun/internal/triggerctx"
)

// RevisionManifest is the persisted shape of revisions/<key>/manifest.json
// (data-model.md §4). It is a denormalized aggregator over revision.json,
// trigger.json, and the latest execution under the revision — every read
// path that wants a "one-stop summary" of a revision goes through this
// document so callers do not stitch the three separate files together
// themselves.
//
// Field order is the persisted order (data-model.md §4); the canonical
// JSON encoder emits struct fields in declaration order so this type's
// layout determines the byte-level output.
type RevisionManifest struct {
	APIVersion string                   `json:"apiVersion"`
	Kind       string                   `json:"kind"`
	Revision   ManifestRevision         `json:"revision"`
	Trigger    ManifestTrigger          `json:"trigger"`
	Source     triggerctx.TriggerSource `json:"source"`
	Summary    ManifestSummary          `json:"summary"`
	Objects    ManifestObjects          `json:"objects"`
}

// ManifestRevision is the per-manifest projection of revision.json.
type ManifestRevision struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	PlanHash  string `json:"planHash"`
	CreatedAt string `json:"createdAt"`
}

// ManifestTrigger is the per-manifest projection of trigger.json.
type ManifestTrigger struct {
	ID       string `json:"id"`
	Key      string `json:"key"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Event    string `json:"event"`
	Action   string `json:"action,omitempty"`
	Scope    string `json:"scope"`
}

// ManifestSummary is the latest-execution-aware summary embedded in the
// manifest. JobCount and ActiveEnvironments are populated from RevSummary
// at write time; LatestExecutionKey + LatestExecutionStatus are populated
// (or cleared) by UpdateLatestExecutionSummary as executions change state.
type ManifestSummary struct {
	JobCount              int      `json:"jobCount"`
	ActiveEnvironments    []string `json:"activeEnvironments"`
	LatestExecutionKey    string   `json:"latestExecutionKey,omitempty"`
	LatestExecutionStatus string   `json:"latestExecutionStatus,omitempty"`
}

// ManifestObjects is the catalogue of sibling files under the revision dir.
// Phase 1 advertises plan/trigger/revision; future objects (artifacts,
// snapshots) will append fields here without breaking compatibility.
type ManifestObjects struct {
	Plan     string `json:"plan"`
	Trigger  string `json:"trigger"`
	Revision string `json:"revision"`
}

// LatestExecutionSummary is the per-execution input to
// UpdateLatestExecutionSummary. Callers (executionstate writer) supply
// this when an execution under the revision changes terminal state. An
// empty Key clears both summary fields — useful when a future tool
// retracts the latest pointer.
type LatestExecutionSummary struct {
	Key    string
	Status string
}

// WriteManifest writes revisions/<rev.RevisionKey>/manifest.json by
// composing rev (revision.json data) + trig (trigger.json data) into a
// single denormalized RevisionManifest per data-model.md §4.
//
// The manifest is unconditionally overwritten via store.Write — the
// document is a derived projection, so any in-flight reader that observes
// either the old or the new bytes still sees a consistent snapshot. The
// per-execution latest pointer is updated separately via
// UpdateLatestExecutionSummary.
//
// cfg.Now and cfg.Store are honored exactly as in WriteRevision so callers
// can use a single Config instance for both calls. Returns an error
// wrapping the underlying statestore sentinel; this function does NOT
// introduce new sentinels.
func WriteManifest(
	ctx context.Context,
	cfg Config,
	rev PlanRevision,
	trig triggerctx.TriggerOccurrence,
) error {
	if cfg.Store == nil {
		return fmt.Errorf("%w: revision.Config.Store is nil", statestore.ErrInvalid)
	}
	if err := ValidateRevisionKey(rev.RevisionKey); err != nil {
		return err
	}
	if err := validateTrigger(trig); err != nil {
		return err
	}
	cfg = cfg.resolveDefaults()

	manifest := manifestFrom(rev, trig)
	path := statestore.ManifestPath(rev.RevisionKey)
	if _, err := cfg.Store.Write(ctx, path,
		marshalCanonicalJSON(manifest), statestore.WriteOptions{}); err != nil {
		return fmt.Errorf("write manifest.json: %w", err)
	}

	// Catalog-parent mirror (design.md §7 / C6) — additive, byte-identical
	// projection under sources/<srcKey>/catalogs/<catKey>/revisions/<revKey>/.
	// Only runs when a (source, catalog) pair was resolved; the Phase 1
	// manifest above is unaffected so the compat suite stays green.
	if cfg.CatalogParent.Active() {
		if err := writeCatalogParentManifest(ctx, cfg.Store, cfg.CatalogParent, manifest, rev.RevisionKey); err != nil {
			return err
		}
	}
	return nil
}

// UpdateLatestExecutionSummary mutates the manifest's summary.latestExecutionKey
// and summary.latestExecutionStatus fields via Read-modify-CAS. The dance is
// caller-owns-retry per state-store.md §6: on ErrConflict the helper re-reads
// and tries again, bounded by casRetryBudget. The mutation is idempotent —
// calling it twice with the same exec yields byte-identical bytes the second
// time, and CAS short-circuits when current == desired (the second write
// still produces a fresh ObjectMeta but the bytes do not change).
//
// Returns an error wrapping ErrNotFound when the manifest has not yet been
// written (callers should call WriteManifest first), ErrConflict when the
// retry budget is exhausted, or another statestore sentinel for transport
// errors. No new sentinels are introduced.
func UpdateLatestExecutionSummary(
	ctx context.Context,
	cfg Config,
	revKey string,
	exec LatestExecutionSummary,
) error {
	if cfg.Store == nil {
		return fmt.Errorf("%w: revision.Config.Store is nil", statestore.ErrInvalid)
	}
	if err := ValidateRevisionKey(revKey); err != nil {
		return err
	}
	store := cfg.Store
	path := statestore.ManifestPath(revKey)

	for attempt := 0; attempt < casRetryBudget; attempt++ {
		raw, meta, err := store.Read(ctx, path)
		if err != nil {
			return fmt.Errorf("read manifest.json: %w", err)
		}
		var current RevisionManifest
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&current); err != nil {
			return fmt.Errorf("%w: decode manifest.json: %v", statestore.ErrInvalid, err)
		}
		next := current
		next.Summary.LatestExecutionKey = exec.Key
		next.Summary.LatestExecutionStatus = exec.Status

		nextBytes := marshalCanonicalJSON(next)
		if bytes.Equal(raw, nextBytes) {
			// Already idempotent — desired state matches persisted state.
			return nil
		}
		_, err = store.CompareAndSwap(ctx, path, meta.Revision, nextBytes)
		if err == nil {
			return nil
		}
		if errors.Is(err, statestore.ErrConflict) {
			continue
		}
		return fmt.Errorf("cas manifest.json: %w", err)
	}
	return fmt.Errorf("%w: manifest CAS retry budget (%d) exhausted",
		statestore.ErrConflict, casRetryBudget)
}

// manifestFrom composes a RevisionManifest from a (rev, trig) pair without
// touching the store. Pulled out for testability and so resolver code that
// synthesizes a manifest in-memory (compat §3 branch 5) can reuse the
// projection rule.
func manifestFrom(rev PlanRevision, trig triggerctx.TriggerOccurrence) RevisionManifest {
	envs := append([]string(nil), rev.Summary.ActiveEnvironments...)
	if envs == nil {
		envs = []string{}
	}
	return RevisionManifest{
		APIVersion: APIVersion,
		Kind:       ManifestKind,
		Revision: ManifestRevision{
			ID:        rev.RevisionID,
			Key:       rev.RevisionKey,
			PlanHash:  rev.PlanHash,
			CreatedAt: rev.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		},
		Trigger: ManifestTrigger{
			ID:       trig.TriggerID,
			Key:      trig.TriggerKey,
			Type:     trig.TriggerType,
			Name:     trig.TriggerName,
			Provider: trig.Provider,
			Event:    trig.Event,
			Action:   trig.Action,
			Scope:    trig.PlanScope.Mode,
		},
		Source: trig.Source,
		Summary: ManifestSummary{
			JobCount:           rev.Summary.JobCount,
			ActiveEnvironments: envs,
		},
		Objects: ManifestObjects{
			Plan:     "plan.json",
			Trigger:  "trigger.json",
			Revision: "revision.json",
		},
	}
}
