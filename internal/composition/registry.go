package composition

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/sourceplane/orun/internal/model"
	"gopkg.in/yaml.v3"
)

const (
	compositionKind        = "Composition"
	compositionPackageKind = "CompositionPackage"
	stackKind              = "Stack"
	lockAPIVersion         = "sourceplane.io/v1alpha1"
	lockKind               = "CompositionLock"
	legacySourceName       = "legacy-config-dir"
	legacySourceKind       = "legacy-dir"
	lockFilePath           = ".orun/compositions.lock.yaml"
)

// Composition is the resolved internal representation used by planning and validation.
type Composition struct {
	Key             string
	Name            string
	Description     string
	DefaultJobName  string
	InputSchema     map[string]interface{}
	Jobs            []model.JobSpec
	JobMap          map[string]*model.JobSpec
	Schema          *jsonschema.Schema
	JobRegistryName string
	JobRegistryDesc string
	SourceName      string
	SourceKind      string
	SourceRef       string
	SourcePath      string
	ExportPath      string
	ResolvedDigest  string
}

// Registry holds resolved compositions and the sources they came from.
type Registry struct {
	Types    map[string]*Composition
	ByKey    map[string]*Composition
	Jobs     *model.JobRegistry
	Bindings map[string]*model.JobBinding
	Sources  []model.ResolvedCompositionSource
}

type sourcePackage struct {
	declared         model.CompositionSource
	manifest         model.CompositionPackage
	resolvedRoot     string
	resolvedMetadata model.ResolvedCompositionSource
	compositions     map[string]*Composition
	index            int
}

// LoadRegistry resolves declared composition sources from intent and falls back to the legacy config dir when needed.
func LoadRegistry(intent *model.Intent, intentPath, legacyConfigDir string) (*Registry, error) {
	if intent == nil {
		return nil, fmt.Errorf("intent cannot be nil")
	}

	baseDir := filepath.Dir(intentPath)
	if baseDir == "" {
		baseDir = "."
	}

	var legacyRegistry *Registry
	var err error
	if strings.TrimSpace(legacyConfigDir) != "" {
		legacyRegistry, err = LoadFromDir(legacyConfigDir)
		if err != nil {
			return nil, err
		}
	}

	if len(intent.Compositions.Sources) == 0 {
		if legacyRegistry != nil {
			annotateIntentWithRegistry(intent, legacyRegistry)
			return legacyRegistry, nil
		}
		return nil, fmt.Errorf("intent does not declare compositions and no legacy --config-dir fallback was provided")
	}

	packages, err := resolveDeclaredSources(baseDir, intent.Compositions)
	if err != nil {
		return nil, err
	}

	selectedTypes, err := selectDefaultCompositions(packages, intent.Compositions.Resolution)
	if err != nil {
		return nil, err
	}

	registry := newRegistry()
	for _, sourcePackage := range packages {
		registry.Sources = append(registry.Sources, sourcePackage.resolvedMetadata)
	}

	for typeName, composition := range selectedTypes {
		registry.Types[typeName] = composition
		registry.ByKey[composition.Key] = composition
		registry.Jobs.Jobs = append(registry.Jobs.Jobs, composition.Jobs...)
	}

	if legacyRegistry != nil {
		registry.Sources = append(registry.Sources, legacyRegistry.Sources...)
	}

	if err := annotateIntentWithResolvedCompositions(intent, registry, packages, legacyRegistry); err != nil {
		return nil, err
	}

	return registry, nil
}

