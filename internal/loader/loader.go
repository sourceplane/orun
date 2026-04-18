package loader

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/sourceplane/arx/internal/model"
	"gopkg.in/yaml.v3"
)

const (
	componentKind                = "Component"
	componentTreeCacheSource     = "discovered"
	componentInlineSource        = "inline"
	componentTreeCacheFile       = ".arx/component-tree.yaml"
	legacyComponentTreeCacheFile = ".ciz/component-tree.yaml"
	oldestComponentTreeCacheFile = ".liteci/component-tree.yaml"
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
	case ".git", ".arx", ".ciz", ".liteci", "node_modules":
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
	cachePath := componentTreeCacheWritePath(intentPath)
	if _, err := os.Stat(cachePath); err == nil {
		return cachePath
	}

	for _, legacyCacheFile := range []string{legacyComponentTreeCacheFile, oldestComponentTreeCacheFile} {
		legacyPath := cachePathFor(intentPath, legacyCacheFile)
		if _, err := os.Stat(legacyPath); err == nil {
			return legacyPath
		}
	}

	return cachePath
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

// Composition holds a composition's job definitions and schema
type Composition struct {
	Name            string
	Jobs            []model.JobSpec           // All jobs for this component type
	JobMap          map[string]*model.JobSpec // Quick lookup by job name
	Schema          *jsonschema.Schema
	Bindings        *model.JobBinding // Optional job binding declaration
	JobRegistryName string
	JobRegistryDesc string
}

// CompositionRegistry holds all loaded compositions
type CompositionRegistry struct {
	Types    map[string]*Composition
	Jobs     *model.JobRegistry           // For backward compatibility
	Bindings map[string]*model.JobBinding // Model -> JobBinding mapping
}

// LoadCompositionsFromDir loads composition jobs and schemas from a config directory path.
// Supports glob patterns for recursive search:
//   - Exact path: Non-recursive, looks for job.yaml and schema.yaml in immediate subdirectories
//   - Path with *: Recursive glob pattern (single level)
//   - Path with **: Recursive glob pattern (multiple levels)
//
// Example paths:
//   - "runtime/config/compositions" - non-recursive: looks in {charts,helm,etc}/
//   - "runtime/config/*" - recursive: looks in all subdirectories
//   - "runtime/config/**" - recursive: looks in all nested subdirectories
func LoadCompositionsFromDir(configDir string) (*CompositionRegistry, error) {
	// Check if path contains glob patterns
	isRecursive := strings.Contains(configDir, "*")

	var searchPaths []string

	if isRecursive {
		// Glob pattern provided - use filepath.Glob
		matches, err := filepath.Glob(configDir)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate glob pattern %s: %w", configDir, err)
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("glob pattern %s matched no directories", configDir)
		}
		searchPaths = matches
	} else {
		// Exact path - check if it exists
		info, err := os.Stat(configDir)
		if err != nil {
			return nil, fmt.Errorf("failed to access config directory %s: %w", configDir, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("config path is not a directory: %s", configDir)
		}
		searchPaths = []string{configDir}
	}

	registry := &CompositionRegistry{
		Types:    make(map[string]*Composition),
		Bindings: make(map[string]*model.JobBinding),
		Jobs: &model.JobRegistry{
			APIVersion: "sourceplane.io/v1",
			Kind:       "JobRegistry",
			Jobs:       []model.JobSpec{},
		},
	}

	// Maps to track job.yaml -> schema.yaml pairs
	jobFiles := make(map[string]string)    // job.yaml path -> variant type
	schemaFiles := make(map[string]string) // variant type -> schema.yaml path

	// Process each search path
	for _, basePath := range searchPaths {
		if isRecursive {
			// For recursive search, walk the directory tree
			err := filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}

				if info.IsDir() {
					return nil
				}

				filename := info.Name()
				if filename != "job.yaml" && filename != "schema.yaml" {
					return nil
				}

				// Extract variant type from immediate parent directory
				parentDir := filepath.Dir(path)
				typeName := filepath.Base(parentDir)

				if filename == "job.yaml" {
					jobFiles[path] = typeName
				} else if filename == "schema.yaml" {
					schemaFiles[typeName] = path
				}

				return nil
			})

			if err != nil {
				return nil, fmt.Errorf("failed to walk directory %s: %w", basePath, err)
			}
		} else {
			// Non-recursive: only look in direct subdirectories for job.yaml and schema.yaml
			entries, err := os.ReadDir(basePath)
			if err != nil {
				return nil, fmt.Errorf("failed to read directory %s: %w", basePath, err)
			}

			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}

				typeName := entry.Name()
				typeDir := filepath.Join(basePath, typeName)

				// Check for job.yaml in this subdirectory
				jobPath := filepath.Join(typeDir, "job.yaml")
				if _, err := os.Stat(jobPath); err == nil {
					jobFiles[jobPath] = typeName
				}

				// Check for schema.yaml in this subdirectory
				schemaPath := filepath.Join(typeDir, "schema.yaml")
				if _, err := os.Stat(schemaPath); err == nil {
					schemaFiles[typeName] = schemaPath
				}
			}
		}
	}

	if len(jobFiles) == 0 {
		return nil, fmt.Errorf("no job.yaml files found in config path: %s", configDir)
	}

	// Process each job.yaml and match with its schema.yaml in sorted order for determinism
	jobPaths := make([]string, 0, len(jobFiles))
	for p := range jobFiles {
		jobPaths = append(jobPaths, p)
	}
	sort.Strings(jobPaths)

	for _, jobPath := range jobPaths {
		typeName := jobFiles[jobPath]
		schemaPath, schemaExists := schemaFiles[typeName]
		if !schemaExists {
			return nil, fmt.Errorf("missing schema.yaml for job registry type %s (job at %s)", typeName, jobPath)
		}

		// Load job registry definition
		jobData, err := os.ReadFile(jobPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read job definition for type %s: %w", typeName, err)
		}

		var jobRegistry model.JobRegistry
		if err := yaml.Unmarshal(jobData, &jobRegistry); err != nil {
			return nil, fmt.Errorf("failed to parse job registry definition for type %s: %w", typeName, err)
		}

		if len(jobRegistry.Jobs) == 0 {
			return nil, fmt.Errorf("no jobs defined in job registry for type %s", typeName)
		}

		// Load schema definition
		schemaData, err := os.ReadFile(schemaPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read schema definition for type %s: %w", typeName, err)
		}

		// Parse YAML to interface{} (supports both YAML and JSON)
		var schemaObj interface{}
		if err := yaml.Unmarshal(schemaData, &schemaObj); err != nil {
			return nil, fmt.Errorf("failed to parse schema file for type %s: %w", typeName, err)
		}

		// Convert to JSON for schema compiler
		jsonData, err := json.Marshal(schemaObj)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal schema for type %s: %w", typeName, err)
		}

		// Compile schema with proper URI and custom LoadURL
		schemaURI := fmt.Sprintf("profiles://%s/schema.json", typeName)
		compiler := jsonschema.NewCompiler()
		compiler.LoadURL = func(url string) (io.ReadCloser, error) {
			// Return the schema we just read
			if url == schemaURI {
				return io.NopCloser(strings.NewReader(string(jsonData))), nil
			}
			// For other URLs, we'll just return an error
			return nil, fmt.Errorf("external schema reference not supported: %s", url)
		}

		schema, err := compiler.Compile(schemaURI)
		if err != nil {
			return nil, fmt.Errorf("failed to compile schema for type %s: %w", typeName, err)
		}

		// Store in registry with job map for quick lookup
		composition := &Composition{
			Name:            typeName,
			Jobs:            jobRegistry.Jobs,
			JobMap:          make(map[string]*model.JobSpec),
			Schema:          schema,
			JobRegistryName: jobRegistry.Metadata.Name,
			JobRegistryDesc: jobRegistry.Metadata.Description,
		}

		// Build job map for quick lookup by name
		for i := range jobRegistry.Jobs {
			composition.JobMap[jobRegistry.Jobs[i].Name] = &jobRegistry.Jobs[i]
		}

		registry.Types[typeName] = composition

		// Also add jobs to the registry's job list for backward compatibility
		registry.Jobs.Jobs = append(registry.Jobs.Jobs, jobRegistry.Jobs...)
	}

	if len(registry.Types) == 0 {
		return nil, fmt.Errorf("no component type jobs found in config path: %s", configDir)
	}

	return registry, nil
}

// ValidateComponentAgainstComposition validates a component against its composition schema
func (reg *CompositionRegistry) ValidateComponentAgainstComposition(component *model.Component) error {
	composition, exists := reg.Types[component.Type]
	if !exists {
		return fmt.Errorf("component type not found: %s", component.Type)
	}

	if composition.Schema == nil {
		return fmt.Errorf("schema not loaded for component type: %s", component.Type)
	}

	// Build validation object with component properties
	validationObj := map[string]interface{}{
		"name":   component.Name,
		"type":   component.Type,
		"inputs": component.Inputs,
		"domain": component.Domain,
		"labels": component.Labels,
	}

	if err := composition.Schema.Validate(validationObj); err != nil {
		return fmt.Errorf("component %s failed validation against type %s: %w", component.Name, component.Type, err)
	}

	return nil
}

// ValidateAllComponents validates all components in a normalized intent
func (reg *CompositionRegistry) ValidateAllComponents(normalized *model.NormalizedIntent) error {
	for _, comp := range normalized.Components {
		if err := reg.ValidateComponentAgainstComposition(&comp); err != nil {
			return err
		}
	}
	return nil
}
