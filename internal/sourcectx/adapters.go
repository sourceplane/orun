package sourcectx

import (
	"context"
	"io/fs"
	"time"
)

// Git is the source-control adapter the resolver depends on. It is the only
// way the resolver may touch a real git repository — both the in-process
// (`go-git`) and the shell-out (`git`) implementations satisfy this surface.
//
// Methods that succeed on a workspace with no git repo at all return
// (zero-value, false, nil). Methods that hit a real failure (corrupt repo,
// unreadable .git, etc.) return a non-nil error. The HasRepo bool is the
// signal callers use to distinguish "no git" from "git is broken".
//
// All methods are read-only. Implementations must NOT mutate the workspace.
type Git interface {
	// HasRepo reports whether workspacePath is inside a git working tree.
	// false + nil error means "no git repo here" (local-nogit scope).
	// A non-nil error means the probe itself failed.
	HasRepo(ctx context.Context, workspacePath string) (bool, error)

	// HeadRevision returns the full hex SHA at HEAD. Empty string + nil
	// error if HasRepo is false (caller must check first).
	HeadRevision(ctx context.Context, workspacePath string) (string, error)

	// TreeHash returns the full hex SHA from `git rev-parse HEAD^{tree}`.
	TreeHash(ctx context.Context, workspacePath string) (string, error)

	// Branch returns the short branch name at HEAD ("" for detached HEAD).
	Branch(ctx context.Context, workspacePath string) (string, error)

	// Ref returns the symbolic ref ("refs/heads/main"), or "" when detached.
	Ref(ctx context.Context, workspacePath string) (string, error)

	// Tag returns the annotated tag at HEAD if any, otherwise "".
	Tag(ctx context.Context, workspacePath string) (string, error)

	// RemoteURL returns the URL of `origin`, or "" if no remote.
	RemoteURL(ctx context.Context, workspacePath string) (string, error)

	// DiffTreePaths returns the set of repo-relative paths that differ
	// between treeHash (the committed HEAD tree) and the working tree —
	// i.e. uncommitted modifications, including untracked files. The
	// resolver uses this set to populate the dirty file probe.
	DiffTreePaths(ctx context.Context, workspacePath, treeHash string) ([]string, error)
}

// Clock is the time adapter. Single method by design — the resolver only
// timestamps the snapshot.
type Clock interface {
	Now() time.Time
}

// Filesystem is the read-only FS adapter the resolver uses to enumerate
// catalog-relevant dirty files. The default implementation in fs.go wraps
// os.* calls; tests inject a faked impl to avoid touching disk.
type Filesystem interface {
	// Walk visits every file rooted at root. fn receives a repo-relative
	// POSIX path and an fs.DirEntry. fn may return fs.SkipDir to skip an
	// entire directory (used to prune `.git/`, `node_modules/`, etc.).
	Walk(root string, fn func(relPath string, d fs.DirEntry) error) error

	// Stat returns the FileInfo for an absolute or root-relative path.
	Stat(path string) (fs.FileInfo, error)

	// ReadFile reads the entire file at path. Path may be absolute or
	// relative to the workspace root.
	ReadFile(path string) ([]byte, error)
}

// InferenceFlags mirrors the `intent.yaml` `catalog.inference.*` block. The
// resolver uses it to decide which file kinds count as "catalog-relevant"
// for dirty-hash purposes (identity-and-keys.md §7). Callers parse
// intent.yaml and populate this struct; the resolver never imports a YAML
// parser.
type InferenceFlags struct {
	Readme      bool
	PackageJSON bool
	Dockerfile  bool
	Helm        bool
	Terraform   bool
}

// CIEventInjection carries the provider-injected scope produced by a CI
// runner (e.g. `--from-ci`). The resolver consumes it verbatim — it never
// re-derives PR/tag/branch from environment variables on its own.
//
// All fields are optional; ResolveSourceSnapshot is the place that decides
// whether the combination is consistent (and emits the §11-shaped error
// envelope when it is not).
type CIEventInjection struct {
	// PRNumber is non-zero when CI is targeting a pull request.
	PRNumber int
	// Tag is non-empty when CI is targeting an annotated tag.
	Tag string
	// CIEventScope is the provider-supplied scope segment (e.g.
	// "ci-pr139") that overrides the natural scope detection. Empty when
	// the runner has no opinion.
	CIEventScope string
	// Provider / Event / Action mirror the triggerctx fields and are used
	// to shape the CIEventNoMatchError envelope when the injection
	// conflicts with the actual workspace state.
	Provider string
	Event    string
	Action   string
}

// ResolveOptions parameterizes ResolveSourceSnapshot. The zero value is
// useless; callers must at minimum set WorkspacePath. Unset adapters are
// auto-filled with their default implementations by WithDefaults.
type ResolveOptions struct {
	// WorkspacePath is the absolute path to the workspace root. Required.
	WorkspacePath string

	// Git / Clock / FS are the adapter triple. Nil → DefaultGit() /
	// DefaultClock() / DefaultFilesystem(WorkspacePath).
	Git   Git
	Clock Clock
	FS    Filesystem

	// Inference toggles whose set of files counts as catalog-relevant
	// when computing dirtyHash. Defaults to "everything off" so callers
	// who skip intent.yaml parsing entirely still get a well-defined
	// dirty set (the always-on files: intent.yaml, component.yaml,
	// stack/composition refs).
	Inference InferenceFlags

	// IntentCanonical is the pre-canonicalised JSON of the
	// `intent.yaml.catalog` block. The resolver passes this through to
	// CatalogInputHashInputs unchanged.
	IntentCanonical []byte

	// OrunVersion / ResolverVersion / SchemaVersion / StackSources feed
	// CatalogInputHash. Resolver does not invent values.
	OrunVersion     string
	ResolverVersion int
	SchemaVersion   string
	StackSources    []string

	// CIEvent carries provider-injected CI scope (PR/tag/event) when the
	// caller is running under `--from-ci`. Zero value means "no CI
	// injection — derive scope from the workspace".
	CIEvent CIEventInjection
}

// WithDefaults returns a copy of opts with nil adapters filled in. It is
// intentionally separate from ResolveSourceSnapshot so tests can dry-run the
// defaulting independent of the resolve.
func WithDefaults(opts ResolveOptions) ResolveOptions {
	if opts.Git == nil {
		opts.Git = DefaultGit()
	}
	if opts.Clock == nil {
		opts.Clock = DefaultClock()
	}
	if opts.FS == nil {
		opts.FS = DefaultFilesystem(opts.WorkspacePath)
	}
	return opts
}
