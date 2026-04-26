package discovery

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// FindComponentFile walks from startDir upward looking for component.yaml (or component.yml).
// It stops at the git repository root or filesystem root.
// Returns the component name (from metadata.name) and absolute path to the component file.
// Returns empty strings with nil error when no component.yaml is found (not an error condition).
func FindComponentFile(startDir string) (componentName string, componentFilePath string, err error) {
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
		for _, name := range []string{"component.yaml", "component.yml"} {
			candidate := filepath.Join(dir, name)
			info, statErr := os.Stat(candidate)
			if statErr != nil || info.IsDir() {
				continue
			}
			cName, readErr := readComponentName(candidate)
			if readErr != nil || cName == "" {
				continue
			}
			return cName, candidate, nil
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

	return "", "", nil
}

// readComponentName reads the metadata.name field from a component.yaml file.
// Uses minimal YAML parsing with just the fields we need.
func readComponentName(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	// Minimal struct to decode only the metadata.name field.
	type meta struct {
		Name string `yaml:"name"`
	}
	type manifest struct {
		Metadata meta `yaml:"metadata"`
	}

	// Use line-by-line parsing to avoid importing yaml here, or use encoding/json indirection.
	// Instead, use simple string scanning for the metadata.name pattern.
	// This avoids adding a yaml dependency to the discovery package.
	name := extractMetadataName(string(data))
	return name, nil
}

// extractMetadataName extracts `metadata.name` from a YAML document via simple line scanning.
// Handles the common case: a top-level "metadata:" key followed by "  name: <value>".
func extractMetadataName(content string) string {
	inMetadata := false
	for _, line := range strings.Split(content, "\n") {
		stripped := strings.TrimRight(line, "\r")
		if stripped == "metadata:" {
			inMetadata = true
			continue
		}
		if inMetadata {
			// Any non-indented line other than "metadata:" ends the block.
			if len(stripped) > 0 && stripped[0] != ' ' && stripped[0] != '\t' {
				inMetadata = false
				continue
			}
			trimmed := strings.TrimSpace(stripped)
			if strings.HasPrefix(trimmed, "name:") {
				val := strings.TrimSpace(strings.TrimPrefix(trimmed, "name:"))
				val = strings.Trim(val, `"'`)
				if val != "" {
					return val
				}
			}
		}
	}
	return ""
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
