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
	// Declared is true when the entity is named by its own block in
	// intent.yaml (an `environments.<name>` entry): the assembly then
	// materializes it even when no component references it. A
	// catalog.entities entry alone never declares (enrich, never create).
	Declared bool
	// URL is the entity's front door (environments.<name>.url); it rides as
	// spec.url beside the primary link.
	URL string
	// Order is the entity's position in its promotion chain (environments
	// only): 0 for an environment nothing depends on, 1 + the deepest
	// dependency otherwise. Rides as spec.order.
	Order int
}

// resolveEnrichments parses + validates the intent's catalog.entities block
// and resolves each entry's doc set. derivedNames maps a canonical kind to
// the set of bare names the current manifests imply — the basis of the
// materialize check (a warning: "derived" stays honest, nothing is created).
// Deterministic: entries are processed in sorted key order.
func resolveEnrichments(intent *intentFile, manifests []*catalogmodel.ComponentManifest, docCtx *docResolveContext) ([]EntityEnrichment, []ValidationIssue) {
	if intent == nil {
		return nil, nil
	}
	var issues []ValidationIssue
	addIssue := func(sev Severity, code, msg string) {
		issues = append(issues, ValidationIssue{
			File: "intent.yaml", Severity: sev, Code: code, Message: msg,
		})
	}

	// The declared environments first: `environments.<name>.{description,
	// url, links}` is the environment's own declaration site, so it
	// materializes the entity and wins over a catalog.entities entry for the
	// same name (which then only fills what the block left empty).
	envEnrichments, envIssues := environmentEnrichments(intent)
	issues = append(issues, envIssues...)
	declaredEnv := map[string]int{} // bare name → index in out
	out := make([]EntityEnrichment, 0, len(envEnrichments))
	for _, e := range envEnrichments {
		declaredEnv[e.Name] = len(out)
		out = append(out, e)
	}

	if intent.Catalog == nil || len(intent.Catalog.Entities) == 0 {
		return out, issues
	}

	derived := derivedNames(manifests)

	keys := make([]string, 0, len(intent.Catalog.Entities))
	for k := range intent.Catalog.Entities {
		keys = append(keys, k)
	}
	sort.Strings(keys)

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
		// this stays a warning, never an error.) A declared environment is
		// the exception: its own block proves it.
		_, declared := declaredEnv[name]
		declared = declared && kind == catalogmodel.EntityKindEnvironment
		if !derived[kind][name] && !declared {
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
		if declared {
			// Merge onto the declared environment: the block's description
			// and front door stand; the enrichment fills the rest and its
			// links follow the declared ones.
			d := &out[declaredEnv[name]]
			if d.Description == "" {
				d.Description = e.Description
			}
			if d.Owner == "" {
				d.Owner = e.Owner
			}
			d.Links = append(d.Links, e.Links...)
			d.Tags = append(d.Tags, e.Tags...)
			if d.Overview == "" && len(d.Pages) == 0 {
				d.Overview, d.Pages, d.Docs = e.Overview, e.Pages, e.Docs
			}
			continue
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out, issues
}

// environmentEnrichments turns the declared `environments.<name>` blocks
// into Declared enrichments of kind Environment: description, the front door
// as links[0] with role=primary (and spec.url), further links after it, and
// the promotion order. An environment with none of description/url/links
// still declares itself (it materializes with its order), so the Overview's
// door list and the environment entity set agree with intent.yaml.
// Deterministic: sorted by name. A url that is not absolute http(s) is an
// error — a relative door opens nothing.
func environmentEnrichments(intent *intentFile) ([]EntityEnrichment, []ValidationIssue) {
	if intent == nil || len(intent.Environments) == 0 {
		return nil, nil
	}
	var issues []ValidationIssue
	names := make([]string, 0, len(intent.Environments))
	for n := range intent.Environments {
		if n != "" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	orders := promotionOrders(intent.Environments)
	out := make([]EntityEnrichment, 0, len(names))
	for _, name := range names {
		spec := intent.Environments[name]
		if spec == nil {
			spec = &intentEnvironment{}
		}
		e := EntityEnrichment{
			Kind:        catalogmodel.EntityKindEnvironment,
			Name:        name,
			Description: spec.Description,
			Declared:    true,
			Order:       orders[name],
		}
		if spec.URL != "" {
			if !isHTTPURL(spec.URL) {
				issues = append(issues, ValidationIssue{
					File: "intent.yaml", Severity: SeverityError, Code: "environments.url.invalid",
					Message: fmt.Sprintf("environments.%s.url: %q must be an absolute http(s) URL", name, spec.URL),
				})
			} else {
				e.URL = spec.URL
				e.Links = append(e.Links, RepoLink{Title: name, URL: spec.URL, Role: catalogmodel.LinkRolePrimary})
			}
		}
		for i, l := range spec.Links {
			if l.URL == "" || !isHTTPURL(l.URL) {
				issues = append(issues, ValidationIssue{
					File: "intent.yaml", Severity: SeverityError, Code: "environments.links.url.invalid",
					Message: fmt.Sprintf("environments.%s.links[%d].url: %q must be an absolute http(s) URL", name, i, l.URL),
				})
				continue
			}
			title := l.Title
			if title == "" {
				title = l.URL
			}
			e.Links = append(e.Links, RepoLink{Title: title, URL: l.URL, Icon: l.Icon})
		}
		out = append(out, e)
	}
	return out, issues
}

// promotionOrders computes each environment's depth in the promotion chain:
// 0 with no dependsOn, else 1 + the deepest dependency. A cycle or an
// unknown dependency stops at the environments resolved so far (order is a
// display hint; the planner validates promotion separately).
func promotionOrders(envs map[string]*intentEnvironment) map[string]int {
	orders := map[string]int{}
	var visit func(name string, stack map[string]bool) int
	visit = func(name string, stack map[string]bool) int {
		if o, ok := orders[name]; ok {
			return o
		}
		spec := envs[name]
		if spec == nil || spec.Promotion == nil || stack[name] {
			return 0
		}
		stack[name] = true
		depth := 0
		for _, d := range spec.Promotion.DependsOn {
			if d.Environment == "" || d.Environment == name {
				continue
			}
			if _, known := envs[d.Environment]; !known {
				continue
			}
			if o := visit(d.Environment, stack) + 1; o > depth {
				depth = o
			}
		}
		delete(stack, name)
		orders[name] = depth
		return depth
	}
	for n := range envs {
		visit(n, map[string]bool{})
	}
	return orders
}

func isHTTPURL(s string) bool {
	l := strings.ToLower(s)
	rest := ""
	switch {
	case strings.HasPrefix(l, "https://"):
		rest = s[len("https://"):]
	case strings.HasPrefix(l, "http://"):
		rest = s[len("http://"):]
	default:
		return false
	}
	return rest != "" && !strings.ContainsAny(rest, " \t\n")
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
