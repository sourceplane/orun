// Package sourcedigest computes a content digest of the source tree a job
// runs against, for the hermetic memo key (specs/orun-cache-efficiency CE6):
// a job labelled hermetic is served a prior result only when the files under
// its directory are the same as when that result was produced.
package sourcedigest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Digest is "sha256:<hex>" over the files under <root>/<rel> (the whole tree
// when rel is empty). In a git checkout it is taken from the index — each
// tracked file's mode and blob id, which git keeps current — plus the content
// of every file the work tree changed, added or deleted under rel, so it
// moves with an edit before it is staged and ignores what .gitignore ignores
// (build outputs, node_modules). Outside git every regular file under rel is
// hashed, less the dependency and output directories a job recreates.
func Digest(root, rel string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	rel = filepath.Clean(filepath.FromSlash(strings.TrimSpace(rel)))
	if rel == "." || rel == "" || rel == string(filepath.Separator) {
		rel = "."
	}
	if filepath.IsAbs(rel) {
		if r, err := filepath.Rel(root, rel); err == nil && !strings.HasPrefix(r, "..") {
			rel = r
		} else {
			return "", fmt.Errorf("source path %s is outside %s", rel, root)
		}
	}
	if info, err := os.Stat(filepath.Join(root, rel)); err != nil || !info.IsDir() {
		return "", fmt.Errorf("source directory %s: %w", filepath.Join(root, rel), errOrNotDir(err))
	}
	if listing, ok := gitListing(root, rel); ok {
		return sum(listing), nil
	}
	listing, err := walkListing(root, rel)
	if err != nil {
		return "", err
	}
	return sum(listing), nil
}

func errOrNotDir(err error) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("not a directory")
}

func sum(listing []byte) string {
	h := sha256.Sum256(listing)
	return "sha256:" + hex.EncodeToString(h[:])
}

// gitListing is the index entries under rel plus the work tree's changes to
// them; ok is false when root is not a git checkout or git is unavailable.
func gitListing(root, rel string) ([]byte, bool) {
	index, err := git(root, "ls-files", "-s", "-z", "--", rel)
	if err != nil {
		return nil, false
	}
	status, err := git(root, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames", "--", rel)
	if err != nil {
		return nil, false
	}
	var buf bytes.Buffer
	buf.WriteString("index\x00")
	for _, entry := range bytes.Split(index, []byte{0}) {
		if len(entry) == 0 {
			continue
		}
		buf.Write(entry)
		buf.WriteByte(0)
	}
	buf.WriteString("worktree\x00")
	var changed []string
	for _, entry := range bytes.Split(status, []byte{0}) {
		if len(entry) < 4 {
			continue
		}
		path := string(entry[3:])
		changed = append(changed, path)
	}
	sort.Strings(changed)
	for _, path := range changed {
		buf.WriteString(path)
		buf.WriteByte(0)
		writeFileDigest(&buf, filepath.Join(root, filepath.FromSlash(path)))
		buf.WriteByte(0)
	}
	return buf.Bytes(), true
}

func git(root string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// skippedDirs are what a job recreates or git would ignore: not source.
var skippedDirs = map[string]struct{}{
	".git": {}, ".orun": {}, "node_modules": {}, ".pnpm-store": {},
	".next": {}, ".nuxt": {}, ".turbo": {}, ".svelte-kit": {}, "dist": {}, "build": {}, "target": {},
	".venv": {}, "venv": {}, "__pycache__": {}, ".terraform": {}, ".pytest_cache": {}, ".mypy_cache": {},
}

// walkListing is every regular file and symlink under rel, with its mode
// and content digest, for a tree outside git.
func walkListing(root, rel string) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("walk\x00")
	start := filepath.Join(root, rel)
	err := filepath.WalkDir(start, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if _, skip := skippedDirs[d.Name()]; skip && path != start {
				return filepath.SkipDir
			}
			return nil
		}
		relPath, _ := filepath.Rel(root, path)
		buf.WriteString(filepath.ToSlash(relPath))
		buf.WriteByte(0)
		if d.Type()&fs.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			buf.WriteString("link:" + target)
			buf.WriteByte(0)
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(&buf, "%o\x00", info.Mode().Perm())
		writeFileDigest(&buf, path)
		buf.WriteByte(0)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeFileDigest appends the file's content digest, "deleted" for a path
// that is gone (a deletion git status reported), or "link:<target>".
func writeFileDigest(buf *bytes.Buffer, path string) {
	info, err := os.Lstat(path)
	if err != nil {
		buf.WriteString("deleted")
		return
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		target, _ := os.Readlink(path)
		buf.WriteString("link:" + target)
		return
	}
	if info.IsDir() {
		buf.WriteString("dir")
		return
	}
	f, err := os.Open(path)
	if err != nil {
		buf.WriteString("unreadable")
		return
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		buf.WriteString("unreadable")
		return
	}
	buf.WriteString(hex.EncodeToString(h.Sum(nil)))
}
