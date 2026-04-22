package loader

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	compositionpkg "github.com/sourceplane/gluon/internal/composition"
	"github.com/sourceplane/gluon/internal/model"
	"gopkg.in/yaml.v3"
)

const (
	componentKind            = "Component"
	componentTreeCacheSource = "discovered"
	componentInlineSource    = "inline"
	componentTreeCacheFile   = ".gluon/component-tree.yaml"
)

// LoadIntent loads and parses an intent YAML file
func LoadIntent(path string) (*model.Intent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read intent file: %w", err)
	}

	var intent model.Intent
	if err := yaml.Unmarshal(data, &intent); err != nil {
		return nil, fmt.Errorf("failed to parse intent YAML: %w", err)
	}

	return &intent, nil
}

// LoadResolvedIntent loads an intent file, discovers external component manifests,
// and returns a merged intent plus the component discovery tree used to build it.
func LoadResolvedIntent(path string) (*model.Intent, *model.ComponentTree, error) {
	intent, err := LoadIntent(path)
	if err != nil {
		return nil, nil, err
	}

	baseDir := filepath.Dir(path)
	if baseDir == "" {
		baseDir = "."
	}

	roots := normalizeDiscoveryRoots(intent.Discovery.Roots)
	inlineEntries := buildInlineComponentEntries(intent.Components)

	discoveredFiles, err := discoverComponentFiles(baseDir, roots)
	if err != nil {
		return nil, nil, err
	}

	discoveredEntries, ok := loadDiscoveredComponentEntriesFromCache(path, baseDir, roots, discoveredFiles)
	if !ok {
		discoveredEntries, err = parseDiscoveredComponentEntries(baseDir, discoveredFiles)
		if err != nil {
			return nil, nil, err
		}
	}

	mergedComponents := make([]model.Component, 0, len(intent.Components)+len(discoveredEntries))
	mergedComponents = append(mergedComponents, intent.Components...)
	for _, entry := range discoveredEntries {
		mergedComponents = append(mergedComponents, entry.ToComponent())
	}
	intent.Components = mergedComponents

	tree := buildComponentTree(path, roots, inlineEntries, discoveredEntries)
	return intent, tree, nil
}

// WriteComponentTreeCache persists the resolved component tree next to the intent.
func WriteComponentTreeCache(intentPath string, tree *model.ComponentTree) error {
	if tree == nil {
		return nil
	}

	cachePath := componentTreeCacheWritePath(intentPath)
	dir := filepath.Dir(cachePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create component tree cache directory: %w", err)
	}

	tree.Metadata.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := yaml.Marshal(tree)
	if err != nil {
		return fmt.Errorf("failed to marshal component tree cache: %w", err)
	}

	if err := os.WriteFile(cachePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write component tree cache: %w", err)
	}

	return nil
}

// LoadJobRegistry loads and parses a job registry YAML file
func LoadJobRegistry(path string) (*model.JobRegistry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read job registry file: %w", err)
	}

	var registry model.JobRegistry
	if err := yaml.Unmarshal(data, &registry); err != nil {
		return nil, fmt.Errorf("failed to parse job registry YAML: %w", err)
	}

	return &registry, nil
}

func normalizeDiscoveryRoots(roots []string) []string {
	if len(roots) == 0 {
		return []string{"."}
	}

	seen := make(map[string]struct{}, len(roots))
	normalized := make([]string, 0, len(roots))
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		root = filepath.ToSlash(filepath.Clean(root))
		if _, exists := seen[root]; exists {
			continue
		}
		seen[root] = struct{}{}
		normalized = append(normalized, root)
	}

	if len(normalized) == 0 {
		return []string{"."}
	}

	sort.Strings(normalized)
	return normalized
}