// LoadFromDir loads legacy folder-shaped compositions from a config dir.
func LoadFromDir(configDir string) (*Registry, error) {
	isRecursive := strings.Contains(configDir, "*")

	var searchPaths []string
	if isRecursive {
		matches, err := filepath.Glob(configDir)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate glob pattern %s: %w", configDir, err)
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("glob pattern %s matched no directories", configDir)
		}
		searchPaths = matches
	} else {
		info, err := os.Stat(configDir)
		if err != nil {
			return nil, fmt.Errorf("failed to access config directory %s: %w", configDir, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("config path is not a directory: %s", configDir)
		}
		searchPaths = []string{configDir}
	}

	registry := newRegistry()

	jobFiles := make(map[string]string)
	schemaFiles := make(map[string]string)
	for _, basePath := range searchPaths {
		if isRecursive {
			err := filepath.Walk(basePath, func(path string, info os.FileInfo, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if info.IsDir() {
					return nil
				}

				filename := info.Name()
				if filename != "job.yaml" && filename != "compositions.yaml" && filename != "schema.yaml" {
					return nil
				}

				typeName := filepath.Base(filepath.Dir(path))
				if filename == "job.yaml" || filename == "compositions.yaml" {
					jobFiles[path] = typeName
				} else {
					schemaFiles[typeName] = path
				}
				return nil
			})
			if err != nil {
				return nil, fmt.Errorf("failed to walk directory %s: %w", basePath, err)
			}
			continue
		}

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
			// Prefer compositions.yaml (new filename) but accept job.yaml for backward compat.
			jobPath := filepath.Join(typeDir, "compositions.yaml")
			if _, err := os.Stat(jobPath); err != nil {
				jobPath = filepath.Join(typeDir, "job.yaml")
			}
			if _, err := os.Stat(jobPath); err == nil {
				jobFiles[jobPath] = typeName
			}
			schemaPath := filepath.Join(typeDir, "schema.yaml")
			if _, err := os.Stat(schemaPath); err == nil {
				schemaFiles[typeName] = schemaPath
			}
		}
	}

	if len(jobFiles) == 0 {
		return nil, fmt.Errorf("no compositions.yaml files found in config path: %s", configDir)
	}

	digest, err := hashDirectories(searchPaths)
	if err != nil {
		return nil, fmt.Errorf("failed to hash legacy composition source %s: %w", configDir, err)
	}

	jobPaths := make([]string, 0, len(jobFiles))
	for jobPath := range jobFiles {
		jobPaths = append(jobPaths, jobPath)
	}
	sort.Strings(jobPaths)

	resolvedExports := make([]string, 0, len(jobPaths))
	for _, jobPath := range jobPaths {
		typeName := jobFiles[jobPath]
		schemaPath, exists := schemaFiles[typeName]
		if !exists {
			return nil, fmt.Errorf("missing schema.yaml for job registry type %s (job at %s)", typeName, jobPath)
		}

		jobRegistry, err := loadLegacyJobRegistry(jobPath, typeName)
		if err != nil {
			return nil, err
		}
		schemaObj, err := loadYAMLMap(schemaPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read schema definition for type %s: %w", typeName, err)
		}
		schema, err := compileSchema(typeName, schemaObj)
		if err != nil {
			return nil, fmt.Errorf("failed to compile schema for type %s: %w", typeName, err)
		}

		composition := &Composition{
			Key:             legacyCompositionKey(typeName),
			Name:            typeName,
			DefaultJobName:  jobRegistry.Jobs[0].Name,
			InputSchema:     schemaObj,
			Jobs:            jobRegistry.Jobs,
			JobMap:          make(map[string]*model.JobSpec),
			Schema:          schema,
			JobRegistryName: jobRegistry.Metadata.Name,
			JobRegistryDesc: jobRegistry.Metadata.Description,
			SourceName:      legacySourceName,
			SourceKind:      legacySourceKind,
			SourcePath:      configDir,
			ResolvedDigest:  digest,
		}

		for i := range composition.Jobs {
			composition.JobMap[composition.Jobs[i].Name] = &composition.Jobs[i]
		}

		registry.Types[typeName] = composition
		registry.ByKey[composition.Key] = composition
		registry.Jobs.Jobs = append(registry.Jobs.Jobs, composition.Jobs...)
		resolvedExports = appendIfMissing(resolvedExports, typeName)
	}

	sort.Strings(resolvedExports)
	registry.Sources = append(registry.Sources, model.ResolvedCompositionSource{
		Name:           legacySourceName,
		Kind:           legacySourceKind,
		Path:           configDir,
		ResolvedDigest: digest,
		Exports:        resolvedExports,
	})

	return registry, nil
}

