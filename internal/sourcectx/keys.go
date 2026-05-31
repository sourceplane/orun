package sourcectx

import (
	"strings"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// BuildSourceSnapshotKey assembles the on-disk SourceSnapshotKey for a
// WorkspaceState. Pure: no FS / git / network calls. The C1 git probe is
// responsible for filling WorkspaceState; this function is the spec-grounded
// transform from state → key.
//
// Implements identity-and-keys.md §2 in full: scope detection,
// branch sanitization, head/tree/dirty segments, and the §2 rule-5
// length-collapse delegated to catalogmodel.FormatSourceSnapshotKey.
func BuildSourceSnapshotKey(w WorkspaceState) string {
	parts := catalogmodel.SourceKeyParts{
		Scope:      buildScopeSegment(w),
		HeadShort:  shortRevision(w.HeadRevision, 8),
		TreeShort:  shortRevision(w.TreeHash, 7),
		DirtyShort: dirtyShort(w.DirtyHash),
	}
	return catalogmodel.FormatSourceSnapshotKey(parts)
}

// buildScopeSegment renders the scope portion of a SourceSnapshotKey. For
// `branch-<name>`, the branch is run through SanitizeBranch.
func buildScopeSegment(w WorkspaceState) string {
	switch w.Scope() {
	case catalogmodel.SourceScopeBranchMain:
		return "branch-main"
	case catalogmodel.SourceScopeBranchProtected, catalogmodel.SourceScopeBranchFeature:
		return "branch-" + catalogmodel.SanitizeBranch(w.Branch)
	case catalogmodel.SourceScopePR:
		// pr<num> — no separator before the number, per §2 examples.
		return "pr" + itoa(w.PRNumber)
	case catalogmodel.SourceScopeTag:
		return "tag-" + catalogmodel.SanitizeBranch(w.Tag)
	case catalogmodel.SourceScopeLocalDirty:
		return "branch-" + catalogmodel.SanitizeBranch(w.Branch)
	case catalogmodel.SourceScopeLocalNoGit:
		return "local-nogit"
	case catalogmodel.SourceScopeCIEvent:
		return catalogmodel.SanitizeBranch(w.CIEvent)
	default:
		return "branch-main"
	}
}

// shortRevision returns the first n hex characters of revision, lowercased.
// Returns "" if revision is shorter than n. Used for the head/tree segments
// of a SourceSnapshotKey.
func shortRevision(revision string, n int) string {
	revision = strings.ToLower(strings.TrimSpace(revision))
	if len(revision) < n {
		return ""
	}
	return revision[:n]
}

// dirtyShort extracts the short dirty hash for the d-segment. Accepts either
// a `sha256:<hex>` value or a bare hex string. Returns "" if the input is
// empty or shorter than 9 hex chars.
func dirtyShort(full string) string {
	full = strings.TrimPrefix(full, "sha256:")
	return catalogmodel.ShortHex(full, 9)
}

// itoa is a small zero-alloc int→decimal helper to avoid pulling in strconv
// just for two callers. Negative inputs render as their absolute value (the
// spec rules out negative PR numbers).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
