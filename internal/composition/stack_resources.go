package composition

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/model"
	"gopkg.in/yaml.v3"
)

// loadStackProfiles loads execution profile documents from a stack directory.
// If spec.Profiles is non-empty, loads from explicit paths. Otherwise auto-discovers profiles/*.yaml.
func loadStackProfiles(rootDir string, stack model.Stack) (map[string]model.ExecutionProfile, error) {
	profiles := make(map[string]model.ExecutionProfile)

	var entries []model.StackExportEntry
	if len(stack.Spec.Profiles) > 0 {
		entries = stack.Spec.Profiles
	} else {
		discovered, err := discoverYAMLFiles(filepath.Join(rootDir, "profiles"))
		if err != nil {
			return profiles, nil
		}
		entries = discovered
	}

	for _, entry := range entries {
		path := filepath.Join(rootDir, filepath.FromSlash(entry.Path))
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read profile %s: %w", entry.Path, err)
		}

		var doc model.ExecutionProfileDocument
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("failed to parse profile %s: %w", entry.Path, err)
		}

		if doc.Kind != "" && doc.Kind != "ExecutionProfile" {
			return nil, fmt.Errorf("profile %s has unexpected kind %q (expected ExecutionProfile)", entry.Path, doc.Kind)
		}

		name := doc.Metadata.Name
		if name == "" {
			name = entry.Name
		}
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(entry.Path), filepath.Ext(entry.Path))
		}

		profiles[name] = model.ExecutionProfile{
			Description:    doc.Spec.Description,
			CompositionRef: doc.Spec.CompositionRef,
			Plan:           doc.Spec.Plan,
			Controls:       doc.Spec.Controls,
		}
	}

	return profiles, nil
}

// loadStackTriggers loads trigger binding documents from a stack directory.
// If spec.Triggers is non-empty, loads from explicit paths. Otherwise auto-discovers triggers/*.yaml.
func loadStackTriggers(rootDir string, stack model.Stack) ([]model.AutomationTrigger, error) {
	var triggers []model.AutomationTrigger

	var entries []model.StackExportEntry
	if len(stack.Spec.Triggers) > 0 {
		entries = stack.Spec.Triggers
	} else {
		discovered, err := discoverYAMLFiles(filepath.Join(rootDir, "triggers"))
		if err != nil {
			return nil, nil
		}
		entries = discovered
	}

	for _, entry := range entries {
		path := filepath.Join(rootDir, filepath.FromSlash(entry.Path))
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read trigger %s: %w", entry.Path, err)
		}

		var doc model.TriggerBindingDocument
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("failed to parse trigger %s: %w", entry.Path, err)
		}

		if doc.Kind != "" && doc.Kind != "TriggerBinding" {
			return nil, fmt.Errorf("trigger %s has unexpected kind %q (expected TriggerBinding)", entry.Path, doc.Kind)
		}

		name := doc.Metadata.Name
		if name == "" {
			name = entry.Name
		}
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(entry.Path), filepath.Ext(entry.Path))
		}

		triggers = append(triggers, model.AutomationTrigger{
			Name: name,
			On:   doc.Spec.On,
			Plan: model.TriggerPlan{Profile: doc.Spec.Plan.ProfileRef},
		})
	}

	return triggers, nil
}

// loadStackOverridePolicy loads a stack override policy.
// If spec.overridePolicy is set, loads from the explicit path. Otherwise
// auto-discovers by scanning policies/*.yaml for a StackOverridePolicy document.
// Returns nil if no policy is declared or discovered.
func loadStackOverridePolicy(rootDir string, stack model.Stack) (*model.StackOverridePolicySpec, error) {
	if stack.Spec.OverridePolicy != nil {
		return loadOverridePolicyFromPath(rootDir, stack.Spec.OverridePolicy.Path)
	}

	return discoverOverridePolicy(rootDir)
}

func loadOverridePolicyFromPath(rootDir, relPath string) (*model.StackOverridePolicySpec, error) {
	path := filepath.Join(rootDir, filepath.FromSlash(relPath))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read override policy %s: %w", relPath, err)
	}

	var doc model.StackOverridePolicyDocument
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse override policy %s: %w", relPath, err)
	}

	if doc.Kind != "" && doc.Kind != "StackOverridePolicy" {
		return nil, fmt.Errorf("override policy %s has unexpected kind %q (expected StackOverridePolicy)", relPath, doc.Kind)
	}

	return &doc.Spec, nil
}