// ValidateComponentAgainstComposition validates a component against the resolved schema selected for it.
func (reg *Registry) ValidateComponentAgainstComposition(component *model.Component) error {
	composition, err := reg.resolveForComponent(component)
	if err != nil {
		return err
	}
	if composition.Schema == nil {
		return fmt.Errorf("schema not loaded for composition %s", composition.Name)
	}

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

// ValidateAllComponents validates all normalized components against their resolved schemas.
func (reg *Registry) ValidateAllComponents(normalized *model.NormalizedIntent) error {
	for _, component := range normalized.Components {
		if err := reg.ValidateComponentAgainstComposition(&component); err != nil {
			return err
		}
	}
	return nil
}

// WriteLockFile writes the resolved composition lock next to the intent.
func WriteLockFile(intentPath string, sources []model.ResolvedCompositionSource) error {
	lock := model.CompositionLock{
		APIVersion: lockAPIVersion,
		Kind:       lockKind,
		Sources:    make([]model.CompositionLockSource, 0, len(sources)),
	}

	for _, source := range sources {
		lock.Sources = append(lock.Sources, model.CompositionLockSource{
			Name:           source.Name,
			Kind:           source.Kind,
			Ref:            source.Ref,
			Path:           source.Path,
			ResolvedDigest: source.ResolvedDigest,
			Exports:        append([]string(nil), source.Exports...),
		})
	}

	data, err := yaml.Marshal(lock)
	if err != nil {
		return fmt.Errorf("failed to marshal composition lock: %w", err)
	}

	lockPath := LockFilePath(intentPath)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return fmt.Errorf("failed to create lock directory: %w", err)
	}
	if err := os.WriteFile(lockPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write composition lock: %w", err)
	}
	return nil
}

// LockFilePath returns the default lock-file path for an intent file.
func LockFilePath(intentPath string) string {
	baseDir := filepath.Dir(intentPath)
	if baseDir == "" {
		baseDir = "."
	}
	return filepath.Join(baseDir, filepath.FromSlash(lockFilePath))
}

func resolveDeclaredSources(baseDir string, config model.CompositionConfig) ([]*sourcePackage, error) {
	if err := validateSourceConfiguration(config); err != nil {
		return nil, err
	}

	packages := make([]*sourcePackage, 0, len(config.Sources))
	for index, source := range config.Sources {
		resolved, err := resolveSourcePackage(baseDir, source, index)
		if err != nil {
			return nil, err
		}
		packages = append(packages, resolved)
	}
	return packages, nil
}

func validateSourceConfiguration(config model.CompositionConfig) error {
	seen := make(map[string]struct{}, len(config.Sources))
	for _, source := range config.Sources {
		if strings.TrimSpace(source.Name) == "" {
			return fmt.Errorf("composition source name is required")
		}
		if _, exists := seen[source.Name]; exists {
			return fmt.Errorf("duplicate composition source name: %s", source.Name)
		}
		seen[source.Name] = struct{}{}

		switch source.Kind {
		case "dir", "archive":
			if strings.TrimSpace(source.Path) == "" {
				return fmt.Errorf("composition source %s kind=%s requires path", source.Name, source.Kind)
			}
		case "oci":
			if strings.TrimSpace(source.Ref) == "" {
				return fmt.Errorf("composition source %s kind=oci requires ref", source.Name)
			}
		default:
			return fmt.Errorf("unsupported composition source kind %q for source %s", source.Kind, source.Name)
		}
	}

	for _, sourceName := range config.Resolution.Precedence {
		if _, exists := seen[sourceName]; !exists {
			return fmt.Errorf("composition resolution precedence references unknown source %s", sourceName)
		}
	}
	for compositionType, sourceName := range config.Resolution.Bindings {
		if _, exists := seen[sourceName]; !exists {
			return fmt.Errorf("composition binding %s references unknown source %s", compositionType, sourceName)
		}
	}

	return nil
}

func selectDefaultCompositions(packages []*sourcePackage, resolution model.CompositionResolution) (map[string]*Composition, error) {
	exportsByType := make(map[string][]*Composition)
	for _, sourcePackage := range packages {
		for compositionType, composition := range sourcePackage.compositions {
			exportsByType[compositionType] = append(exportsByType[compositionType], composition)
		}
	}

	orderedSources, err := resolutionOrder(packages, resolution.Precedence)
	if err != nil {
		return nil, err
	}

	rankBySource := make(map[string]int, len(orderedSources))
	for index, sourcePackage := range orderedSources {
		rankBySource[sourcePackage.declared.Name] = index
	}

	selected := make(map[string]*Composition, len(exportsByType))
	for compositionType, candidates := range exportsByType {
		bindingSource, hasBinding := resolution.Bindings[compositionType]
		if hasBinding {
			chosen := compositionFromSource(candidates, bindingSource)
			if chosen == nil {
				return nil, fmt.Errorf("composition binding %s=%s does not match an exported composition", compositionType, bindingSource)
			}
			selected[compositionType] = chosen
			continue
		}

		if len(candidates) == 1 {
			selected[compositionType] = candidates[0]
			continue
		}

		if len(resolution.Precedence) == 0 {
			sourceNames := make([]string, 0, len(candidates))
			for _, candidate := range candidates {
				sourceNames = append(sourceNames, candidate.SourceName)
			}
			sort.Strings(sourceNames)
			return nil, fmt.Errorf("composition type %s is exported by multiple sources (%s); declare resolution.precedence or a binding", compositionType, strings.Join(sourceNames, ", "))
		}

		sort.SliceStable(candidates, func(i, j int) bool {
			return rankBySource[candidates[i].SourceName] < rankBySource[candidates[j].SourceName]
		})
		selected[compositionType] = candidates[0]
	}

	return selected, nil
}

