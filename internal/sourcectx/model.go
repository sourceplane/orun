package sourcectx

// WorkspaceState is the in-memory description of the workspace VCS context
// the catalog resolver will run against. C1's git probe populates this; C0
// only fixes the shape.
//
// Field-for-field a superset of catalogmodel.SourceSnapshot but kept separate
// so the resolver's upstream inputs (raw git output) are decoupled from the
// persisted on-disk record. Conversion is a one-way function in C1.
type WorkspaceState struct {
	// Repo is the canonical "<owner>/<repo>" string derived from the workspace
	// remote URL. Empty when the workspace has no git remote.
	Repo string
	// RemoteURL is the raw `git remote get-url origin` value. Empty when the
	// workspace has no git remote.
	RemoteURL string
	// Ref is the symbolic ref name (e.g. "refs/heads/main"). Empty for
	// detached HEAD or local-nogit.
	Ref string
	// Branch is the short branch name. Empty for detached HEAD or local-nogit.
	Branch string
	// HeadRevision is the short SHA at HEAD (12+ hex chars). Empty for
	// local-nogit.
	HeadRevision string
	// TreeHash is the short tree SHA at HEAD (7+ hex chars). Empty for
	// local-nogit.
	TreeHash string
	// Dirty is true when the workspace has uncommitted changes touching
	// catalog-relevant files (per identity-and-keys.md §7).
	Dirty bool
	// DirtyHash is the full sha256 (with "sha256:" prefix) over
	// catalog-relevant dirty files. Empty when Dirty is false.
	DirtyHash string
	// PRNumber is non-zero when the resolver is invoked under a CI event
	// targeting a pull request.
	PRNumber int
	// Tag is non-empty when HEAD is at an annotated tag.
	Tag string
	// CIEvent is the provider-injected event scope (e.g. "ci-pr139"). Empty
	// outside CI.
	CIEvent string
}

// Scope returns the SourceScope enum value derived from this state per
// identity-and-keys.md §2. The mapping is total — every WorkspaceState
// produces exactly one scope.
//
// Precedence (from spec §2 rule 3): PR > tag > branch > local. CI events use
// the explicit CIEvent string when set.
func (w WorkspaceState) Scope() string {
	switch {
	case w.CIEvent != "":
		return "ci-event"
	case w.PRNumber > 0:
		return "pr"
	case w.Tag != "":
		return "tag"
	case w.HeadRevision == "":
		// No git context at all.
		return "local-nogit"
	case w.Dirty:
		return "local-dirty"
	case w.Branch == "main":
		return "branch-main"
	default:
		return "branch-feature"
	}
}
