package scaffold

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// FileTree is the source-agnostic, read-only view the placement engine reads
// module content from (design §5). inline, dir, oci, and git sources all present
// this same interface, so placement never branches on where bytes came from.
// Paths are always slash-separated and relative to the tree root.
type FileTree interface {
	// ReadFile returns the bytes at rel.
	ReadFile(rel string) ([]byte, error)
	// List returns all file paths under subpath (recursively), sorted. An
	// empty subpath lists the whole tree.
	List(subpath string) ([]string, error)
}

// inlineTree presents a module's inline Files map as a FileTree. Keys are
// target-relative paths carried in the blueprint itself.
type inlineTree struct {
	files map[string]string
}

func (t inlineTree) ReadFile(rel string) ([]byte, error) {
	body, ok := t.files[rel]
	if !ok {
		return nil, fmt.Errorf("inline file %q not found", rel)
	}
	return []byte(body), nil
}

func (t inlineTree) List(subpath string) ([]string, error) {
	subpath = path.Clean("/" + subpath)
	out := make([]string, 0, len(t.files))
	for name := range t.files {
		if subpath == "/" || strings.HasPrefix(path.Clean("/"+name)+"/", subpath+"/") {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}

// osTree presents an on-disk directory as a FileTree (used by dir/git/oci
// sources). It is read-only, refuses to escape its root, and skips VCS/derived
// dirs plus any blueprint-declared ignore patterns.
type osTree struct {
	root   string
	ignore []string
}

// ignored reports whether a source-relative path is excluded: a bare ignore
// entry matches any path segment (".next"), a glob matches the whole path via
// path.Match ("**/dist" → dist anywhere). Always skips .git/.orun/node_modules.
func (t osTree) ignored(rel string) bool {
	segs := strings.Split(rel, "/")
	for _, seg := range segs {
		switch seg {
		case ".git", ".orun", "node_modules":
			return true
		}
	}
	for _, pat := range t.ignore {
		if !strings.ContainsAny(pat, "*?[") {
			for _, seg := range segs {
				if seg == pat {
					return true
				}
			}
			continue
		}
		// Glob: match the full path and its basename; a "**/" prefix means
		// "at any depth", handled by also testing each trailing subpath.
		clean := strings.TrimPrefix(pat, "**/")
		if ok, _ := path.Match(clean, rel); ok {
			return true
		}
		if ok, _ := path.Match(clean, path.Base(rel)); ok {
			return true
		}
	}
	return false
}

func (t osTree) abs(rel string) (string, error) {
	clean := path.Clean("/" + rel)
	abs := filepath.Join(t.root, filepath.FromSlash(clean))
	if !withinRoot(t.root, abs) {
		return "", fmt.Errorf("path %q escapes source root", rel)
	}
	return abs, nil
}

func (t osTree) ReadFile(rel string) ([]byte, error) {
	abs, err := t.abs(rel)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(abs)
}

func (t osTree) List(subpath string) ([]string, error) {
	base, err := t.abs(subpath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(base)
	if err != nil {
		return nil, err
	}
	var out []string
	if !info.IsDir() {
		rel, _ := filepath.Rel(t.root, base)
		return []string{filepath.ToSlash(rel)}, nil
	}
	err = filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(t.root, p)
		if rerr != nil {
			return rerr
		}
		relSlash := filepath.ToSlash(rel)
		if d.IsDir() {
			// Skip VCS/derived dirs and blueprint-declared ignores so a
			// git/dir snapshot doesn't drag them in.
			if relSlash != "." && t.ignored(relSlash) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("refusing to read symlink %q from source (design §9)", p)
		}
		if t.ignored(relSlash) {
			return nil
		}
		out = append(out, relSlash)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// withinRoot reports whether abs is inside root (after cleaning). Guards path
// containment for both source reads and target writes (design §9).
func withinRoot(root, abs string) bool {
	root = filepath.Clean(root)
	abs = filepath.Clean(abs)
	if abs == root {
		return true
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
