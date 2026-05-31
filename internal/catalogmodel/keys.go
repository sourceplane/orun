package catalogmodel

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/oklog/ulid/v2"
)

// ID prefix constants per identity-and-keys.md §6. Used for both
// construction (NewSourceSnapshotID etc.) and validation (HasPrefix on
// untrusted input).
const (
	IDPrefixSource    = "src_"
	IDPrefixCatalog   = "cat_"
	IDPrefixComponent = "cmp_"
)

// SourceSnapshotKey-related constants per identity-and-keys.md §2.
const (
	// SourceKeyMaxLen is the maximum allowed length of a SourceSnapshotKey
	// before the trailing-segment hashing rule kicks in (rule 5).
	SourceKeyMaxLen = 128
	// SourceKeyPrefix is the literal `src-` prefix every source snapshot key
	// starts with.
	SourceKeyPrefix = "src-"
)

var (
	// sourceKeyPattern is the on-disk regex every well-formed SourceSnapshotKey
	// must satisfy. Mirrors data-model.md §1 validation.
	sourceKeyPattern = regexp.MustCompile(`^src-[a-z0-9-]{1,128}$`)
	// catalogKeyPattern is the regex CatalogSnapshotKey must satisfy
	// (`cat-<6..16 hex>`, plus optional `-x<n>` collision suffix).
	catalogKeyPattern = regexp.MustCompile(`^cat-[a-f0-9]{6,16}(-x[0-9]+)?$`)
	// componentKeyPattern is the 3-segment `<namespace>/<repo>/<name>` shape.
	// Each segment matches [a-z0-9._-]+ per identity-and-keys.md §4.
	componentKeyPattern = regexp.MustCompile(`^[a-z0-9._-]+/[a-z0-9._-]+/[a-z0-9._-]+$`)
)

// ErrInvalidKey is returned by every Validate*Key function on a malformed
// input. Callers may pair it with their own context.
var ErrInvalidKey = errors.New("catalogmodel: invalid key")

// NewSourceSnapshotID returns a fresh `src_<ulid>` identifier. The ULID is
// monotonic per process via the package-level entropy source.
func NewSourceSnapshotID() string { return IDPrefixSource + nextULID() }

// NewCatalogSnapshotID returns a fresh `cat_<ulid>` identifier.
func NewCatalogSnapshotID() string { return IDPrefixCatalog + nextULID() }

// NewComponentID returns a fresh `cmp_<ulid>` identifier.
func NewComponentID() string { return IDPrefixComponent + nextULID() }

// HasIDPrefix reports whether id begins with one of the catalog ID prefixes
// (src_, cat_, cmp_) followed by at least one ULID character.
func HasIDPrefix(id string) bool {
	for _, p := range []string{IDPrefixSource, IDPrefixCatalog, IDPrefixComponent} {
		if strings.HasPrefix(id, p) && len(id) > len(p) {
			return true
		}
	}
	return false
}

// SourceKeyParts is the structured form of a SourceSnapshotKey. Resolution
// (the FormatSourceSnapshotKey input) belongs in C1 — this file owns the
// pure construction + validation surface.
type SourceKeyParts struct {
	// Scope is one of {branch-main, branch-<name>, pr<num>, tag-<name>,
	// local-dirty, local-nogit, ci-event} per identity-and-keys.md §2. The
	// caller is responsible for SanitizeBranch on `<name>`-bearing scopes.
	Scope string
	// HeadShort is the 7+ char short SHA prefixed `c…`. Empty for local-nogit.
	HeadShort string
	// TreeShort is the 7+ char tree hash prefixed `t…`. Empty for local-nogit.
	TreeShort string
	// DirtyShort is the 9-char dirty-hash prefix. Empty when working tree is
	// clean.
	DirtyShort string
}

