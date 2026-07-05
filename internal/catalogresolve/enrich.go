package catalogresolve

// enrich.go — the `catalog.entities` enrichment block (saas-catalog-docs CD2).
// Enrichment merges metadata + docs onto entities the resolver DERIVES
// (System, Domain, Group, Environment) — kinds that exist only as names
// implied by component specs and so have no manifest of their own to declare
// docs in. Enrichment never creates an entity: a target that doesn't
// materialize from the resolve is a warning, and an enrichment for a declared
// kind (component/repo/api/…) is an error — one declaration site per entity.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// enrichableKinds maps the lowercase declared kind segment to the canonical
// entity kind. Only derived kinds with no own manifest are enrichable (CD2).
var enrichableKinds = map[string]string{
	"system":      catalogmodel.EntityKindSystem,
	"domain":      catalogmodel.EntityKindDomain,
	"group":       catalogmodel.EntityKindGroup,
	"environment": catalogmodel.EntityKindEnvironment,
}

// declaredKinds are the kinds an enrichment must NOT target — they have their
// own declaration site (component.yaml / the repo block / contracts).
var declaredKinds = map[string]bool{
	"component": true, "repo": true, "api": true, "resource": true,
	"composition": true, "user": true, "product": true,
}

// EntityEnrichment is one resolved catalog.entities entry: the target
// (canonical kind + bare name), the fill-empty metadata, and the resolved doc
// set (bytes read through the same doc context as every other doc set).
type EntityEnrichment struct {
	Kind        string // canonical, e.g. "Domain"
	Name        string // bare target name, e.g. "identity"
	Description string
	Owner       string
	Links       []RepoLink
	Tags        []string
	Overview    string
	Pages       []catalogmodel.DocPage
	Docs        []catalogmodel.ResolvedDoc
}

// resolveEnrichments parses + validates the intent's catalog.entities block
// and resolves each entry's doc set. derivedNames maps a canonical kind to
// the set of bare names the current manifests imply — the basis of the
// materialize check (a warning: "derived" stays honest, nothing is created).
// Deterministic: entries are processed in sorted key order.
func resolveEnrichments(intent *intentFile, manifests []*catalogmodel.ComponentManifest, docCtx *docResolveContext) ([]EntityEnrichment, []ValidationIssue) {
	if intent == nil || intent.Catalog == nil || len(intent.Catalog.Entities) == 0 {
		return nil, nil
	}
	var issues []ValidationIssue
	addIssue := func(sev Severity, code, msg string) {
		issues = append(issues, ValidationIssue{
			File: "intent.yaml", Severity: sev, Code: code, Message: msg,
		})
	}

	derived := derivedNames(manifests)

	keys := make([]string, 0, len(intent.Catalog.Entities))
	for k := range intent.Catalog.Entities {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var out []EntityEnrichment
	for _, key := range keys {
		spec := intent.Catalog.Entities[key]
		kindSeg, name, ok := strings.Cut(key, "/")
		if !ok || kindSeg == "" || name == "" {
			addIssue(SeverityError, "catalog.entities.key.invalid",
				fmt.Sprintf("catalog.entities %q: keys are \"<kind>/<name>\" with a lowercase kind (e.g. domain/identity)", key))
			continue
		}
		kindSeg = strings.ToLower(kindSeg)
		if declaredKinds[kindSeg] {
			addIssue(SeverityError, "catalog.entities.kind.declared",
				fmt.Sprintf("catalog.entities %q: %s is a declared kind — set its metadata and docs where it is declared, not via enrichment", key, kindSeg))
			continue
		}
		kind, enrichable := enrichableKinds[kindSeg]
		if !enrichable {
			addIssue(SeverityError, "catalog.entities.kind.unknown",
				fmt.Sprintf("catalog.entities %q: kind %q is not enrichable (allowed: system, domain, group, environment)", key, kindSeg))
			continue
		}
		if spec == nil {
			spec = &intentEntityEnrichment{}
		}

		// Enrich, never create: a target no component implies is a warning —
		// nothing appears in the catalog that the derived model doesn't prove.
		// (Group membership resolved from CODEOWNERS is only known later, so
		// this stays a warning, never an error.)
		if !derived[kind][name] {
			addIssue(SeverityWarning, "catalog.entities.target.unreferenced",
				fmt.Sprintf("catalog.entities %q: no component references %s %q at resolve time — the enrichment produces nothing until one does", key, kindSeg, name))
		}

		e := EntityEnrichment{
			Kind:        kind,
			Name:        name,
			Description: spec.Description,
			Owner:       spec.Owner,
			Tags:        append([]string(nil), spec.Tags...),
		}
		for _, l := range spec.Links {
			e.Links = append(e.Links, RepoLink{Title: l.Title, URL: l.URL, Icon: l.Icon})
		}
		if spec.Docs != nil {
			e.Overview = spec.Docs.Overview
			e.Pages = append([]catalogmodel.DocPage(nil), spec.Docs.Pages...)
			issues = append(issues, validateDocPages(spec.Docs.Pages, "intent.yaml")...)
			if docCtx != nil {
				// Enrichment doc paths are repo-root-relative (declared in
				// intent.yaml at the root).
				e.Docs = docCtx.resolve("", spec.Docs.Overview, spec.Docs.Pages)
			}
		}
		out = append(out, e)
	}
	return out, issues
}

// derivedNames computes, per enrichable kind, the set of bare names the
// manifests imply — the same signals nodes.deriveEntities walks (spec.system,
// spec.domain, authored owner, environment bindings), reduced to names.
func derivedNames(manifests []*catalogmodel.ComponentManifest) map[string]map[string]bool {
	out := map[string]map[string]bool{
		catalogmodel.EntityKindSystem:      {},
		catalogmodel.EntityKindDomain:      {},
		catalogmodel.EntityKindGroup:       {},
		catalogmodel.EntityKindEnvironment: {},
	}
	add := func(kind, name string) {
		if name != "" {
			out[kind][name] = true
		}
	}
	for _, cm := range manifests {
		if cm == nil {
			continue
		}
		add(catalogmodel.EntityKindSystem, cm.Spec.System)
		add(catalogmodel.EntityKindDomain, cm.Spec.Domain)
		if cm.Metadata.Owner != "" {
			if k, kind := catalogmodel.NormalizeOwnerRef(cm.Metadata.Owner); k != "" && kind == catalogmodel.EntityKindGroup {
				// The derived Group's bare name: the typed ref minus its
				// `group:` prefix, last path segment ("group:org/team" → "team").
				add(catalogmodel.EntityKindGroup, lastPathSegment(strings.TrimPrefix(k, "group:")))
			}
		}
		for env := range cm.Spec.Environments {
			add(catalogmodel.EntityKindEnvironment, env)
		}
	}
	return out
}

func lastPathSegment(s string) string {
	if i := strings.LastIndexByte(s, '/'); i >= 0 && i < len(s)-1 {
		return s[i+1:]
	}
	return s
}
