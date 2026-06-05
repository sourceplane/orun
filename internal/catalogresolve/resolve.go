package catalogresolve

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// DiscoverAndLoad runs the first three resolution-pipeline stages on the
// workspace identified by opts:
//
//  1. discover  — walk for component.yaml / .yml respecting intent
//     `catalog.discovery.exclude`;
//  2. load      — YAML+schema-validate each manifest into a typed
//     catalogmodel.ComponentYAML;
//  3. inherit   — apply intent.yaml `catalog.defaults` underneath
//     authored values, per the precedence rules in
//     resolution-pipeline.md §3.
//
// The full resolver entry point (Resolve(ctx, opts)) is intentionally
// out of scope for this PR — see Task 0026 / C2 second PR. The output
// of DiscoverAndLoad is the input to that next pass.
//
// DiscoverAndLoad is deterministic: two consecutive calls on the same
// workspace produce a byte-identical DiscoveryResult.
func DiscoverAndLoad(ctx context.Context, opts Options) (DiscoveryResult, error) {
	if opts.WorkspaceRoot == "" {
		return DiscoveryResult{}, &ErrWorkspaceInvalid{Reason: errEmptyRoot.Error()}
	}
	root, err := filepath.Abs(opts.WorkspaceRoot)
	if err != nil {
		return DiscoveryResult{}, &ErrWorkspaceInvalid{Reason: err.Error()}
	}
	// Validate the root is a directory before doing anything else so a
	// caller pointing us at a file gets a typed ErrWorkspaceInvalid
	// rather than a downstream intent-read failure.
	if info, err := os.Stat(root); err != nil {
		return DiscoveryResult{}, &ErrWorkspaceInvalid{Reason: err.Error()}
	} else if !info.IsDir() {
		return DiscoveryResult{}, &ErrWorkspaceInvalid{Reason: fmt.Sprintf("workspace root %q is not a directory", root)}
	}

	// Resolve intent path + load (file-not-found is OK, file-malformed
	// is a typed error).
	intentAbs := opts.IntentPath
	if intentAbs == "" {
		intentAbs = filepath.Join(root, "intent.yaml")
	} else if !filepath.IsAbs(intentAbs) {
		intentAbs = filepath.Join(root, intentAbs)
	}
	intentRel := toRelSlash(root, intentAbs)
	intent, err := loadIntent(intentAbs, intentRel)
	if err != nil {
		return DiscoveryResult{}, err
	}

	var (
		excludes []string
		defaults *intentCatalogDefaults
		intentOK string
	)
	if intent != nil {
		intentOK = intentRel
		if intent.Catalog != nil {
			defaults = intent.Catalog.Defaults
			if intent.Catalog.Discovery != nil {
				excludes = intent.Catalog.Discovery.Exclude
			}
		}
	}

	rels, err := discover(ctx, root, excludes)
	if err != nil {
		return DiscoveryResult{}, err
	}

	manifests := make([]AuthoredManifest, 0, len(rels))
	for _, rel := range rels {
		if cancelErr := ctx.Err(); cancelErr != nil {
			return DiscoveryResult{}, cancelErr
		}
		am, err := loadAuthored(root, rel)
		if err != nil {
			return DiscoveryResult{}, err
		}
		am = inherit(am, defaults, intentOK)
		manifests = append(manifests, am)
	}

	// Ingest inline intent.yaml components (those not already discovered as a
	// component.yaml file), so the catalog's component set matches the legacy
	// inline∪discovered set the cockpit and --changed operate on.
	if intent != nil && len(intent.Components) > 0 {
		discoveredNames := make(map[string]bool, len(manifests))
		for _, m := range manifests {
			discoveredNames[m.Component.Metadata.Name] = true
		}
		for _, am := range inlineManifests(intent, discoveredNames) {
			manifests = append(manifests, inherit(am, defaults, intentOK))
		}
	}

	return DiscoveryResult{
		Manifests:  manifests,
		IntentPath: intentOK,
	}, nil
}