// FormatSourceSnapshotKey assembles a SourceSnapshotKey per
// identity-and-keys.md §2. Pure construction — no FS or git calls.
//
// Rules enforced here:
//   - local-nogit scope: only the dirty segment is included; head/tree are
//     ignored even if set.
//   - other scopes: head + tree are required; dirty is appended only when
//     non-empty.
//   - the assembled key is run through the §2 rule-5 length collapse: if it
//     exceeds 128 chars, the trailing scope segment is hashed.
//
// The output is always validated against ValidateSourceSnapshotKey before
// return; a violation surfaces as a non-empty key + nil error from this
// function (the validator is the source of truth — surface the validator's
// error from the caller).
func FormatSourceSnapshotKey(p SourceKeyParts) string {
	if p.Scope == SourceScopeLocalNoGit {
		if p.DirtyShort == "" {
			return SourceKeyPrefix + p.Scope
		}
		return SourceKeyPrefix + p.Scope + "-d" + p.DirtyShort
	}
	var b strings.Builder
	b.Grow(SourceKeyMaxLen)
	b.WriteString(SourceKeyPrefix)
	b.WriteString(p.Scope)
	if p.HeadShort != "" {
		b.WriteString("-c")
		b.WriteString(p.HeadShort)
	}
	if p.TreeShort != "" {
		b.WriteString("-t")
		b.WriteString(p.TreeShort)
	}
	if p.DirtyShort != "" {
		b.WriteString("-d")
		b.WriteString(p.DirtyShort)
	}
	out := b.String()
	if len(out) > SourceKeyMaxLen {
		// Hash the original scope into 8 hex; recurse with the shrunk Scope.
		hashed := SanitizeBranch(p.Scope) // SanitizeBranch always returns ≤40
		if len(hashed) > 8 {
			hashed = hashed[:8]
		}
		shrunk := p
		shrunk.Scope = hashed
		out = FormatSourceSnapshotKey(shrunk)
	}
	return out
}

// ValidateSourceSnapshotKey reports a non-nil error when key does not match
// the on-disk shape. Used by readers (writer + reader will lean on this when
// they land in C2/C3) and by the constructor's tests.
func ValidateSourceSnapshotKey(key string) error {
	if !sourceKeyPattern.MatchString(key) {
		return fmt.Errorf("%w: %q does not match %s", ErrInvalidKey, key, sourceKeyPattern)
	}
	if len(key) > SourceKeyMaxLen {
		return fmt.Errorf("%w: %q exceeds %d chars", ErrInvalidKey, key, SourceKeyMaxLen)
	}
	return nil
}

// FormatCatalogSnapshotKey returns `cat-<short>` where short is the first
// `width` hex chars of catalogHash. `width` must be in [6, 16]; out-of-range
// values are clamped.
//
// Collision policy (identity-and-keys.md §3) is the writer's job (C3) — this
// helper only produces the un-suffixed form.
func FormatCatalogSnapshotKey(catalogHashHex string, width int) string {
	if width < 6 {
		width = 6
	}
	if width > 16 {
		width = 16
	}
	short := ShortHex(strings.TrimPrefix(catalogHashHex, "sha256:"), width)
	return "cat-" + short
}

// ValidateCatalogSnapshotKey reports a non-nil error when key does not match
// the documented shape (`cat-<6..16 hex>` with an optional `-x<n>` suffix).
func ValidateCatalogSnapshotKey(key string) error {
	if !catalogKeyPattern.MatchString(key) {
		return fmt.Errorf("%w: %q does not match %s", ErrInvalidKey, key, catalogKeyPattern)
	}
	return nil
}

// FormatComponentKey assembles `<namespace>/<repo>/<name>`. The caller is
// responsible for ensuring each segment matches `[a-z0-9._-]+` (the resolver
// in C2 enforces this; we re-validate here defensively).
func FormatComponentKey(namespace, repo, name string) string {
	return namespace + "/" + repo + "/" + name
}

// ValidateComponentKey reports a non-nil error when componentKey does not
// match the 3-segment shape required by identity-and-keys.md §4.
func ValidateComponentKey(componentKey string) error {
	if !componentKeyPattern.MatchString(componentKey) {
		return fmt.Errorf("%w: %q does not match %s", ErrInvalidKey, componentKey, componentKeyPattern)
	}
	return nil
}

// SourceKeyPattern returns the regexp every well-formed SourceSnapshotKey
// must satisfy. Exposed so sibling packages (C1 sourcectx, C3 catalogstore)
// can validate without re-deriving the rule.
func SourceKeyPattern() *regexp.Regexp { return sourceKeyPattern }

// CatalogKeyPattern returns the regexp every well-formed CatalogSnapshotKey
// must satisfy.
func CatalogKeyPattern() *regexp.Regexp { return catalogKeyPattern }

// ComponentKeyPattern returns the regexp every well-formed componentKey
// must satisfy.
func ComponentKeyPattern() *regexp.Regexp { return componentKeyPattern }

// nextULID returns a fresh monotonic ULID string. Wraps the package-level
// entropy source so callers don't need to thread one through.
func nextULID() string { return newULID().String() }

// newULID is split out for tests that pin time. Production callers go
// through nextULID.
func newULID() ulid.ULID {
	monoMu.Lock()
	defer monoMu.Unlock()
	return ulid.MustNew(ulid.Timestamp(nowFn()), monoEntropy)
}
