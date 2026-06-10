package objplan

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/sourceplane/orun/internal/model"
	"gopkg.in/yaml.v3"
)

// compositionLockPath is the workspace-relative location of the resolved
// composition lock (mirrors internal/composition.lockFilePath).
const compositionLockPath = ".orun/compositions.lock.yaml"

// CompositionMeta is the resolved identity of a composition source that backs a
// component type (orun-service-catalog SC7). It comes from the composition lock
// — the same reproducible-planning record the plan engine resolves against. The
// optional Effects (SC8) are read from the composition manifest.
type CompositionMeta struct {
	Name       string
	Digest     string
	SourceKind string
	SourceRef  string
	SourcePath string
	Effects    *model.CompositionEffects
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
		// SC8: for a local (dir) source, read the composition manifests under the
		// source path to pick up per-type effects declarations. Best-effort — a
		// missing/undecodable manifest simply yields no effects.
		effectsByType := map[string]*model.CompositionEffects{}
		if s.Kind == "dir" && s.Path != "" {
			effectsByType = readCompositionEffects(filepath.Join(root, filepath.FromSlash(s.Path)))
		}
		for _, t := range s.Exports {
			if _, ok := byType[t]; ok { // first source exporting a type wins
				continue
			}
			m := *meta // per-type copy so effects don't leak across types
			m.Effects = effectsByType[t]
			byType[t] = &m
		}
	}
	if len(byType) == 0 {
		return nil
	}
	return func(t string) *CompositionMeta { return byType[t] }
}

// readCompositionEffects walks a composition source directory for composition
// manifests (kind: Composition) and returns their per-type effects. Bounded and
// tolerant: unreadable/unparseable files are skipped.
func readCompositionEffects(dir string) map[string]*model.CompositionEffects {
	out := map[string]*model.CompositionEffects{}
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if name != "composition.yaml" && name != "composition.yml" && !strings.HasPrefix(name, "compositions.") {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		var doc model.CompositionDocument
		if yaml.Unmarshal(b, &doc) != nil || doc.Kind != "Composition" {
			return nil
		}
		if doc.Spec.Type != "" && doc.Spec.Effects != nil {
			out[doc.Spec.Type] = doc.Spec.Effects
		}
		return nil
	})
	return out
}
