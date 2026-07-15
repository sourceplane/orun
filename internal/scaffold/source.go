package scaffold

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/composition"
	"github.com/sourceplane/orun/internal/objectstore"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// ResolvedSource is a source pinned by digest into the object store, plus a
// source-agnostic read view for the placement engine (design §5).
type ResolvedSource struct {
	Name   string
	Kind   SourceKind
	Digest objectstore.ObjectID
	Tree   FileTree
}

// resolveSources resolves every declared source into the object store, pinning
// each by digest before any module reads it (design §5). Placement then reads
// through a FileTree, so it never branches on where bytes came from.
//
// A scratch directory (under workDir) is used to materialize git/oci fetches
// before snapshotting; the caller owns cleanup of workDir.
func resolveSources(ctx context.Context, store objectstore.ObjectStore, sources []SourceSpec, ignore []string, baseDir, workDir string) (map[string]ResolvedSource, error) {
	out := make(map[string]ResolvedSource, len(sources))
	for _, s := range sources {
		rs, err := resolveSource(ctx, store, s, ignore, baseDir, workDir)
		if err != nil {
			return nil, err
		}
		out[s.Name] = rs
	}
	return out, nil
}

func resolveSource(ctx context.Context, store objectstore.ObjectStore, s SourceSpec, ignore []string, baseDir, workDir string) (ResolvedSource, error) {
	switch s.Kind {
	case SourceInline:
		// Inline sources carry no fetched content; modules using them supply
		// their own Files. Pin an empty tree so provenance still has a digest.
		id, err := store.PutTree(ctx, nil)
		if err != nil {
			return ResolvedSource{}, err
		}
		return ResolvedSource{Name: s.Name, Kind: s.Kind, Digest: id, Tree: inlineTree{files: map[string]string{}}}, nil

	case SourceDir:
		if s.Path == "" {
			return ResolvedSource{}, notFoundErr("source %q (dir): path is required", s.Name)
		}
		// Resolve a relative dir path against the blueprint's directory so a
		// scaffold works from any CWD.
		path := s.Path
		if baseDir != "" && !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, path)
		}
		return snapshotAndPin(ctx, store, s, path, ignore)

	case SourceGit:
		dir, err := fetchGit(s, workDir)
		if err != nil {
			return ResolvedSource{}, err
		}
		return snapshotAndPin(ctx, store, s, dir, ignore)

	case SourceOCI:
		if s.Package == "" {
			return ResolvedSource{}, notFoundErr("source %q (oci): package is required", s.Name)
		}
		dest := filepath.Join(workDir, "oci-"+s.Name)
		if _, err := composition.FetchToDir(s.Package, dest); err != nil {
			return ResolvedSource{}, notFoundErr("source %q (oci): %v", s.Name, err)
		}
		return snapshotAndPin(ctx, store, s, dest, ignore)

	default:
		return ResolvedSource{}, notFoundErr("source %q: unknown kind %q", s.Name, s.Kind)
	}
}

// snapshotAndPin content-addresses a directory into the object store and pins
// it by a single Merkle digest, then reads placement bytes back through an
// osTree over the same tree. The digest is what makes a scaffold reproducible +
// provenanced (design §5, §11): identical directory content ⇒ identical digest.
//
// Content-addressing is over blobs (PutBlob, no name constraint) plus a
// manifest hash over sorted (relpath, blob-digest) pairs — NOT the object
// store's named-tree encoding, whose entry-name alphabet ([A-Za-z0-9._-])
// cannot represent real repo paths (e.g. a Next.js `[envSlug]` route segment).
func snapshotAndPin(ctx context.Context, store objectstore.ObjectStore, s SourceSpec, dir string, ignore []string) (ResolvedSource, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return ResolvedSource{}, notFoundErr("source %q: %v", s.Name, err)
	}
	if !info.IsDir() {
		return ResolvedSource{}, notFoundErr("source %q: %q is not a directory", s.Name, dir)
	}
	tree := osTree{root: dir, ignore: ignore}
	digest, err := snapshotDigest(ctx, store, tree)
	if err != nil {
		return ResolvedSource{}, fmt.Errorf("source %q: snapshot: %w", s.Name, err)
	}
	if s.Digest != "" && string(digest) != s.Digest {
		return ResolvedSource{}, gateErr("source %q: resolved digest %s != pinned digest %s", s.Name, digest, s.Digest)
	}
	return ResolvedSource{Name: s.Name, Kind: s.Kind, Digest: digest, Tree: tree}, nil
}

// snapshotDigest content-addresses every file the tree exposes (PutBlob) and
// returns a deterministic Merkle digest over the sorted (relpath → blob-digest)
// manifest. It reads through the same ignore-aware osTree used for placement, so
// the digest excludes exactly what a scaffold would (VCS/derived dirs +
// blueprint ignores) — keeping it stable across checkouts.
func snapshotDigest(ctx context.Context, store objectstore.ObjectStore, tree osTree) (objectstore.ObjectID, error) {
	files, err := tree.List("")
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	h := sha256.New()
	for _, rel := range files {
		data, rerr := tree.ReadFile(rel)
		if rerr != nil {
			return "", rerr
		}
		id, berr := store.PutBlob(ctx, data)
		if berr != nil {
			return "", berr
		}
		fmt.Fprintf(h, "%s\x00%s\n", rel, string(id))
	}
	return objectstore.ObjectID(fmt.Sprintf("sha256:%x", h.Sum(nil))), nil
}

// fetchGit resolves repo@ref to an immutable commit and materializes its tree
// into a scratch dir (design §5). This is the one genuinely new fetch path.
func fetchGit(s SourceSpec, workDir string) (string, error) {
	if s.Repo == "" {
		return "", notFoundErr("source %q (git): repo is required", s.Name)
	}
	dest := filepath.Join(workDir, "git-"+s.Name)
	url := s.Repo
	if !strings.Contains(url, "://") && !strings.HasPrefix(url, "git@") {
		// A bare host/path (github.com/org/repo) or a local path. Prefer a
		// local checkout if it exists on disk, else assume https.
		if _, err := os.Stat(url); err != nil {
			url = "https://" + url
		}
	}
	opts := &git.CloneOptions{URL: url, Depth: 1}
	if s.Ref != "" {
		opts.ReferenceName = plumbing.NewBranchReferenceName(s.Ref)
		opts.SingleBranch = true
	}
	repo, err := git.PlainClone(dest, false, opts)
	if err != nil {
		// Retry treating ref as a tag.
		if s.Ref != "" {
			_ = os.RemoveAll(dest)
			opts.ReferenceName = plumbing.NewTagReferenceName(s.Ref)
			repo, err = git.PlainClone(dest, false, opts)
		}
		if err != nil {
			return "", notFoundErr("source %q (git): clone %s@%s: %v", s.Name, s.Repo, s.Ref, err)
		}
	}
	if _, err := repo.Head(); err != nil {
		return "", notFoundErr("source %q (git): resolve head: %v", s.Name, err)
	}
	return dest, nil
}