func resolutionOrder(packages []*sourcePackage, precedence []string) ([]*sourcePackage, error) {
	byName := make(map[string]*sourcePackage, len(packages))
	for _, sourcePackage := range packages {
		byName[sourcePackage.declared.Name] = sourcePackage
	}

	ordered := make([]*sourcePackage, 0, len(packages))
	seen := make(map[string]struct{}, len(packages))
	for _, sourceName := range precedence {
		sourcePackage, exists := byName[sourceName]
		if !exists {
			return nil, fmt.Errorf("composition precedence references unknown source %s", sourceName)
		}
		if _, exists := seen[sourceName]; exists {
			continue
		}
		ordered = append(ordered, sourcePackage)
		seen[sourceName] = struct{}{}
	}

	for _, sourcePackage := range packages {
		if _, exists := seen[sourcePackage.declared.Name]; exists {
			continue
		}
		ordered = append(ordered, sourcePackage)
	}

	return ordered, nil
}

func annotateIntentWithResolvedCompositions(intent *model.Intent, registry *Registry, packages []*sourcePackage, legacyRegistry *Registry) error {
	packagesByName := make(map[string]*sourcePackage, len(packages))
	for _, sourcePackage := range packages {
		packagesByName[sourcePackage.declared.Name] = sourcePackage
	}

	for index := range intent.Components {
		component := &intent.Components[index]
		resolved, err := resolveComponentSelection(component, registry.Types, packagesByName, legacyRegistry)
		if err != nil {
			return err
		}
		component.ResolvedComposition = resolved.Key
		component.ResolvedCompositionSource = resolved.SourceName
		if _, exists := registry.ByKey[resolved.Key]; !exists {
			registry.ByKey[resolved.Key] = resolved
		}
		if _, exists := registry.Types[component.Type]; !exists && resolved.Name == component.Type {
			registry.Types[component.Type] = resolved
		}
	}

	return nil
}

func resolveComponentSelection(component *model.Component, selectedTypes map[string]*Composition, packagesByName map[string]*sourcePackage, legacyRegistry *Registry) (*Composition, error) {
	if component.CompositionRef != nil {
		sourceName := strings.TrimSpace(component.CompositionRef.Source)
		if sourceName == "" {
			return nil, fmt.Errorf("component %s compositionRef.source must reference a declared source", component.Name)
		}
		sourcePackage, exists := packagesByName[sourceName]
		if !exists {
			return nil, fmt.Errorf("component %s compositionRef.source %s is not declared", component.Name, sourceName)
		}

		compositionName := strings.TrimSpace(component.CompositionRef.Name)
		if compositionName == "" {
			compositionName = component.Type
		}
		resolved, exists := sourcePackage.compositions[compositionName]
		if !exists {
			return nil, fmt.Errorf("component %s requested composition %s from source %s, but that export was not found", component.Name, compositionName, sourceName)
		}
		return resolved, nil
	}

	if resolved, exists := selectedTypes[component.Type]; exists {
		return resolved, nil
	}
	if legacyRegistry != nil {
		if resolved, exists := legacyRegistry.Types[component.Type]; exists {
			return resolved, nil
		}
	}

	return nil, fmt.Errorf("component type not found: %s", component.Type)
}

func annotateIntentWithRegistry(intent *model.Intent, registry *Registry) {
	for index := range intent.Components {
		component := &intent.Components[index]
		if resolved, exists := registry.Types[component.Type]; exists {
			component.ResolvedComposition = resolved.Key
			component.ResolvedCompositionSource = resolved.SourceName
		}
	}
}

