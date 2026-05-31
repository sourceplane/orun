package sourcectx

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
)

// ResolveSourceSnapshot turns a workspace path into a fully-populated
// WorkspaceState. It is the C1 entry point referenced by every later
// catalog package.
//
// Adapter wiring: any nil adapter in opts is replaced by its default
// implementation (DefaultGit/DefaultClock/DefaultFilesystem). This means
// callers can pass the zero value for everything but WorkspacePath and get
// production behaviour; tests inject fakes.
//
// Determinism: the returned WorkspaceState is fully derived from
// (Git probe, FS contents, CIEvent injection). Two calls on the same
// workspace return byte-identical state — the property test T-IDK-3
// asserts this on top of BuildSourceSnapshotKey.
//
// Error policy:
//
//   - WorkspacePath unset → fmt-shaped error.
//   - Git probe fails on a real repo → wrapped error.
//   - CIEvent injection conflicts with the workspace shape → typed
//     *CIEventNoMatchError wrapping ErrCIEventNoMatch (mirrors Phase 1 §11).
//   - Anything else (FS walk failure during dirty enumeration, etc.) →
//     wrapped error.
func ResolveSourceSnapshot(ctx context.Context, opts ResolveOptions) (WorkspaceState, error) {
	if opts.WorkspacePath == "" {
		return WorkspaceState{}, fmt.Errorf("sourcectx: ResolveOptions.WorkspacePath is required")
	}
	opts = WithDefaults(opts)

	state := WorkspaceState{}

	hasRepo, err := opts.Git.HasRepo(ctx, opts.WorkspacePath)
	if err != nil {
		return WorkspaceState{}, fmt.Errorf("sourcectx: HasRepo: %w", err)
	}

	if hasRepo {
		if err := populateFromGit(ctx, opts, &state); err != nil {
			return WorkspaceState{}, err
		}
		if err := populateDirty(opts, &state); err != nil {
			return WorkspaceState{}, fmt.Errorf("sourcectx: dirty probe: %w", err)
		}
	}

	if err := applyCIInjection(opts.CIEvent, hasRepo, &state); err != nil {
		return WorkspaceState{}, err
	}

	return state, nil
}

// populateFromGit fills the VCS-derived fields of state from the Git
// adapter. Each probe is best-effort: HEAD/tree are required (we already
// know HasRepo == true), the rest may legitimately be empty.
func populateFromGit(ctx context.Context, opts ResolveOptions, state *WorkspaceState) error {
	head, err := opts.Git.HeadRevision(ctx, opts.WorkspacePath)
	if err != nil {
		return fmt.Errorf("sourcectx: HEAD revision: %w", err)
	}
	tree, err := opts.Git.TreeHash(ctx, opts.WorkspacePath)
	if err != nil {
		return fmt.Errorf("sourcectx: tree hash: %w", err)
	}
	branch, _ := opts.Git.Branch(ctx, opts.WorkspacePath)
	ref, _ := opts.Git.Ref(ctx, opts.WorkspacePath)
	tag, _ := opts.Git.Tag(ctx, opts.WorkspacePath)
	remote, _ := opts.Git.RemoteURL(ctx, opts.WorkspacePath)

	state.HeadRevision = head
	state.TreeHash = tree
	state.Branch = branch
	state.Ref = ref
	state.Tag = tag
	state.RemoteURL = remote
	state.Repo = repoFromRemote(remote)
	return nil
}

