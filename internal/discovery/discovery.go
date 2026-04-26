package discovery

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sourceplane/gluon/internal/model"
)

// FindIntentFile walks from startDir upward looking for intent.yaml (or intent.yml).
// It stops at the git repository root or filesystem root.
// Returns the absolute path to the intent file and its containing directory.
func FindIntentFile(startDir string) (intentPath string, intentDir string, err error) {
	absStart, err := filepath.Abs(startDir)
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve absolute path: %w", err)
	}
	if resolved, evalErr := filepath.EvalSymlinks(absStart); evalErr == nil {
		absStart = resolved
	}

	ceiling := gitRootDir(absStart)

	dir := absStart
	for {
		for _, name := range []string{"intent.yaml", "intent.yml"} {
			candidate := filepath.Join(dir, name)
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
				return candidate, dir, nil
			}
		}

		if dir == ceiling {
			break
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", "", fmt.Errorf("no intent.yaml found between %s and %s", absStart, ceiling)
}

// DetectComponentContext determines which component the user is working in
// based on their current working directory. It compares the relative CWD path
// against known component paths and picks the longest matching prefix.
// Components with path "./" are skipped as too broad.
// Returns empty string (not an error) when no component matches.
func DetectComponentContext(cwd string, intentDir string, components []model.Component) (string, error) {
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("failed to resolve absolute cwd: %w", err)
	}
	absIntentDir, err := filepath.Abs(intentDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve absolute intent dir: %w", err)
	}

	relCwd, err := filepath.Rel(absIntentDir, absCwd)
	if err != nil {
		return "", nil
	}
	relCwd = filepath.ToSlash(relCwd)

	if strings.HasPrefix(relCwd, "..") {
		return "", nil
	}

	type candidate struct {
		name string
		path string
	}
	var candidates []candidate

	for _, comp := range components {
		p := filepath.ToSlash(filepath.Clean(comp.Path))
		if p == "" || p == "." || p == "./" {
			continue
		}
		p = strings.TrimSuffix(p, "/")

		if relCwd == p || strings.HasPrefix(relCwd, p+"/") {
			candidates = append(candidates, candidate{name: comp.Name, path: p})
		}
	}

	if len(candidates) == 0 {
		return "", nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		if len(candidates[i].path) != len(candidates[j].path) {
			return len(candidates[i].path) > len(candidates[j].path)
		}
		return candidates[i].name < candidates[j].name
	})

	return candidates[0].name, nil
}

func gitRootDir(startDir string) string {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = startDir
	out, err := cmd.Output()
	if err != nil {
		return filepath.VolumeName(startDir) + string(filepath.Separator)
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return filepath.VolumeName(startDir) + string(filepath.Separator)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return root
	}
	if resolved, evalErr := filepath.EvalSymlinks(abs); evalErr == nil {
		abs = resolved
	}
	return abs
}
