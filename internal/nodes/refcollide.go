package nodes

import (
	"fmt"
	"sort"
	"strings"
)

// refcollide.go — one catalog ref names one entity.
//
// Every repo-local entity is keyed `<namespace>/<repo>/<name>` whatever its
// kind: a Component's componentKey and the key of the Domain, System,
// Environment, API, Resource, Composition, Repo or AgentType it implies are
// built the same way, and the kind is not part of the ref. Inside this
// package the kind keeps them apart (entities/<Kind>/ vs components/), but
// every reader that keys on the ref alone — Orunbase's catalog projection,
// whose identity is (project, environment, ref), and the console routes built
// on it — keeps ONE of them, and which one depends on the order it reads the
// tree in: components/ first, then entities/<Kind>/, so the derived entity
// silently replaced the Component. A `spec.domain: web` on the `web`
// component hid the product's one deployable from its own catalog.
//
// The rule: when an entity of another kind resolves to a Component's ref, the
// Component keeps the ref and the other entity is left out of the catalog
// (withheldByComponents) — it is a name and a membership list, both still
// recorded on the component itself (spec.domain, its relations), while the
// Component is the thing that is built, deployed, scored and documented. A
// collision between two non-Component kinds keeps both (neither is the
// deployable, and this package can tell them apart). Every collision is
// recorded on the catalog snapshot as a warning issue, and is what `orun
// catalog validate` refuses and `orun plan` warns about.

// IssueCodeEntityRefCollision is the CatalogIssue (and validation issue) code
// for one catalog ref that entities of more than one kind resolve to.
const IssueCodeEntityRefCollision = "catalog.entity.ref.collision"

// EntityRefSource is one place an entity is declared: the workspace-relative
// file and the field in it. Pointer is the RFC 6901 pointer to that field when
// it names exactly one ("" otherwise).
type EntityRefSource struct {
	File    string
	Field   string
	Pointer string
}

// EntityRefClaim is one entity — a kind and a name — that resolves to a
// catalog ref, with the places it is declared.
type EntityRefClaim struct {
	Kind    string
	Name    string
	Sources []EntityRefSource
}

// EntityRefCollision is a catalog ref that entities of more than one kind
// resolve to. Claims holds one entry per kind: the Component first (when one
// holds the ref), then the other kinds in name order.
type EntityRefCollision struct {
	EntityRef string
	Claims    []EntityRefClaim
}

// HasComponent reports whether a Component is one of the claims — the case in
// which the Component keeps the ref and the others are left out.
func (c EntityRefCollision) HasComponent() bool {
	return len(c.Claims) > 0 && c.Claims[0].Kind == EntityKindComponent
}

// Fix is the declaration to point an author at: the first source of the first
// claim that is not the Component (renaming it is the smaller change — the
// component's name is its identity in plans, state and history).
func (c EntityRefCollision) Fix() (EntityRefClaim, EntityRefSource) {
	for _, cl := range c.Claims {
		if cl.Kind == EntityKindComponent {
			continue
		}
		if len(cl.Sources) > 0 {
			return cl, cl.Sources[0]
		}
		return cl, EntityRefSource{}
	}
	return EntityRefClaim{}, EntityRefSource{}
}

// Message renders the collision as one line naming every claim (kind, name,
// where it is declared), what the catalog does about it, and how to fix it.
func (c EntityRefCollision) Message() string {
	parts := make([]string, 0, len(c.Claims))
	for _, cl := range c.Claims {
		parts = append(parts, describeClaim(cl))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "catalog ref %s names %d entities of different kinds: %s", c.EntityRef, len(c.Claims), joinAnd(parts))
	if c.HasComponent() {
		others := make([]string, 0, len(c.Claims)-1)
		for _, cl := range c.Claims[1:] {
			others = append(others, cl.Kind)
		}
		fmt.Fprintf(&b, ". A ref names one entity, so the catalog keeps the Component and leaves the %s out", joinAnd(others))
	} else {
		kept := "Both are"
		if len(c.Claims) > 2 {
			kept = "All are"
		}
		fmt.Fprintf(&b, ". %s kept here, but Orunbase keeps one entity per ref and shows only one of them", kept)
	}
	b.WriteString(". Rename one of them")
	if cl, src := c.Fix(); cl.Kind != "" && src.File != "" {
		if src.Field != "" {
			fmt.Fprintf(&b, " — e.g. the %s's %s in %s", cl.Kind, src.Field, src.File)
		} else {
			fmt.Fprintf(&b, " — e.g. the %s in %s", cl.Kind, src.File)
		}
	}
	return b.String()
}