func (reg *Registry) resolveForComponent(component *model.Component) (*Composition, error) {
	if component.ResolvedComposition != "" {
		if resolved, exists := reg.ByKey[component.ResolvedComposition]; exists {
			return resolved, nil
		}
	}
	if resolved, exists := reg.Types[component.Type]; exists {
		return resolved, nil
	}
	return nil, fmt.Errorf("component type not found: %s", component.Type)
}

func resolveSourcePackage(baseDir string, source model.CompositionSource, index int) (*sourcePackage, error) {
	switch source.Kind {
	case "dir":
		return resolveDirectorySource(baseDir, source, index)
	case "archive":
		return resolveArchiveSource(baseDir, source, index)
	case "oci":
		return resolveOCISource(source, index)
	default:
		return nil, fmt.Errorf("unsupported composition source kind %q", source.Kind)
	}
}

func resolveDirectorySource(baseDir string, source model.CompositionSource, index int) (*sourcePackage, error) {
	absPath := resolveRelativePath(baseDir, source.Path)
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to access composition source %s: %w", source.Name, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("composition source %s path is not a directory: %s", source.Name, source.Path)
	}

	digest, err := hashDirectory(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to hash composition source %s: %w", source.Name, err)
	}
	if source.Digest != "" && source.Digest != digest {
		return nil, fmt.Errorf("composition source %s digest mismatch: expected %s, got %s", source.Name, source.Digest, digest)
	}

	cacheDir, err := ensureCachedDirectory(absPath, digest)
	if err != nil {
		return nil, fmt.Errorf("failed to cache composition source %s: %w", source.Name, err)
	}
	return loadPackageSource(cacheDir, source, digest, index)
}

func resolveArchiveSource(baseDir string, source model.CompositionSource, index int) (*sourcePackage, error) {
	absPath := resolveRelativePath(baseDir, source.Path)
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to access composition archive %s: %w", source.Name, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("composition source %s archive path is a directory: %s", source.Name, source.Path)
	}

	digest, err := hashFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to hash composition archive %s: %w", source.Name, err)
	}
	if source.Digest != "" && source.Digest != digest {
		return nil, fmt.Errorf("composition source %s digest mismatch: expected %s, got %s", source.Name, source.Digest, digest)
	}

	cacheDir, err := ensureCachedArchive(absPath, digest)
	if err != nil {
		return nil, fmt.Errorf("failed to extract composition archive %s: %w", source.Name, err)
	}
	return loadPackageSource(cacheDir, source, digest, index)
}

func resolveOCISource(source model.CompositionSource, index int) (*sourcePackage, error) {
	remoteRef := normalizeOCIRef(source.Ref)
	if remoteRef == "" {
		return nil, fmt.Errorf("composition source %s ref cannot be empty", source.Name)
	}

	resolvedDigest, err := resolveOCIDigest(remoteRef)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve oci composition source %s: %w", source.Name, err)
	}
	if source.Digest != "" && source.Digest != resolvedDigest {
		return nil, fmt.Errorf("composition source %s digest mismatch: expected %s, got %s", source.Name, source.Digest, resolvedDigest)
	}

	cacheDir, err := ensureCachedOCI(remoteRef, resolvedDigest)
	if err != nil {
		return nil, fmt.Errorf("failed to pull oci composition source %s: %w", source.Name, err)
	}
	return loadPackageSource(cacheDir, source, resolvedDigest, index)
}

func loadPackageSource(rootDir string, source model.CompositionSource, digest string, index int) (*sourcePackage, error) {
	manifest, err := loadManifestFromRoot(rootDir, source.Name)
	if err != nil {
		return nil, err
	}
	if err := validatePackageManifest(source.Name, *manifest); err != nil {
		return nil, err
	}

	resolved := &sourcePackage{
		declared:     source,
		manifest:     *manifest,
		resolvedRoot: rootDir,
		compositions: make(map[string]*Composition),
		resolvedMetadata: model.ResolvedCompositionSource{
			Name:           source.Name,
			Kind:           source.Kind,
			Path:           source.Path,
			Ref:            source.Ref,
			ResolvedDigest: digest,
			Exports:        make([]string, 0, len(manifest.Spec.Exports)),
		},
		index: index,
	}

	for _, export := range manifest.Spec.Exports {
		composition, err := loadExportedComposition(rootDir, source, *manifest, export, digest)
		if err != nil {
			return nil, err
		}
		resolved.compositions[export.Composition] = composition
		resolved.resolvedMetadata.Exports = append(resolved.resolvedMetadata.Exports, export.Composition)
	}

	sort.Strings(resolved.resolvedMetadata.Exports)
	return resolved, nil
}

