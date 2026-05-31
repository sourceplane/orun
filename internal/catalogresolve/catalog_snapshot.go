package catalogresolve

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// ResolverInputs carries the caller-supplied facts the resolver cannot
// invent. Per Task 0028 §Constraints, these come from the call site —
// typically `internal/sourcectx.ResolveSourceSnapshot` outputs plus the
// caller's policy decisions on `authoritative` / `preview`.
//
// Every field below is REQUIRED for BuildCatalog. Missing fields produce
// a typed ErrResolverInputsIncomplete error.
type ResolverInputs struct {
	// OrunVersion is the orun binary version stamped on the snapshot's
	// `resolver.orunVersion` field (e.g. "0.18.0").
	OrunVersion string

	// SchemaVersion is the catalog schema version (e.g. "orun.io/v1alpha1").
	SchemaVersion string

	// ResolverVersion is the integer resolver version from
	// identity-and-keys.md §9 — feeds catalogHash.
	ResolverVersion int

	// StackSources is the list of composition stack sources resolved at
	// stage 4 (e.g. ["ghcr.io/sourceplane/stack-tectonic:0.12.0"]).
	// May be empty but must be non-nil.
	StackSources []string

	// SourceSnapshotKey is the key of the SourceSnapshot this catalog
	// was resolved against. From sourcectx.WorkspaceState.
	SourceSnapshotKey string

	// CatalogInputHash is the dirty-hash inputs hash per
	// identity-and-keys.md §1. From sourcectx.CatalogInputHash.
	CatalogInputHash string

	// Repo is the human-readable repo (e.g. "sourceplane/orun"); may
	// differ from the componentKey repo segment when a workspace is
	// cloned under a different name.
	Repo string

	// SourceScope is one of {branch-main, branch-protected, branch-feature,
	// pr, tag, local-dirty, local-nogit, ci-event}.
	SourceScope string

	// HeadRevision is the 12+ char SHA, or "" for local-nogit.
	HeadRevision string

	// TreeHash is the 7+ char tree hash, or "" for local-nogit.
	TreeHash string

	// WorkingTree is "clean" or "dirty".
	WorkingTree string

	// Authoritative is the catalog-of-record flag. Caller computes per
	// data-model.md §2 validation rule: authoritative = true iff
	// (sourceScope ∈ canonicalBranches) AND (workingTree = clean).
	Authoritative bool

	// Preview must equal !Authoritative per data-model.md §2.
	Preview bool

	// CreatedAt is the RFC 3339 / Z timestamp the catalog was assembled.
	// Caller-supplied so the resolver remains deterministic; tests pin
	// this for golden-file comparisons.
	CreatedAt string
}

// ErrResolverInputsIncomplete is returned by BuildCatalog when one or
// more ResolverInputs fields are unset.
type ErrResolverInputsIncomplete struct {
	MissingFields []string
}

func (e *ErrResolverInputsIncomplete) Error() string {
	return fmt.Sprintf("catalogresolve: ResolverInputs incomplete: missing %v", e.MissingFields)
}

// CatalogView is the BuildCatalog output: the existing ResolvedCatalog
// (stages 1–10) plus the post-resolution graph and snapshot views
// (stages 11–13). Pure data — no FS side effects.
type CatalogView struct {
	*ResolvedCatalog
	Snapshot *catalogmodel.CatalogSnapshot
	Graphs   []*catalogmodel.CatalogGraph
}

