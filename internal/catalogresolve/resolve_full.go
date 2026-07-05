package catalogresolve

import (
	"context"
	"path"
	"path/filepath"
	"sort"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// Resolve runs the full per-resolution-pipeline.md §1 stages 1–10
// against the workspace identified by opts and returns a
// ResolvedCatalog plus collected ValidationIssues.
//
// Pipeline:
//
//  1. discover + load + inherit → DiscoverAndLoad (stages 1–5, 7)
//  2. authoredToManifest          → bridge to catalogmodel.ComponentManifest
//  3. infer                       → stage 6 (per resolution-pipeline.md §4)
//  4. resolveDependencies         → stage 8 (per §5)
//  5. validate                    → stage 9 (per §6 rules table)
//  6. manifestHash                → stage 10 (per identity-and-keys.md §10)
//
// In default mode warnings are collected but the resolver continues; a
// SeverityError validation issue aborts the pipeline and surfaces as a
// typed error wrapping the first error issue. In strict mode every
// warning is promoted to an error before the abort check.
//
// Resolve is deterministic: two consecutive calls on the same workspace
// produce byte-identical ResolvedCatalogs (verified by the
// determinism test).
func Resolve(ctx context.Context, opts Options) (*ResolvedCatalog, []ValidationIssue, error) {
	// Stage 1–5, 7 — reuse PR-1's DiscoverAndLoad. Errors there are
	// already typed (ErrWorkspaceInvalid / ErrManifestInvalid /
	// ErrIntentInvalid).
	dr, err := DiscoverAndLoad(ctx, opts)
	if err != nil {
		return nil, nil, err
	}

	// Re-load the intent file once for inference + namespace
	// resolution. DiscoverAndLoad already validated it; we only need
	// the inference block which it does not surface.
	intent, _ := loadIntentForResolve(opts)

	namespace := resolveNamespace(opts, intent)
	repo := resolveRepo(opts)

	manifests := make([]*catalogmodel.ComponentManifest, 0, len(dr.Manifests))
	for i := range dr.Manifests {
		manifests = append(manifests, authoredToManifest(dr.Manifests[i], namespace, repo))
	}

	// Stage 6 — inference. Per-manifest, errors are collected as
	// ErrInferenceFailed and (in default mode) NOT promoted to issues;
	// strict mode promotes via the validate stage table at C8 (out of
	// scope here — kept additive).
	cfg := resolveInferenceConfig(intentInferenceFor(intent))
	var inferenceIssues []ValidationIssue
	for i := range dr.Manifests {
		_, issues := infer(opts.WorkspaceRoot, dr.Manifests[i], manifests[i], cfg)
		inferenceIssues = append(inferenceIssues, issues...)
	}

	// Stage 8 — dependency resolution. Returns issues for unresolved
	// targets; the validate stage promotes them per the §6 rule table.
	depIssues := resolveDependencies(dr.Manifests, manifests)

	// Stage 9 — validation.
	issues := validate(dr.Manifests, manifests, opts.Strict)
	// Splice in inference + dep issues (with strict promotion). These
	// already carry their default severity; promote here so the abort
	// check sees a uniform severity field.
	for _, ii := range append(inferenceIssues, depIssues...) {
		if opts.Strict && ii.Severity == SeverityWarning {
			ii.Severity = SeverityError
		}
		issues = append(issues, ii)
	}

	// Stage 9b — doc-set resolution (saas-catalog-docs CD1): validate each
	// entity's declared docs.pages shape (errors — an authoring bug) and
	// resolve the doc set (overview + pages) into bytes read at the pinned
	// commit. Attachment problems (dirty path, over cap, missing file) are
	// never issues — a doc problem never fails a plan; they log and leave the
	// entry declared-only. One git probe + one byte budget span the resolve.
	docCtx := newDocResolveContext(opts.WorkspaceRoot)
	for _, cm := range manifests {
		if cm.Docs == nil || (cm.Docs.Overview == "" && len(cm.Docs.Pages) == 0) {
			continue
		}
		issues = append(issues, validateDocPages(cm.Docs.Pages, cm.Identity.SourceFile)...)
		baseDir := path.Dir(cm.Identity.SourceFile)
		if baseDir == "." {
			baseDir = ""
		}
		cm.ResolvedDocs = docCtx.resolve(baseDir, cm.Docs.Overview, cm.Docs.Pages)
	}
	if intent != nil && intent.Repo != nil && intent.Repo.Docs != nil {
		issues = append(issues, validateDocPages(intent.Repo.Docs.Pages, "intent.yaml")...)
	}

	// Stage 9c — catalog.entities enrichment (saas-catalog-docs CD2): metadata
	// + docs for derived kinds, resolved through the same doc context.
	enrichments, enrichIssues := resolveEnrichments(intent, manifests, docCtx)
	for _, ei := range enrichIssues {
		if opts.Strict && ei.Severity == SeverityWarning {
			ei.Severity = SeverityError
		}
		issues = append(issues, ei)
	}
	sortIssues(issues)

	// Stage 10 — manifestHash per component.
	for _, cm := range manifests {
		h, err := manifestHash(cm)
		if err != nil {
			return nil, issues, err
		}
		cm.Source.ManifestHash = h
	}

	// Order manifests by componentKey for determinism.
	sort.SliceStable(manifests, func(a, b int) bool {
		return manifests[a].Identity.ComponentKey < manifests[b].Identity.ComponentKey
	})

	var intentExcludes []string
	if intent != nil && intent.Catalog != nil && intent.Catalog.Discovery != nil {
		intentExcludes = intent.Catalog.Discovery.Exclude
	}

	intentAbs := opts.IntentPath
	if intentAbs == "" {
		intentAbs = filepath.Join(opts.WorkspaceRoot, "intent.yaml")
	}
	globalDigest := computeGlobalDigest(intentAbs)

	rc := &ResolvedCatalog{
		Manifests:    manifests,
		Issues:       issues,
		IntentPath:   dr.IntentPath,
		Namespace:    namespace,
		Repo:         repo,
		Excludes:     EffectiveExcludes(intentExcludes),
		Fingerprints: computeFingerprints(opts.WorkspaceRoot, manifests, globalDigest),
		RepoDecl:     repoDeclFromIntent(intent, namespace, repo, docCtx),
		Enrichments:  enrichments,
	}

	if firstErr := firstError(issues); firstErr != nil {
		return rc, issues, *firstErr
	}
	return rc, issues, nil
}

// loadIntentForResolve re-reads the intent file using the same
// resolution rules DiscoverAndLoad applies. Errors are swallowed —
// DiscoverAndLoad already ran successfully, so any failure here means
// the file vanished mid-call (best effort: no inference).
func loadIntentForResolve(opts Options) (*intentFile, error) {
	intentAbs := opts.IntentPath
	if intentAbs == "" {
		intentAbs = opts.WorkspaceRoot + "/intent.yaml"
	}
	rel := "intent.yaml"
	intent, _ := loadIntent(intentAbs, rel)
	return intent, nil
}

// repoDeclFromIntent resolves the top-level `repo:` block into a
// RepoDeclaration, or nil when the intent declares none. The entity key is
// repo-local (<namespace>/<repo>/<name>); no cloud project id is available at
// resolve time (saas-workspace-overview WO3). displayName/description default
// from metadata when omitted. The doc set (overview + pages, saas-catalog-docs
// CD1) resolves through docCtx: bytes read at the pinned commit, dirty paths
// refused, caps enforced — declared paths are repo-root-relative here.
func repoDeclFromIntent(intent *intentFile, namespace, repo string, docCtx *docResolveContext) *RepoDeclaration {
	if intent == nil || intent.Repo == nil {
		return nil
	}
	rb := intent.Repo
	name := repo
	if intent.Metadata != nil && intent.Metadata.Name != "" {
		name = intent.Metadata.Name
	}
	displayName := rb.DisplayName
	if displayName == "" {
		displayName = name
	}
	description := rb.Description
	if description == "" && intent.Metadata != nil {
		description = intent.Metadata.Description
	}
	overview := ""
	var pages []catalogmodel.DocPage
	if rb.Docs != nil {
		overview = rb.Docs.Overview
		pages = rb.Docs.Pages
	}
	links := make([]RepoLink, 0, len(rb.Links))
	for _, l := range rb.Links {
		links = append(links, RepoLink{Title: l.Title, URL: l.URL, Icon: l.Icon})
	}
	d := &RepoDeclaration{
		EntityKey:   catalogmodel.FormatEntityKey(namespace, repo, name),
		Name:        name,
		Namespace:   namespace,
		Repo:        repo,
		DisplayName: displayName,
		Description: description,
		Owner:       rb.Owner,
		Overview:    overview,
		Pages:       append([]catalogmodel.DocPage(nil), pages...),
		Links:       links,
		Tags:        append([]string(nil), rb.Tags...),
	}
	if docCtx != nil {
		d.Docs = docCtx.resolve("", overview, pages)
	}
	return d
}

// intentInferenceFor pulls the (optional) inference block from a parsed
// intent file. Nil-safe at every level.
func intentInferenceFor(intent *intentFile) *intentInference {
	if intent == nil || intent.Catalog == nil {
		return nil
	}
	return intent.Catalog.Inference
}

// sortIssues re-applies the validate stage's ordering after splicing in
// later issues (inference, deps).
func sortIssues(issues []ValidationIssue) {
	sort.SliceStable(issues, func(a, b int) bool {
		if issues[a].Severity != issues[b].Severity {
			return issues[a].Severity > issues[b].Severity
		}
		if issues[a].Code != issues[b].Code {
			return issues[a].Code < issues[b].Code
		}
		if issues[a].File != issues[b].File {
			return issues[a].File < issues[b].File
		}
		return issues[a].Pointer < issues[b].Pointer
	})
}