// loadManifestFromRoot reads stack.yaml (preferred) or orun.yaml from rootDir,
// converting a Stack manifest to the internal CompositionPackage representation.
func loadManifestFromRoot(rootDir, sourceName string) (*model.CompositionPackage, error) {
	if data, err := os.ReadFile(filepath.Join(rootDir, "stack.yaml")); err == nil {
		pkg, convErr := stackYAMLToCompositionPackage(data, rootDir)
		if convErr != nil {
			return nil, fmt.Errorf("failed to parse stack.yaml for source %s: %w", sourceName, convErr)
		}
		return pkg, nil
	}
	data, err := os.ReadFile(filepath.Join(rootDir, "orun.yaml"))
	if err != nil {
		return nil, fmt.Errorf("failed to read package manifest for source %s: %w", sourceName, err)
	}
	var manifest model.CompositionPackage
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse package manifest for source %s: %w", sourceName, err)
	}
	return &manifest, nil
}

func validatePackageManifest(sourceName string, manifest model.CompositionPackage) error {
	if manifest.Kind != compositionPackageKind {
		return fmt.Errorf("composition source %s manifest must have kind %s", sourceName, compositionPackageKind)
	}
	if strings.TrimSpace(manifest.Metadata.Name) == "" {
		return fmt.Errorf("composition source %s manifest must set metadata.name", sourceName)
	}
	if len(manifest.Spec.Exports) == 0 {
		return fmt.Errorf("composition source %s must export at least one composition", sourceName)
	}

	seen := make(map[string]struct{}, len(manifest.Spec.Exports))
	for _, export := range manifest.Spec.Exports {
		if strings.TrimSpace(export.Composition) == "" {
			return fmt.Errorf("composition source %s contains an export without a composition name", sourceName)
		}
		if strings.TrimSpace(export.Path) == "" {
			return fmt.Errorf("composition source %s export %s is missing a path", sourceName, export.Composition)
		}
		if _, exists := seen[export.Composition]; exists {
			return fmt.Errorf("composition source %s exports duplicate composition %s", sourceName, export.Composition)
		}
		seen[export.Composition] = struct{}{}
	}

	return nil
}

func loadExportedComposition(rootDir string, source model.CompositionSource, manifest model.CompositionPackage, export model.CompositionExport, digest string) (*Composition, error) {
	compositionPath := filepath.Join(rootDir, filepath.FromSlash(export.Path))
	data, err := os.ReadFile(compositionPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read composition %s from source %s: %w", export.Composition, source.Name, err)
	}

	var document model.CompositionDocument
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("failed to parse composition %s from source %s: %w", export.Composition, source.Name, err)
	}
	if err := validateCompositionDocument(source.Name, export.Composition, document); err != nil {
		return nil, err
	}

	schema, err := compileSchema(document.Spec.Type, document.Spec.InputSchema)
	if err != nil {
		return nil, fmt.Errorf("failed to compile input schema for composition %s from source %s: %w", export.Composition, source.Name, err)
	}

	composition := &Composition{
		Key:             compositionKey(source.Name, export.Composition),
		Name:            export.Composition,
		Description:     document.Spec.Description,
		DefaultJobName:  document.Spec.DefaultJob,
		InputSchema:     document.Spec.InputSchema,
		Jobs:            document.Spec.Jobs,
		JobMap:          make(map[string]*model.JobSpec),
		Schema:          schema,
		JobRegistryName: manifest.Metadata.Name,
		JobRegistryDesc: manifest.Metadata.Description,
		SourceName:      source.Name,
		SourceKind:      source.Kind,
		SourceRef:       source.Ref,
		SourcePath:      source.Path,
		ExportPath:      export.Path,
		ResolvedDigest:  digest,
	}

	for i := range composition.Jobs {
		composition.JobMap[composition.Jobs[i].Name] = &composition.Jobs[i]
	}

	return composition, nil
}