// BuildCatalog is the C3 entrypoint: runs Resolve (stages 1–10) and then
// stages 11–13 on top to produce the snapshot+graph view. Existing
// Resolve callers continue to work unchanged.
//
// Determinism contract: two consecutive BuildCatalog calls on the same
// inputs produce byte-identical canonical-encoded
// (*CatalogSnapshot, []*CatalogGraph). T-IDK-1 covers catalogHash
// determinism across 1000 random orderings of the manifest input set.
func BuildCatalog(ctx context.Context, opts Options, inputs ResolverInputs) (*CatalogView, []ValidationIssue, error) {
	if err := validateResolverInputs(inputs); err != nil {
		return nil, nil, err
	}

	rc, issues, err := Resolve(ctx, opts)
	if err != nil {
		// Surface the error but include the partial view if Resolve
		// returned a non-nil ResolvedCatalog (matches Resolve's own
		// behaviour for first-error abort).
		if rc != nil {
			return &CatalogView{ResolvedCatalog: rc}, issues, err
		}
		return nil, issues, err
	}

	// Stage 11 — graph construction. SnapshotKey is unknown at this point
	// (it is derived from catalogHash); pass empty and back-fill below.
	graphs := buildGraphs(rc.Manifests, inputs.SourceSnapshotKey, "")

	// Stage 12 — catalogHash.
	hash, err := catalogHash(inputs.CatalogInputHash, rc.Manifests, graphs, inputs.ResolverVersion)
	if err != nil {
		return nil, issues, err
	}

	// Derive catalogSnapshotKey from the hash short prefix. Width 8 is
	// the recommended starting width per identity-and-keys.md §3
	// (`catalogHashShort` starts at 8). Collision policy (the `-x<n>`
	// suffix) is the C4 writer's job.
	snapshotKey := catalogmodel.FormatCatalogSnapshotKey(hash, 8)
	if err := catalogmodel.ValidateCatalogSnapshotKey(snapshotKey); err != nil {
		return nil, issues, &ErrResolverInternal{Stage: 12, Underlying: err}
	}
	stampCatalogSnapshotKey(graphs, inputs.SourceSnapshotKey, snapshotKey)

	// Stage 13 — assemble snapshot.
	snap := assembleSnapshot(rc.Manifests, graphs, hash, snapshotKey, inputs)

	// Back-fill source/snapshot keys onto the ComponentManifest.Source
	// blocks now that snapshotKey is known. This is an additive fill —
	// stages 1–10 left these empty.
	for _, m := range rc.Manifests {
		m.Source.SourceSnapshotKey = inputs.SourceSnapshotKey
		m.Source.CatalogSnapshotKey = snapshotKey
		m.Source.HeadRevision = inputs.HeadRevision
		m.Source.TreeHash = inputs.TreeHash
		m.Source.WorkingTree = inputs.WorkingTree
	}

	return &CatalogView{
		ResolvedCatalog: rc,
		Snapshot:        snap,
		Graphs:          graphs,
	}, issues, nil
}

// validateResolverInputs enforces that every required field is set.
// Booleans (Authoritative, Preview) are caller-supplied policy and have
// no "unset" sentinel — they are not validated here. CreatedAt and
// HeadRevision/TreeHash/WorkingTree are required (HeadRevision/TreeHash
// may be empty for local-nogit, but the caller MUST tell us by setting
// SourceScope = local-nogit).
func validateResolverInputs(in ResolverInputs) error {
	var missing []string
	if in.OrunVersion == "" {
		missing = append(missing, "OrunVersion")
	}
	if in.SchemaVersion == "" {
		missing = append(missing, "SchemaVersion")
	}
	if in.ResolverVersion == 0 {
		missing = append(missing, "ResolverVersion")
	}
	if in.StackSources == nil {
		missing = append(missing, "StackSources")
	}
	if in.SourceSnapshotKey == "" {
		missing = append(missing, "SourceSnapshotKey")
	}
	if in.CatalogInputHash == "" {
		missing = append(missing, "CatalogInputHash")
	}
	if in.Repo == "" {
		missing = append(missing, "Repo")
	}
	if in.SourceScope == "" {
		missing = append(missing, "SourceScope")
	}
	if in.WorkingTree == "" {
		missing = append(missing, "WorkingTree")
	}
	if in.SourceScope != "local-nogit" {
		if in.HeadRevision == "" {
			missing = append(missing, "HeadRevision")
		}
		if in.TreeHash == "" {
			missing = append(missing, "TreeHash")
		}
	}
	if in.CreatedAt == "" {
		missing = append(missing, "CreatedAt")
	}
	if len(missing) > 0 {
		return &ErrResolverInputsIncomplete{MissingFields: missing}
	}
	return nil
}