// Issue is the collision as a catalog-snapshot warning.
func (c EntityRefCollision) Issue() CatalogIssue {
	is := CatalogIssue{Severity: "warning", Code: IssueCodeEntityRefCollision, Message: c.Message()}
	if c.HasComponent() {
		is.Component = c.EntityRef
	}
	return is
}

// EntityRefCollisions reports every catalog ref that entities of more than one
// kind would resolve to in the catalog AssembleCatalog writes from these
// inputs: the components, the entities derived from their relations and
// contracts, the declared environments, and cat.DeclaredEntities (the Repo
// self-description, agent types). Sorted by ref. Pure — it writes nothing.
func EntityRefCollisions(manifests []ComponentManifest, cat CatalogSnapshot) []EntityRefCollision {
	entities := deriveEntities(manifests, cat.EntityEnrichments)
	entities = append(entities, cat.DeclaredEntities...)
	return entityRefCollisions(manifests, entities, cat.EntityEnrichments)
}

// entityRefCollisions is EntityRefCollisions over an already-derived entity set
// (AssembleCatalog derives once and reuses it).
func entityRefCollisions(manifests []ComponentManifest, entities []Entity, enrichments []EntityEnrichment) []EntityRefCollision {
	type kindKey struct{ ref, kind string }
	claims := map[kindKey]*EntityRefClaim{}
	kindsByRef := map[string][]string{}
	claim := func(ref, kind, name string) *EntityRefClaim {
		kk := kindKey{ref, kind}
		if cl, ok := claims[kk]; ok {
			return cl
		}
		cl := &EntityRefClaim{Kind: kind, Name: name}
		claims[kk] = cl
		kindsByRef[ref] = append(kindsByRef[ref], kind)
		return cl
	}
	addSource := func(ref, kind string, src EntityRefSource) {
		cl, ok := claims[kindKey{ref, kind}]
		if !ok || src.File == "" {
			return
		}
		for _, s := range cl.Sources {
			if s == src {
				return
			}
		}
		cl.Sources = append(cl.Sources, src)
	}

	for _, m := range manifests {
		cl := claim(m.Identity.ComponentKey, EntityKindComponent, m.Identity.Name)
		if m.Identity.Path != "" {
			cl.Sources = append(cl.Sources, EntityRefSource{File: m.Identity.Path, Field: "metadata.name", Pointer: "/metadata/name"})
		}
	}
	for _, e := range entities {
		cl := claim(e.Identity.EntityKey, e.Kind, e.Identity.Name)
		switch {
		case e.Identity.Path != "": // e.g. an agent type's agents/<name>.md
			cl.Sources = append(cl.Sources, EntityRefSource{File: e.Identity.Path})
		case e.Kind == EntityKindRepo:
			cl.Sources = append(cl.Sources, EntityRefSource{File: "intent.yaml", Field: "metadata.name"})
		}
	}

	// Only refs claimed by more than one kind need their derived sources.
	colliding := map[string]bool{}
	for ref, kinds := range kindsByRef {
		if len(kinds) > 1 {
			colliding[ref] = true
		}
	}
	if len(colliding) == 0 {
		return nil
	}

	// Where each derived entity comes from: the component relations and
	// contracts that imply it, then the intent's declared environments.
	for _, m := range manifests {
		file := m.Identity.Path
		for _, r := range m.Relations {
			if colliding[r.To] {
				addSource(r.To, r.ToKind, relationSource(file, r))
			}
		}
		for _, api := range contractAPIs(m.Contracts) {
			if colliding[api] {
				addSource(api, EntityKindAPI, EntityRefSource{File: file, Field: "providesApis/consumesApis"})
			}
		}
	}
	for _, enr := range enrichments {
		if enr.Declared && colliding[enr.EntityKey] {
			addSource(enr.EntityKey, enr.Kind, EntityRefSource{
				File: "intent.yaml", Field: "environments." + enr.Name, Pointer: "/environments/" + escapePointerToken(enr.Name),
			})
		}
	}

	out := make([]EntityRefCollision, 0, len(colliding))
	for ref := range colliding {
		c := EntityRefCollision{EntityRef: ref}
		for _, kind := range kindsByRef[ref] {
			cl := *claims[kindKey{ref, kind}]
			sort.SliceStable(cl.Sources, func(i, j int) bool {
				if cl.Sources[i].File != cl.Sources[j].File {
					return cl.Sources[i].File < cl.Sources[j].File
				}
				return cl.Sources[i].Field < cl.Sources[j].Field
			})
			c.Claims = append(c.Claims, cl)
		}
		sort.SliceStable(c.Claims, func(i, j int) bool {
			ci, cj := c.Claims[i].Kind == EntityKindComponent, c.Claims[j].Kind == EntityKindComponent
			if ci != cj {
				return ci
			}
			return c.Claims[i].Kind < c.Claims[j].Kind
		})
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EntityRef < out[j].EntityRef })
	return out
}