func validateCompositionDocument(sourceName, exportName string, document model.CompositionDocument) error {
	if document.Kind != compositionKind {
		return fmt.Errorf("composition source %s export %s must have kind %s", sourceName, exportName, compositionKind)
	}
	if strings.TrimSpace(document.Metadata.Name) == "" {
		return fmt.Errorf("composition source %s export %s must set metadata.name", sourceName, exportName)
	}
	if document.Metadata.Name != exportName {
		return fmt.Errorf("composition source %s export %s metadata.name must match the export name", sourceName, exportName)
	}
	if strings.TrimSpace(document.Spec.Type) == "" {
		return fmt.Errorf("composition source %s export %s must set spec.type", sourceName, exportName)
	}
	if document.Spec.Type != document.Metadata.Name {
		return fmt.Errorf("composition source %s export %s must keep spec.type equal to metadata.name", sourceName, exportName)
	}
	if len(document.Spec.InputSchema) == 0 {
		return fmt.Errorf("composition source %s export %s must define spec.inputSchema", sourceName, exportName)
	}
	if len(document.Spec.Jobs) == 0 {
		return fmt.Errorf("composition source %s export %s must define at least one job", sourceName, exportName)
	}
	if strings.TrimSpace(document.Spec.DefaultJob) == "" {
		return fmt.Errorf("composition source %s export %s must define spec.defaultJob", sourceName, exportName)
	}

	defaultJobExists := false
	for _, job := range document.Spec.Jobs {
		if job.Name == document.Spec.DefaultJob {
			defaultJobExists = true
			break
		}
	}
	if !defaultJobExists {
		return fmt.Errorf("composition source %s export %s defaultJob %s is not present in spec.jobs", sourceName, exportName, document.Spec.DefaultJob)
	}

	return nil
}

func compileSchema(typeName string, schemaObj map[string]interface{}) (*jsonschema.Schema, error) {
	jsonData, err := json.Marshal(schemaObj)
	if err != nil {
		return nil, err
	}

	schemaURI := fmt.Sprintf("profiles://%s/schema.json", typeName)
	compiler := jsonschema.NewCompiler()
	compiler.LoadURL = func(url string) (io.ReadCloser, error) {
		if url == schemaURI {
			return io.NopCloser(bytes.NewReader(jsonData)), nil
		}
		return nil, fmt.Errorf("external schema reference not supported: %s", url)
	}

	return compiler.Compile(schemaURI)
}

func loadYAMLMap(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := yaml.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func loadLegacyJobRegistry(path, typeName string) (*model.JobRegistry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read job definition for type %s: %w", typeName, err)
	}

	var registry model.JobRegistry
	if err := yaml.Unmarshal(data, &registry); err != nil {
		return nil, fmt.Errorf("failed to parse job registry definition for type %s: %w", typeName, err)
	}
	if len(registry.Jobs) == 0 {
		return nil, fmt.Errorf("no jobs defined in job registry for type %s", typeName)
	}
	return &registry, nil
}

func compositionFromSource(candidates []*Composition, sourceName string) *Composition {
	for _, candidate := range candidates {
		if candidate.SourceName == sourceName {
			return candidate
		}
	}
	return nil
}

func compositionKey(sourceName, compositionName string) string {
	return sourceName + ":" + compositionName
}

func legacyCompositionKey(compositionName string) string {
	return compositionKey(legacySourceName, compositionName)
}

func newRegistry() *Registry {
	return &Registry{
		Types: make(map[string]*Composition),
		ByKey: make(map[string]*Composition),
		Jobs: &model.JobRegistry{
			APIVersion: "sourceplane.io/v1",
			Kind:       "JobRegistry",
			Jobs:       []model.JobSpec{},
		},
		Bindings: make(map[string]*model.JobBinding),
		Sources:  make([]model.ResolvedCompositionSource, 0),
	}
}

func appendIfMissing(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func resolveRelativePath(baseDir, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(baseDir, path))
}

func cacheRoot() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to determine user home directory: %w", err)
	}
	root := filepath.Join(homeDir, ".orun", "cache", "compositions")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	return root, nil
}