// assembleSnapshot builds the *CatalogSnapshot per data-model.md §2.
// summary.* counts are computed from sorted-distinct enumeration of the
// resolved manifest set; objects.components is ordered by componentKey.
func assembleSnapshot(manifests []*catalogmodel.ComponentManifest, graphs []*catalogmodel.CatalogGraph, hash, snapshotKey string, inputs ResolverInputs) *catalogmodel.CatalogSnapshot {
	stackSources := append([]string(nil), inputs.StackSources...)

	summary := computeSummary(manifests)

	objects := catalogmodel.CatalogObjects{
		Components: make([]catalogmodel.ManifestRef, 0, len(manifests)),
	}
	for _, m := range manifests {
		objects.Components = append(objects.Components, catalogmodel.ManifestRef{
			Key:          m.Identity.ComponentKey,
			Name:         m.Identity.Name,
			Path:         "components/" + m.Identity.Name + "/manifest.json",
			ManifestHash: m.Source.ManifestHash,
		})
	}
	sort.SliceStable(objects.Components, func(a, b int) bool {
		return objects.Components[a].Key < objects.Components[b].Key
	})

	return &catalogmodel.CatalogSnapshot{
		APIVersion:         "orun.io/v1alpha1",
		Kind:               "CatalogSnapshot",
		CatalogSnapshotKey: snapshotKey,
		CatalogSnapshotID:  catalogmodel.NewCatalogSnapshotID(),
		SourceSnapshotKey:  inputs.SourceSnapshotKey,
		Repo:               inputs.Repo,
		SourceScope:        inputs.SourceScope,
		HeadRevision:       inputs.HeadRevision,
		TreeHash:           inputs.TreeHash,
		WorkingTree:        inputs.WorkingTree,
		Authoritative:      inputs.Authoritative,
		Preview:            inputs.Preview,
		Resolver: catalogmodel.CatalogResolver{
			OrunVersion:     inputs.OrunVersion,
			SchemaVersion:   inputs.SchemaVersion,
			ResolverVersion: inputs.ResolverVersion,
			StackSources:    stackSources,
		},
		CatalogHash: hash,
		Summary:     summary,
		Objects:     objects,
		CreatedAt:   inputs.CreatedAt,
	}
}

// computeSummary derives the summary.* counts from sorted-distinct
// enumeration over the resolved manifest set. Mirrors the §2 spec:
// components = len(manifests); systems / owners / domains = distinct
// non-empty values of metadata.system / metadata.owner / spec.domain;
// apis = distinct (provides ∪ consumes); resources = distinct uses.
func computeSummary(manifests []*catalogmodel.ComponentManifest) catalogmodel.CatalogSummary {
	systems := map[string]struct{}{}
	apis := map[string]struct{}{}
	resources := map[string]struct{}{}
	owners := map[string]struct{}{}
	domains := map[string]struct{}{}
	for _, m := range manifests {
		if s := m.Spec.System; s != "" {
			systems[s] = struct{}{}
		}
		for _, a := range m.Spec.Dependencies.APIs.Provides {
			if a != "" {
				apis[a] = struct{}{}
			}
		}
		for _, a := range m.Spec.Dependencies.APIs.Consumes {
			if a != "" {
				apis[a] = struct{}{}
			}
		}
		for _, r := range m.Spec.Dependencies.Resources.Uses {
			if r != "" {
				resources[r] = struct{}{}
			}
		}
		if o := m.Metadata.Owner; o != "" {
			owners[o] = struct{}{}
		}
		if d := m.Spec.Domain; d != "" {
			domains[d] = struct{}{}
		}
	}
	return catalogmodel.CatalogSummary{
		Components: len(manifests),
		Systems:    len(systems),
		APIs:       len(apis),
		Resources:  len(resources),
		Owners:     len(owners),
		Domains:    len(domains),
	}
}

// IsResolverInputsIncomplete reports whether err is or wraps an
// ErrResolverInputsIncomplete. Useful for callers that want to retry
// after filling missing fields.
func IsResolverInputsIncomplete(err error) bool {
	var target *ErrResolverInputsIncomplete
	return errors.As(err, &target)
}
