package sourcectx

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// osFilesystem is the default Filesystem — wraps `os` and `filepath.Walk`.
// Paths exposed to the Walk callback are repo-relative POSIX strings.
type osFilesystem struct {
	root string
}

// DefaultFilesystem returns a Filesystem rooted at root. The root is held
// for Walk and used to resolve relative paths in Stat/ReadFile.
func DefaultFilesystem(root string) Filesystem { return osFilesystem{root: root} }

// Walk implements Filesystem. It prunes obviously-non-source directories
// (`.git`, `node_modules`, `vendor` at the root) so the catalog-relevant
// scan stays cheap on real workspaces.
func (o osFilesystem) Walk(root string, fn func(relPath string, d fs.DirEntry) error) error {
	base := root
	if base == "" {
		base = o.root
	}
	return filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Compute repo-relative POSIX path.
		rel, relErr := filepath.Rel(base, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return fn("", d)
		}
		// Prune common non-source dirs that would otherwise dominate the
		// walk on a real workspace. Catalog-relevant files never live
		// under these.
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" {
				return fs.SkipDir
			}
		}
		return fn(rel, d)
	})
}

// Stat implements Filesystem. Relative paths are resolved against the root.
func (o osFilesystem) Stat(path string) (fs.FileInfo, error) {
	return os.Stat(o.absPath(path))
}

// ReadFile implements Filesystem. Relative paths are resolved against the root.
func (o osFilesystem) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(o.absPath(path))
}

func (o osFilesystem) absPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	// Convert POSIX-shaped relative paths to OS-native form before joining.
	return filepath.Join(o.root, filepath.FromSlash(strings.TrimPrefix(path, "./")))
}