func discoverComponentFiles(baseDir string, roots []string) ([]string, error) {
	seen := make(map[string]struct{})
	files := make([]string, 0)

	for _, root := range roots {
		resolvedRoot := filepath.Join(baseDir, filepath.FromSlash(root))
		info, err := os.Stat(resolvedRoot)
		if err != nil {
			return nil, fmt.Errorf("failed to access discovery root %s: %w", root, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("discovery root is not a directory: %s", root)
		}

		err = filepath.WalkDir(resolvedRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			if entry.IsDir() {
				if shouldSkipDiscoveryDir(entry.Name()) {
					return filepath.SkipDir
				}
				return nil
			}

			if entry.Name() != "component.yaml" && entry.Name() != "component.yml" {
				return nil
			}

			relPath, err := filepath.Rel(baseDir, path)
			if err != nil {
				return fmt.Errorf("failed to compute relative component path for %s: %w", path, err)
			}

			relPath = filepath.ToSlash(filepath.Clean(relPath))
			if _, exists := seen[relPath]; exists {
				return nil
			}
			seen[relPath] = struct{}{}
			files = append(files, relPath)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed to scan discovery root %s: %w", root, err)
		}
	}

	sort.Strings(files)
	return files, nil
}

func shouldSkipDiscoveryDir(name string) bool {
	switch name {
	case ".git", ".gluon", "node_modules":
		return true
	default:
		return false
	}
}

func parseDiscoveredComponentEntries(baseDir string, files []string) ([]model.ComponentTreeComponent, error) {
	entries := make([]model.ComponentTreeComponent, 0, len(files))
	for _, relPath := range files {
		entry, err := loadComponentTreeEntry(baseDir, relPath)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func loadComponentTreeEntry(baseDir, relPath string) (model.ComponentTreeComponent, error) {
	absPath := filepath.Join(baseDir, filepath.FromSlash(relPath))
	data, err := os.ReadFile(absPath)
	if err != nil {
		return model.ComponentTreeComponent{}, fmt.Errorf("failed to read component manifest %s: %w", relPath, err)
	}

	var manifest model.ComponentManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return model.ComponentTreeComponent{}, fmt.Errorf("failed to parse component manifest %s: %w", relPath, err)
	}

	if manifest.Kind != componentKind {
		return model.ComponentTreeComponent{}, fmt.Errorf("component manifest %s must have kind %s", relPath, componentKind)
	}
	if manifest.Metadata.Name == "" {
		return model.ComponentTreeComponent{}, fmt.Errorf("component manifest %s must set metadata.name", relPath)
	}

	component := manifest.Spec
	component.Name = manifest.Metadata.Name
	component.SourcePath = relPath
	if component.Path == "" {
		component.Path = defaultComponentPath(relPath)
	}

	entry := model.FromComponent(component, componentTreeCacheSource)
	info, err := os.Stat(absPath)
	if err != nil {
		return model.ComponentTreeComponent{}, fmt.Errorf("failed to stat component manifest %s: %w", relPath, err)
	}
	entry.FileSize = info.Size()
	entry.FileModTime = info.ModTime().UTC().Format(time.RFC3339Nano)
	return entry, nil
}

func defaultComponentPath(relPath string) string {
	dir := filepath.ToSlash(filepath.Dir(relPath))
	if dir == "." || dir == "" {
		return "./"
	}
	return dir
}

func buildInlineComponentEntries(components []model.Component) []model.ComponentTreeComponent {
	entries := make([]model.ComponentTreeComponent, 0, len(components))
	for _, component := range components {
		entries = append(entries, model.FromComponent(component, componentInlineSource))
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
	return entries
}

func buildComponentTree(intentPath string, roots []string, inlineEntries, discoveredEntries []model.ComponentTreeComponent) *model.ComponentTree {
	components := make([]model.ComponentTreeComponent, 0, len(inlineEntries)+len(discoveredEntries))
	components = append(components, inlineEntries...)
	components = append(components, discoveredEntries...)
	sort.SliceStable(components, func(i, j int) bool {
		if components[i].Source != components[j].Source {
			return components[i].Source < components[j].Source
		}
		if components[i].SourcePath != components[j].SourcePath {
			return components[i].SourcePath < components[j].SourcePath
		}
		return components[i].Name < components[j].Name
	})

	return &model.ComponentTree{
		APIVersion: "sourceplane.io/v1",
		Kind:       "ComponentTree",
		Metadata: model.ComponentTreeMetadata{
			Intent: normalizeCachePath(intentPath),
		},
		Discovery: model.ComponentTreeDiscovery{
			Roots: append([]string(nil), roots...),
		},
		Components: components,
	}
}

func loadDiscoveredComponentEntriesFromCache(intentPath, baseDir string, roots, files []string) ([]model.ComponentTreeComponent, bool) {
	cachePath := componentTreeCacheReadPath(intentPath)
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, false
	}

	var tree model.ComponentTree
	if err := yaml.Unmarshal(data, &tree); err != nil {
		return nil, false
	}
	if normalizeCachePath(tree.Metadata.Intent) != normalizeCachePath(intentPath) {
		return nil, false
	}

	if !sameStrings(tree.Discovery.Roots, roots) {
		return nil, false
	}

	discovered := make([]model.ComponentTreeComponent, 0)
	for _, component := range tree.Components {
		if component.Source == componentTreeCacheSource {
			discovered = append(discovered, component)
		}
	}
	sort.Slice(discovered, func(i, j int) bool {
		return discovered[i].SourcePath < discovered[j].SourcePath
	})

	if len(discovered) != len(files) {
		return nil, false
	}
	for i, file := range files {
		entry := discovered[i]
		if entry.SourcePath != file {
			return nil, false
		}
		info, err := os.Stat(filepath.Join(baseDir, filepath.FromSlash(file)))
		if err != nil {
			return nil, false
		}
		if entry.FileSize != info.Size() {
			return nil, false
		}
		if entry.FileModTime != info.ModTime().UTC().Format(time.RFC3339Nano) {
			return nil, false
		}
	}

	return discovered, true
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func componentTreeCacheReadPath(intentPath string) string {
	return componentTreeCacheWritePath(intentPath)
}

func componentTreeCacheWritePath(intentPath string) string {
	return cachePathFor(intentPath, componentTreeCacheFile)
}

func componentTreeCachePath(intentPath string) string {
	return componentTreeCacheWritePath(intentPath)
}

func cachePathFor(intentPath, cacheFile string) string {
	baseDir := filepath.Dir(intentPath)
	if baseDir == "" {
		baseDir = "."
	}
	return filepath.Join(baseDir, filepath.FromSlash(cacheFile))
}

func normalizeCachePath(path string) string {
	return filepath.ToSlash(filepath.Clean(path))
}

// LoadJSONSchema loads a JSON schema file
func LoadJSONSchema(path string) (interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read schema file: %w", err)
	}

	var schema interface{}
	if err := yaml.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("failed to parse schema: %w", err)
	}

	return schema, nil
}

// Composition holds a composition's job definitions and schema.
type Composition = compositionpkg.Composition

// CompositionRegistry holds resolved compositions.
type CompositionRegistry = compositionpkg.Registry

// LoadCompositionsFromDir loads legacy folder-shaped compositions from --config-dir.
func LoadCompositionsFromDir(configDir string) (*CompositionRegistry, error) {
	return compositionpkg.LoadFromDir(configDir)
}

// LoadCompositionsForIntent resolves declared composition sources from intent and
// falls back to legacy --config-dir when needed.
func LoadCompositionsForIntent(intent *model.Intent, intentPath, configDir string) (*CompositionRegistry, error) {
	return compositionpkg.LoadRegistry(intent, intentPath, configDir)
}

// WriteCompositionLockFile writes .gluon/compositions.lock.yaml for the resolved sources.
func WriteCompositionLockFile(intentPath string, sources []model.ResolvedCompositionSource) error {
	return compositionpkg.WriteLockFile(intentPath, sources)
}