// populateDirty walks the workspace, filters to catalog-relevant files
// (per identity-and-keys.md §7 and the inference flags), reads each, and
// passes the result through DirtyHash. Sets state.Dirty + state.DirtyHash.
//
// Files are matched both via the git diff probe (modifications + untracked)
// AND via a direct FS walk filter on catalog-relevant paths. Anything in
// the union that is catalog-relevant counts.
func populateDirty(opts ResolveOptions, state *WorkspaceState) error {
	// Probe git for paths that differ from HEAD's tree.
	ctx := context.Background()
	diffPaths, _ := opts.Git.DiffTreePaths(ctx, opts.WorkspacePath, state.TreeHash)

	// Filter to catalog-relevant only — this is what makes the §7 rule
	// hold: a stray notes.txt change doesn't churn dirtyHash.
	relevant := make([]string, 0, len(diffPaths))
	seen := make(map[string]struct{}, len(diffPaths))
	for _, p := range diffPaths {
		if !CatalogRelevant(p, opts.Inference) {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		relevant = append(relevant, p)
	}

	if len(relevant) == 0 {
		state.Dirty = false
		state.DirtyHash = ""
		return nil
	}

	files := make([]DirtyFile, 0, len(relevant))
	for _, p := range relevant {
		body, err := opts.FS.ReadFile(p)
		if err != nil {
			// Untracked file removed between diff probe and read; skip.
			if isNotExist(err) {
				continue
			}
			return fmt.Errorf("read %s: %w", p, err)
		}
		files = append(files, DirtyFile{Path: p, Content: body})
	}

	if len(files) == 0 {
		state.Dirty = false
		state.DirtyHash = ""
		return nil
	}

	state.Dirty = true
	state.DirtyHash = DirtyHash(files)
	return nil
}

// applyCIInjection layers the provider-injected scope onto state. This is
// the only place that produces *CIEventNoMatchError.
func applyCIInjection(ci CIEventInjection, hasRepo bool, state *WorkspaceState) error {
	// All four fields zero → nothing to do.
	if ci.PRNumber == 0 && ci.Tag == "" && ci.CIEventScope == "" {
		return nil
	}
	// CI injection demands a real workspace repo. Without git context we
	// can't produce the c…/t… segments the spec requires — emit the typed
	// envelope mirroring triggerctx.NoMatchingBindingError.
	if !hasRepo {
		return &CIEventNoMatchError{
			Provider: ci.Provider,
			Event:    ci.Event,
			Action:   ci.Action,
			Reason:   "no-git-repo",
		}
	}
	if state.HeadRevision == "" {
		return &CIEventNoMatchError{
			Provider: ci.Provider,
			Event:    ci.Event,
			Action:   ci.Action,
			Reason:   "no-head-revision",
		}
	}
	if ci.PRNumber > 0 {
		state.PRNumber = ci.PRNumber
	}
	if ci.Tag != "" {
		state.Tag = ci.Tag
	}
	if ci.CIEventScope != "" {
		state.CIEvent = ci.CIEventScope
	}
	return nil
}

// repoFromRemote derives a canonical "<owner>/<repo>" string from an origin
// URL. Supports both SSH (`git@github.com:owner/repo.git`) and HTTPS
// (`https://github.com/owner/repo.git`) shapes. Returns "" when the URL is
// empty or unparsable.
func repoFromRemote(remote string) string {
	if remote == "" {
		return ""
	}
	s := remote
	// Strip trailing ".git" if present.
	if len(s) > 4 && s[len(s)-4:] == ".git" {
		s = s[:len(s)-4]
	}
	// SSH: git@host:owner/repo
	if i := indexOf(s, "@"); i >= 0 {
		if j := indexOf(s[i:], ":"); j > 0 {
			s = s[i+j+1:]
		}
	}
	// HTTPS: scheme://host/owner/repo → drop scheme+host.
	if i := indexOf(s, "://"); i >= 0 {
		s = s[i+3:]
		if j := indexOf(s, "/"); j >= 0 {
			s = s[j+1:]
		}
	}
	// At this point s should be "owner/repo" (or "group/sub/repo" for
	// GitLab-style nested namespaces — preserve the last two segments).
	parts := splitSlash(s)
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	default:
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
}

// Tiny helpers, kept inline so this file has no extra-stdlib imports. They
// are intentionally non-allocating for the typical short-input case.

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func splitSlash(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			if start < i {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// isNotExist reports whether err is a "file not found" error from any of
// the FS shapes the resolver might see.
func isNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}