// withheldByComponents drops every non-Component entity whose ref a component
// already holds (see the file comment), returning the entities to write.
// Order is preserved.
func withheldByComponents(manifests []ComponentManifest, entities []Entity) []Entity {
	held := make(map[string]bool, len(manifests))
	for _, m := range manifests {
		held[m.Identity.ComponentKey] = true
	}
	out := entities[:0:0]
	for _, e := range entities {
		if e.Kind != EntityKindComponent && held[e.Identity.EntityKey] {
			continue
		}
		out = append(out, e)
	}
	return out
}

// relationSource names the component field a derived entity's relation comes
// from.
func relationSource(file string, r EntityRelation) EntityRefSource {
	switch {
	case r.Type == "partOf" && r.ToKind == EntityKindDomain:
		return EntityRefSource{File: file, Field: "spec.domain", Pointer: "/spec/domain"}
	case r.Type == "partOf" && r.ToKind == EntityKindSystem:
		return EntityRefSource{File: file, Field: "spec.system", Pointer: "/spec/system"}
	case r.Type == "deployedTo":
		return EntityRefSource{File: file, Field: "environments"}
	case r.Type == "composedBy":
		return EntityRefSource{File: file, Field: "spec.type"} // the composition the lock binds it to
	case r.Type == "dependsOn" && r.ToKind == EntityKindResource:
		return EntityRefSource{File: file, Field: "resources"}
	default:
		return EntityRefSource{File: file, Field: r.Type}
	}
}

// describeClaim renders one claim: `Component "web" (apps/web/component.yaml)`
// or `Domain "web" (spec.domain in apps/web/component.yaml)`. At most three
// sources are listed.
func describeClaim(cl EntityRefClaim) string {
	head := fmt.Sprintf("%s %q", cl.Kind, cl.Name)
	if len(cl.Sources) == 0 {
		return head
	}
	const max = 3
	where := make([]string, 0, max)
	for i, s := range cl.Sources {
		if i == max {
			break
		}
		switch {
		case cl.Kind == EntityKindComponent || s.Field == "":
			where = append(where, s.File)
		default:
			where = append(where, s.Field+" in "+s.File)
		}
	}
	list := strings.Join(where, ", ")
	if extra := len(cl.Sources) - len(where); extra > 0 {
		list += fmt.Sprintf(" and %d more", extra)
	}
	return head + " (" + list + ")"
}

// joinAnd joins "a", "a and b", "a, b and c".
func joinAnd(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

// escapePointerToken escapes one RFC 6901 reference token.
func escapePointerToken(s string) string {
	s = strings.ReplaceAll(s, "~", "~0")
	return strings.ReplaceAll(s, "/", "~1")
}
