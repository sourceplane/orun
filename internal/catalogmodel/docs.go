package catalogmodel

// docs.go — the doc-set model (saas-catalog-docs CD1). A doc set is the
// reserved `overview` front page plus an ordered list of named, role-tagged
// `pages`, declared on the shared docs struct every kind carries. Bytes are
// resolved at plan time and walked into the catalog closure as
// content-addressed blobs; this file owns only the declared shape and the
// resolved carrier type.

import (
	"path"
	"regexp"
	"strings"
)

// DocPage is one declared additional document on an entity's docs block
// (model.md §2a, normative in orun-cloud specs/epics/saas-catalog-docs).
// Path is required; Key defaults to the filename stem, Title to the doc's
// first H1 (else the filename), Role to "guide".
type DocPage struct {
	Path  string `json:"path"`
	Key   string `json:"key,omitempty"`
	Title string `json:"title,omitempty"`
	Role  string `json:"role,omitempty"`
}

// DocKeyOverview is the reserved doc key for the front page. A `pages` entry
// may not redeclare it.
const DocKeyOverview = "overview"

// DocRoleDefault is the role assumed when a page declares none.
const DocRoleDefault = "guide"

// MaxDocPagesPerEntity bounds the declared page count per entity (model.md §0).
const MaxDocPagesPerEntity = 24

// MaxDocBytes is the per-document attachment cap; larger docs stay declared
// (path pointer) but carry no body (model.md §0).
const MaxDocBytes = 256 * 1024

// MaxDocClosureBytes is the per-closure doc budget; once exceeded, further
// docs stay declared-only and the resolve logs the cutoff (model.md §0).
const MaxDocClosureBytes = 8 * 1024 * 1024

// wellKnownDocRoles is the styled role set. Unknown role slugs are allowed
// (free taxonomy, same posture as the entity `kind` column); consumers render
// them neutrally.
var wellKnownDocRoles = map[string]bool{
	"guide": true, "architecture": true, "runbook": true, "adr": true,
	"reference": true, "changelog": true, "faq": true, "onboarding": true,
}

// IsWellKnownDocRole reports whether role is in the styled set.
func IsWellKnownDocRole(role string) bool { return wellKnownDocRoles[role] }

var docSlugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// IsDocSlug reports whether s is a valid doc key/role slug
// ([a-z0-9][a-z0-9-]*, key length is checked separately).
func IsDocSlug(s string) bool { return s != "" && docSlugRe.MatchString(s) }

// MaxDocKeyLen bounds a doc key slug.
const MaxDocKeyLen = 64

// DocKeyFromPath derives the default doc key from a doc path: the filename
// stem, lowercased, with every non-slug run collapsed to a hyphen. Returns ""
// when nothing usable remains (the author must then name the page explicitly).
func DocKeyFromPath(p string) string {
	stem := strings.TrimSuffix(path.Base(p), path.Ext(p))
	stem = strings.ToLower(stem)
	var b strings.Builder
	lastHyphen := true // trim leading hyphens
	for _, r := range stem {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	out := strings.TrimSuffix(b.String(), "-")
	if len(out) > MaxDocKeyLen {
		out = strings.TrimSuffix(out[:MaxDocKeyLen], "-")
	}
	return out
}

// DocTitleFromContent derives a default title: the first ATX `# H1` line of
// the doc (trimmed, capped), else the filename stem title-cased word-wise.
func DocTitleFromContent(content []byte, p string) string {
	const maxTitle = 120
	for _, line := range strings.Split(string(content), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "# ") {
			t = strings.TrimSpace(strings.TrimPrefix(t, "# "))
			if t != "" {
				if len(t) > maxTitle {
					t = strings.TrimSpace(t[:maxTitle])
				}
				return t
			}
		}
		// Only scan past blank/comment-ish leading lines; a doc whose first
		// real content line is not an H1 falls back to the filename.
		if t != "" && !strings.HasPrefix(t, "<!--") {
			break
		}
	}
	stem := strings.TrimSuffix(path.Base(p), path.Ext(p))
	words := strings.FieldsFunc(stem, func(r rune) bool { return r == '-' || r == '_' || r == ' ' })
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	if len(words) == 0 {
		return stem
	}
	return strings.Join(words, " ")
}

// ResolvedDoc is one document of an entity's resolved doc set: the declared
// identity plus the bytes read at the pinned commit (CD1). Bytes==nil means
// declared-only (missing file, over cap, dirty path) with Reason saying why —
// the entry still travels as a path pointer so the declaration is visible.
// Transient: carried on resolved manifests via a json:"-" field, never
// serialized itself.
type ResolvedDoc struct {
	Key    string
	Title  string
	Role   string // "" for the overview (the front page has no role)
	Path   string // repo-relative, forward slashes
	Commit string // the commit the bytes were read at ("" when no git state)
	SHA    string // hex sha256 of Bytes — legacy wire compat on overview only
	Bytes  []byte
	Reason string // why Bytes is nil ("" when attached)
}

// Attached reports whether the doc carries a body to walk into the closure.
func (d ResolvedDoc) Attached() bool { return len(d.Bytes) > 0 }
