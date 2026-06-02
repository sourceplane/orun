package objplan

import (
	"strings"

	"github.com/sourceplane/orun/internal/nodes"
)

// sanitizeRefSeg folds an arbitrary string into the ref/path alphabet so it can
// be a ref segment; the original value is preserved inside the node JSON.
func sanitizeRefSeg(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-._")
	if out == "" {
		return "x"
	}
	return out
}

// SourceRefs returns the ref names a source node of the given scope should be
// published under: always sources/current, plus the scope-specific pointer.
func SourceRefs(src nodes.SourceSnapshot) []string {
	refs := []string{"refs/sources/current"}
	switch src.Scope {
	case nodes.ScopeMain:
		refs = append(refs, "refs/sources/main")
	case nodes.ScopeBranch:
		if src.Branch != "" {
			refs = append(refs, "refs/sources/branches/"+sanitizeRefSeg(src.Branch))
		}
	case nodes.ScopePR:
		if src.PR != "" {
			refs = append(refs, "refs/sources/prs/"+sanitizeRefSeg(src.PR))
		}
	}
	return refs
}

// CatalogRefs mirrors SourceRefs for the catalog layer (catalogs/current plus a
// scope-specific pointer), keyed off the source scope the catalog was resolved
// at.
func CatalogRefs(src nodes.SourceSnapshot) []string {
	refs := []string{"refs/catalogs/current"}
	switch src.Scope {
	case nodes.ScopeMain:
		refs = append(refs, "refs/catalogs/main")
	case nodes.ScopeBranch:
		if src.Branch != "" {
			refs = append(refs, "refs/catalogs/branches/"+sanitizeRefSeg(src.Branch))
		}
	case nodes.ScopePR:
		if src.PR != "" {
			refs = append(refs, "refs/catalogs/prs/"+sanitizeRefSeg(src.PR))
		}
	}
	return refs
}

// RevisionRefs returns the ref names a fresh revision is published under.
func RevisionRefs() []string { return []string{"refs/revisions/latest"} }

// TriggerRefs returns the ref names a trigger event is published under, keyed by
// the trigger name.
func TriggerRefs(triggerName string) []string {
	return []string{"refs/triggers/" + sanitizeRefSeg(triggerName) + "/latest"}
}