// discoverOverridePolicy scans policies/*.yaml for the first StackOverridePolicy document.
func discoverOverridePolicy(rootDir string) (*model.StackOverridePolicySpec, error) {
	entries, err := discoverYAMLFiles(filepath.Join(rootDir, "policies"))
	if err != nil {
		return nil, nil
	}

	for _, entry := range entries {
		path := filepath.Join(rootDir, filepath.FromSlash(entry.Path))
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var doc model.StackOverridePolicyDocument
		if err := yaml.Unmarshal(data, &doc); err != nil {
			continue
		}

		if doc.Kind == "StackOverridePolicy" {
			return &doc.Spec, nil
		}
	}

	return nil, nil
}

// loadStackResourcesIntoRegistry loads profiles, triggers, and override policy from a stack
// source and merges them into the registry.
func loadStackResourcesIntoRegistry(registry *Registry, rootDir, sourceName string) error {
	data, err := os.ReadFile(filepath.Join(rootDir, "stack.yaml"))
	if err != nil {
		return nil
	}

	var stack model.Stack
	if err := yaml.Unmarshal(data, &stack); err != nil {
		return nil
	}

	profiles, err := loadStackProfiles(rootDir, stack)
	if err != nil {
		return fmt.Errorf("source %s: %w", sourceName, err)
	}
	for name, profile := range profiles {
		if _, exists := registry.Profiles[name]; !exists {
			registry.Profiles[name] = profile
		}
	}

	// Load composition-scoped profiles (compositions/<type>/profiles/*.yaml)
	compositionProfiles, err := loadCompositionScopedProfiles(rootDir)
	if err != nil {
		return fmt.Errorf("source %s: %w", sourceName, err)
	}
	for name, profile := range compositionProfiles {
		if _, exists := registry.Profiles[name]; !exists {
			registry.Profiles[name] = profile
		}
	}

	triggers, err := loadStackTriggers(rootDir, stack)
	if err != nil {
		return fmt.Errorf("source %s: %w", sourceName, err)
	}
	for _, t := range triggers {
		if t.Plan.Profile != "" {
			registry.Triggers = append(registry.Triggers, t)
		} else {
			registry.TriggerBindings = append(registry.TriggerBindings, t)
		}
	}

	policy, err := loadStackOverridePolicy(rootDir, stack)
	if err != nil {
		return fmt.Errorf("source %s: %w", sourceName, err)
	}
	if policy != nil && registry.OverridePolicy == nil {
		registry.OverridePolicy = policy
	}

	return nil
}

// loadCompositionScopedProfiles discovers profiles inside composition directories.
// Profiles at compositions/<type>/profiles/*.yaml are stored with fully-qualified keys: <type>.<profileName>.
func loadCompositionScopedProfiles(rootDir string) (map[string]model.ExecutionProfile, error) {
	profiles := make(map[string]model.ExecutionProfile)

	compositionsDir := filepath.Join(rootDir, "compositions")
	info, err := os.Stat(compositionsDir)
	if err != nil || !info.IsDir() {
		return profiles, nil
	}

	topEntries, err := os.ReadDir(compositionsDir)
	if err != nil {
		return profiles, nil
	}

	for _, te := range topEntries {
		if !te.IsDir() {
			continue
		}
		compositionType := te.Name()
		profilesDir := filepath.Join(compositionsDir, compositionType, "profiles")
		entries, err := discoverYAMLFiles(profilesDir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			path := filepath.Join(rootDir, filepath.FromSlash(entry.Path))
			// entry.Path is "profiles/<file>" relative to the profiles dir parent,
			// but discoverYAMLFiles returns paths relative to rootDir via the dir basename.
			// We need the actual full path.
			path = filepath.Join(profilesDir, filepath.Base(entry.Path))
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}

			var doc model.ExecutionProfileDocument
			if err := yaml.Unmarshal(data, &doc); err != nil {
				continue
			}

			if doc.Kind != "" && doc.Kind != "ExecutionProfile" {
				continue
			}

			name := doc.Metadata.Name
			if name == "" {
				name = entry.Name
			}

			compositionRef := doc.Spec.CompositionRef
			if compositionRef == "" {
				compositionRef = compositionType
			}

			fqName := compositionRef + "." + name
			profiles[fqName] = model.ExecutionProfile{
				Description:    doc.Spec.Description,
				CompositionRef: compositionRef,
				Plan:           doc.Spec.Plan,
				Controls:       doc.Spec.Controls,
			}
		}
	}

	return profiles, nil
}

// discoverYAMLFiles finds *.yaml files in a directory and returns them as export entries.
func discoverYAMLFiles(dir string) ([]model.StackExportEntry, error) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("directory not found: %s", dir)
	}

	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var entries []model.StackExportEntry
	for _, de := range dirEntries {
		if de.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(de.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		name := strings.TrimSuffix(de.Name(), ext)
		relPath := filepath.ToSlash(filepath.Join(filepath.Base(dir), de.Name()))
		entries = append(entries, model.StackExportEntry{
			Name: name,
			Path: relPath,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	return entries, nil
}
