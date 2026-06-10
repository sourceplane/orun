package objplan

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// compositionLockPath is the workspace-relative location of the resolved
// composition lock (mirrors internal/composition.lockFilePath).
const compositionLockPath = ".orun/compositions.lock.yaml"

// CompositionMeta is the resolved identity of a composition source that backs a
// component type (orun-service-catalog SC7). It comes from the composition lock
// — the same reproducible-planning record the plan engine resolves against.
type CompositionMeta struct {
	Name       string
	Digest     string
	SourceKind string
	SourceRef  string
	SourcePath string
}

// CompositionResolver maps a component `type` to the composition that exports
// it, or nil when no composition backs the type.
type CompositionResolver func(componentType string) *CompositionMeta

// lockFile is the minimal shape of compositions.lock.yaml this package reads.
type lockFile struct {
	Sources []struct {
		Name           string   `yaml:"name"`
		Kind           string   `yaml:"kind"`
		Ref            string   `yaml:"ref"`
		Path           string   `yaml:"path"`
		ResolvedDigest string   `yaml:"resolvedDigest"`
		Exports        []string `yaml:"exports"`
	} `yaml:"sources"`
}

// CompositionResolverForWorkspace reads the workspace's composition lock (if
// any) and returns a resolver over it, or nil when no lock exists. Like the
// CODEOWNERS resolver, every catalog-building path derives it the same way so
// the resolved composition bindings — and therefore the catalog content id —
// are path-independent.
func CompositionResolverForWorkspace(root string) CompositionResolver {
	if root == "" {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(compositionLockPath)))
	if err != nil {
		return nil
	}
	var lf lockFile
	if yaml.Unmarshal(b, &lf) != nil {
		return nil
	}
	byType := map[string]*CompositionMeta{}
	for i := range lf.Sources {
		s := lf.Sources[i]
		meta := &CompositionMeta{
			Name:       s.Name,
			Digest:     s.ResolvedDigest,
			SourceKind: s.Kind,
			SourceRef:  s.Ref,
			SourcePath: s.Path,
		}
		for _, t := range s.Exports {
			if _, ok := byType[t]; !ok { // first source exporting a type wins
				byType[t] = meta
			}
		}
	}
	if len(byType) == 0 {
		return nil
	}
	return func(t string) *CompositionMeta { return byType[t] }
}