func ensureCachedDirectory(srcDir, digest string) (string, error) {
	root, err := cacheRoot()
	if err != nil {
		return "", err
	}
	cacheDir := filepath.Join(root, strings.TrimPrefix(digest, "sha256:"))
	if _, err := os.Stat(filepath.Join(cacheDir, "stack.yaml")); err == nil {
		return cacheDir, nil
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "orun.yaml")); err == nil {
		return cacheDir, nil
	}

	tempDir, err := os.MkdirTemp(root, filepath.Base(cacheDir)+"-tmp-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tempDir)

	if err := copyDir(srcDir, tempDir); err != nil {
		return "", err
	}
	if err := os.RemoveAll(cacheDir); err != nil {
		return "", err
	}
	if err := os.Rename(tempDir, cacheDir); err != nil {
		return "", err
	}
	return cacheDir, nil
}

func ensureCachedArchive(archivePath, digest string) (string, error) {
	root, err := cacheRoot()
	if err != nil {
		return "", err
	}
	cacheDir := filepath.Join(root, strings.TrimPrefix(digest, "sha256:"))
	if _, err := os.Stat(filepath.Join(cacheDir, "stack.yaml")); err == nil {
		return cacheDir, nil
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "orun.yaml")); err == nil {
		return cacheDir, nil
	}

	tempDir, err := os.MkdirTemp(root, filepath.Base(cacheDir)+"-tmp-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tempDir)

	if err := extractTarGz(archivePath, tempDir); err != nil {
		return "", err
	}
	if err := os.RemoveAll(cacheDir); err != nil {
		return "", err
	}
	if err := os.Rename(tempDir, cacheDir); err != nil {
		return "", err
	}
	return cacheDir, nil
}

func ensureCachedOCI(remoteRef, digest string) (string, error) {
	root, err := cacheRoot()
	if err != nil {
		return "", err
	}
	cacheDir := filepath.Join(root, strings.TrimPrefix(digest, "sha256:"))
	// Check for either manifest filename as a cache-hit indicator.
	if _, err := os.Stat(filepath.Join(cacheDir, "stack.yaml")); err == nil {
		return cacheDir, nil
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "orun.yaml")); err == nil {
		return cacheDir, nil
	}

	if _, err := exec.LookPath("oras"); err != nil {
		return "", fmt.Errorf("oras is required to pull OCI composition sources")
	}

	tempDir, err := os.MkdirTemp(root, filepath.Base(cacheDir)+"-tmp-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tempDir)

	outputDir := tempDir + string(filepath.Separator)

	// If the remote is a stack artifact, pull only the compositions layer to avoid
	// downloading the (potentially large) examples layer.
	isStack, checkErr := ociArtifactIsStack(remoteRef)
	var cmd *exec.Cmd
	if checkErr == nil && isStack {
		cmd = exec.Command("oras", "pull", remoteRef, "-o", outputDir,
			"--media-type", compositionsLayerMediaType)
	} else {
		cmd = exec.Command("oras", "pull", remoteRef, "-o", outputDir)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("oras pull failed: %w\n%s", err, strings.TrimSpace(string(output)))
	}

	if err := os.RemoveAll(cacheDir); err != nil {
		return "", err
	}
	if err := os.Rename(tempDir, cacheDir); err != nil {
		return "", err
	}
	return cacheDir, nil
}

// ociArtifactIsStack fetches the OCI manifest and returns true when at least one layer
// carries the stack compositions media type, meaning selective layer pull is safe.
func ociArtifactIsStack(remoteRef string) (bool, error) {
	cmd := exec.Command("oras", "manifest", "fetch", remoteRef, "--format", "json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("oras manifest fetch failed: %w", err)
	}
	return strings.Contains(string(output), compositionsLayerMediaType), nil
}

func resolveOCIDigest(remoteRef string) (string, error) {
	if _, err := exec.LookPath("oras"); err != nil {
		return "", fmt.Errorf("oras is required to resolve OCI composition sources")
	}

	cmd := exec.Command("oras", "manifest", "fetch", remoteRef, "--format", "json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("oras manifest fetch failed: %w\n%s", err, strings.TrimSpace(string(output)))
	}

	var manifest struct {
		Digest string `json:"digest"`
	}
	if err := json.Unmarshal(output, &manifest); err != nil {
		return "", fmt.Errorf("failed to parse oras manifest output: %w", err)
	}
	if strings.TrimSpace(manifest.Digest) == "" {
		return "", fmt.Errorf("oras manifest output did not include a digest")
	}

	return manifest.Digest, nil
}
