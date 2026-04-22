package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/sourceplane/gluon/internal/expand"
	"github.com/sourceplane/gluon/internal/git"
	"github.com/sourceplane/gluon/internal/loader"
	"github.com/sourceplane/gluon/internal/model"
	"github.com/sourceplane/gluon/internal/normalize"
	"github.com/sourceplane/gluon/internal/planner"
	"github.com/sourceplane/gluon/internal/render"
	"github.com/sourceplane/gluon/internal/ui"
)

func generatePlan() error {
	fmt.Println("□ Loading intent...")
	intent, _, err := loadResolvedIntentFile(intentFile)
	if err != nil {
		return fmt.Errorf("failed to load intent: %w", err)
	}

	fmt.Println("□ Loading compositions...")
	compositionRegistry, err := loader.LoadCompositionsForIntent(intent, intentFile, configDir)
	if err != nil {
		return fmt.Errorf("failed to resolve compositions: %w", err)
	}
	if err := loader.WriteCompositionLockFile(intentFile, compositionRegistry.Sources); err != nil {
		fmt.Fprintf(os.Stderr, "⚠ warning: failed to write composition lock file: %v\n", err)
	}

	// Build CompositionInfo map for the planner with default jobs
	compositionInfos := make(map[string]*planner.CompositionInfo)
	for compositionKey, composition := range compositionRegistry.ByKey {
		var defaultJob *model.JobSpec
		if composition.DefaultJobName != "" {
			defaultJob = composition.JobMap[composition.DefaultJobName]
		}
		if defaultJob == nil && len(composition.Jobs) > 0 {
			defaultJob = &composition.Jobs[0]
		}
		compositionInfos[compositionKey] = &planner.CompositionInfo{
			Type:       composition.Name,
			DefaultJob: defaultJob,
		}
	}

	fmt.Println("□ Normalizing intent...")
	normalized, err := normalize.NormalizeIntent(intent)
	if err != nil {
		return fmt.Errorf("failed to normalize intent: %w", err)
	}

	fmt.Println("□ Validating components against composition schemas...")
	if err := compositionRegistry.ValidateAllComponents(normalized); err != nil {
		return fmt.Errorf("component validation failed: %w", err)
	}

	fmt.Println("□ Expanding (env × component)...")
	expander := expand.NewExpander(normalized)
	instances, err := expander.Expand()
	if err != nil {
		return fmt.Errorf("failed to expand intent: %w", err)
	}

	// Filter instances if --changed flag is set
	if changedOnly {
		changeOptions, err := buildChangeOptions()
		if err != nil {
			return err
		}

		changedSet, err := changedFilesSet(changeOptions)
		if err != nil {
			return fmt.Errorf("failed to detect changed files: %w", err)
		}
		intentChanged := isIntentPathChanged(changedSet, intentFile)

		// Build map of changed components by checking their resolved paths
		changedComps := make(map[string]bool)
		for _, comp := range normalized.Components {
			if intentChanged {
				changedComps[comp.Name] = true
			} else if isFileChanged(changedSet, comp.SourcePath) {
				changedComps[comp.Name] = true
			} else {
				// Use the expanded component instances to get resolved paths
				// Check if any instance of this component has a changed path
				for _, envInstances := range instances {
					for _, inst := range envInstances {
						if inst.ComponentName == comp.Name && inst.Path != "" && inst.Path != "./" {
							if isPathChanged(changedSet, inst.Path) {
								changedComps[comp.Name] = true
								break
							}
						}
					}
					if changedComps[comp.Name] {
						break
					}
				}
			}
		}

		// Use dependency resolver to include all required dependencies
		resolver := expand.NewDependencyResolver(normalized)
		includedComps := resolver.ResolveComponentSet(changedComps)

		// Filter instances to include changed components and their dependencies
		for envName := range instances {
			var filtered []*model.ComponentInstance
			for _, inst := range instances[envName] {
				if includedComps[inst.ComponentName] {
					filtered = append(filtered, inst)
				}
			}
			instances[envName] = filtered
		}
	}

	if debugMode {
		count := 0
		for _, envInsts := range instances {
			count += len(envInsts)
		}
		fmt.Printf("  Generated %d component instances\n", count)
	}

	fmt.Println("□ Binding jobs and resolving dependencies...")
	jobPlanner := planner.NewJobPlanner(compositionInfos)
	jobInstances, err := jobPlanner.PlanJobs(instances)
	if err != nil {
		return fmt.Errorf("failed to plan jobs: %w", err)
	}

	fmt.Println("□ Detecting cycles...")
	dag := planner.NewJobGraph(jobInstances)
	if err := dag.DetectCycles(); err != nil {
		return fmt.Errorf("cycle detection failed: %w", err)
	}

	fmt.Println("□ Topologically sorting...")
	sorted, err := dag.TopologicalSort()
	if err != nil {
		return fmt.Errorf("topological sort failed: %w", err)
	}

	if debugMode {
		fmt.Printf("  Sorted %d jobs\n", len(sorted))
	}

	fmt.Println("□ Rendering plan...")

	// Build JobRegistry bindings map (model -> JobRegistry name)
	jobBindings := make(map[string]string)
	for typeName, composition := range compositionRegistry.Types {
		if composition.JobRegistryName != "" {
			jobBindings[typeName] = composition.JobRegistryName
		}
	}

	renderer := render.NewRenderer()
	plan := renderer.RenderPlanWithOrder(intent.Metadata, jobInstances, jobBindings, sorted)
	plan.Spec.CompositionSources = compositionRegistry.Sources

	if debugMode {
		fmt.Println("\n" + renderer.DebugDump(plan))
	}

	// Write plan to file
	if err := renderer.WritePlan(plan, outputFile); err != nil {
		return fmt.Errorf("failed to write plan: %w", err)
	}

	fmt.Printf("✓ Plan generated with %d jobs\n", len(plan.Jobs))
	fmt.Printf("✓ Saved to: %s\n", outputFile)

	// Handle --view flag
	if viewPlan != "" {
		viewer := render.NewPlanViewer(plan).SetColor(ui.ColorEnabledForWriter(os.Stdout))
		var output string

		switch {
		case viewPlan == "dag":
			output = viewer.ViewDAG()
		case viewPlan == "dependencies":
			output = viewer.ViewDependencies()
		case strings.HasPrefix(viewPlan, "component="):
			componentName := strings.TrimPrefix(viewPlan, "component=")
			output = viewer.ViewByComponent(componentName)
		default:
			// Default to DAG view
			output = viewer.ViewDAG()
		}

		fmt.Println("\n" + output)
	}

	return nil
}

func validateFiles() error {
	fmt.Println("□ Validating intent...")
	intent, _, err := loadResolvedIntentFile(intentFile)
	if err != nil {
		return fmt.Errorf("failed to load intent: %w", err)
	}

	fmt.Println("✓ Intent is valid")

	fmt.Println("□ Normalizing intent...")
	_, err = normalize.NormalizeIntent(intent)
	if err != nil {
		return fmt.Errorf("normalization failed: %w", err)
	}

	fmt.Println("✓ All validation passed")
	return nil
}

func debugIntent() error {
	fmt.Println("□ Loading and normalizing...")
	intent, tree, err := loadResolvedIntentFile(intentFile)
	if err != nil {
		return err
	}

	normalized, err := normalize.NormalizeIntent(intent)
	if err != nil {
		return err
	}

	fmt.Printf("\nMetadata: %+v\n", normalized.Metadata)
	fmt.Printf("Groups: %d\n", len(normalized.Groups))
	for name, group := range normalized.Groups {
		fmt.Printf("  - %s: policies=%v, defaults=%v\n", name, group.Policies, group.Defaults)
	}

	fmt.Printf("Environments: %d\n", len(normalized.Environments))
	for name, env := range normalized.Environments {
		fmt.Printf("  - %s: %d components, policies=%v\n", name, len(env.Selectors.Components), env.Policies)
	}

	fmt.Printf("Components: %d\n", len(normalized.Components))
	for name, comp := range normalized.Components {
		fmt.Printf("  - %s: type=%s, domain=%s, enabled=%v, deps=%d\n",
			name, comp.Type, comp.Domain, comp.Enabled, len(comp.DependsOn))
	}

	discovered := 0
	for _, component := range tree.Components {
		if component.Source == "discovered" {
			discovered++
		}
	}
	if discovered > 0 {
		fmt.Printf("Discovered components: %d (roots=%v)\n", discovered, tree.Discovery.Roots)
	}

	return nil
}

func listCompositions(args []string) error {
	intent, _, intentErr := loadResolvedIntentFile(intentFile)

	var compositionRegistry *loader.CompositionRegistry
	var err error
	if intentErr == nil {
		compositionRegistry, err = loader.LoadCompositionsForIntent(intent, intentFile, configDir)
		if err != nil {
			return fmt.Errorf("failed to resolve compositions: %w", err)
		}
	} else {
		compositionRegistry, err = loader.LoadCompositionsFromDir(configDir)
		if err != nil {
			return fmt.Errorf("failed to load compositions from %s: %w", configDir, err)
		}
	}

	// If a specific composition is requested, show detailed info
	if len(args) > 0 {
		compositionName := args[0]
		composition, exists := compositionRegistry.Types[compositionName]
		if !exists {
			return fmt.Errorf("composition not found: %s", compositionName)
		}

		info, err := ExtractModelInfo(compositionName, composition, configDir)
		if err != nil {
			return fmt.Errorf("failed to extract composition info: %w", err)
		}

		PrintLongFormat(info, expandJobs)
		return nil
	}

	// List all compositions
	fmt.Println(stylePanel("┌──────────────────────────────────────────────────────────┐"))
	fmt.Println(styleTitle("│ compositions                                             │"))
	fmt.Println(stylePanel("├──────────────────────────────────────────────────────────┤"))
	if len(compositionRegistry.Sources) > 0 {
		firstSource := compositionRegistry.Sources[0]
		location := firstSource.Ref
		if location == "" {
			location = firstSource.Path
		}
		fmt.Printf("│ source: %s (%s)\n", firstSource.Name, firstSource.Kind)
		if location != "" {
			fmt.Printf("│ location: %s\n", location)
		}
	} else {
		fmt.Println("│ source: none                                             │")
	}
	fmt.Println(stylePanel("└──────────────────────────────────────────────────────────┘"))

	// Sort composition names for consistent output
	var compositionNames []string
	for compositionName := range compositionRegistry.Types {
		compositionNames = append(compositionNames, compositionName)
	}
	sort.Strings(compositionNames)

	// Print header
	if longFormat {
		// Long format - show each composition's full details
		for _, compositionName := range compositionNames {
			composition := compositionRegistry.Types[compositionName]
			info, _ := ExtractModelInfo(compositionName, composition, configDir)
			PrintLongFormat(info, expandJobs)
		}
	} else {
		// Short format - just names and job descriptions
		fmt.Println("\n" + styleTitle("Available"))
		for _, compositionName := range compositionNames {
			composition := compositionRegistry.Types[compositionName]
			if len(composition.Jobs) > 0 {
				fmt.Printf("  ├─ %s\n", compositionName)
			}
		}
	}

	if !longFormat {
		fmt.Println("\n" + styleTip("Tip: run 'gluon composition <name>' for detailed information"))
	}

	return nil
}

func listComponents(args []string) error {
	fmt.Println(stylePanel("┌──────────────────────────────────────────────────────────┐"))
	fmt.Println(styleTitle("│ components                                               │"))
	fmt.Println(stylePanel("├──────────────────────────────────────────────────────────┤"))
	fmt.Printf("│ intent: %s\n", intentFile)
	fmt.Println(stylePanel("└──────────────────────────────────────────────────────────┘"))

	fmt.Println("□ Loading intent...")
	intent, _, err := loadResolvedIntentFile(intentFile)
	if err != nil {
		return fmt.Errorf("failed to load intent: %w", err)
	}

	fmt.Println("□ Normalizing intent...")
	normalized, err := normalize.NormalizeIntent(intent)
	if err != nil {
		return fmt.Errorf("failed to normalize intent: %w", err)
	}

	// Analyze components first to get expanded/resolved paths
	analyzer := expand.NewComponentAnalyzer(normalized)
	components, err := analyzer.ListAll()
	if err != nil {
		return fmt.Errorf("failed to analyze components: %w", err)
	}

	// Initialize change detector if --changed flag is set
	var changedComps map[string]bool
	if changedOnly {
		changedComps = make(map[string]bool)

		changeOptions, err := buildChangeOptions()
		if err != nil {
			return err
		}

		changedSet, err := changedFilesSet(changeOptions)
		if err != nil {
			return fmt.Errorf("failed to detect changed files: %w", err)
		}

		// Check intent file for changes
		intentChanged := isIntentPathChanged(changedSet, intentFile)

		// For each component, check if any resolved paths have changed
		for _, comp := range components {
			normalizedComp, exists := normalized.ComponentIndex[comp.Name]
			if !exists {
				continue
			}
			if intentChanged {
				// If intent changed, all components are affected
				changedComps[comp.Name] = true
			} else if isFileChanged(changedSet, normalizedComp.SourcePath) {
				changedComps[comp.Name] = true
			} else {
				// Check each instance's resolved path
				found := false
				for _, inst := range comp.Instances {
					if inst.Path != "" && inst.Path != "./" {
						if isPathChanged(changedSet, inst.Path) {
							changedComps[comp.Name] = true
							found = true
							break
						}
					}
				}
				if found {
					continue
				}
			}
		}

		if len(changedComps) == 0 {
			fmt.Println("\n" + stylePanel("┌──────────────────────────────────────────────────────────┐"))
			fmt.Println(styleTitle("│ changed components                                       │"))
			fmt.Println(stylePanel("├──────────────────────────────────────────────────────────┤"))
			fmt.Println("│ none                                                     │")
			fmt.Println(stylePanel("└──────────────────────────────────────────────────────────┘"))
			return nil
		}
	}

	// Filter by specific component if requested
	if len(args) > 0 {
		componentName := args[0]
		comp, err := analyzer.GetComponentByName(componentName)
		if err != nil {
			return fmt.Errorf("failed to get component: %w", err)
		}

		if comp.Type == "" {
			return fmt.Errorf("component not found: %s", componentName)
		}

		if changedOnly && !changedComps[componentName] {
			fmt.Printf("component %s has not changed\n", componentName)
			return nil
		}

		printComponentDetails(comp)
		return nil
	}

	// List all components (or just changed ones)
	if len(components) == 0 {
		fmt.Println("No components found")
		return nil
	}

	// If --changed, use dependency resolver to show categorized components
	if changedOnly && len(changedComps) > 0 {
		resolver := expand.NewDependencyResolver(normalized)
		changed, dependencies, dependents := resolver.CategorizeDependencies(changedComps)

		// Create a combined map for filtering
		includedComps := make(map[string]bool)
		for comp := range changed {
			includedComps[comp] = true
		}
		for comp := range dependencies {
			includedComps[comp] = true
		}
		for comp := range dependents {
			includedComps[comp] = true
		}

		fmt.Println("\n" + styleTitle("Components:"))
		fmt.Println("\n" + styleTitle("Dependency impact"))

		// Print changed components
		if len(changed) > 0 {
			fmt.Println("\n  ├─ changed")
			for _, comp := range components {
				if changed[comp.Name] {
					if longFormat {
						printComponentDetails(comp)
					} else {
						fmt.Printf("  │  ├─ %s (type=%s, domain=%s, enabled=%v, envs=%d)\n",
							comp.Name, comp.Type, comp.Domain, comp.Enabled, len(comp.Instances))
					}
				}
			}
		}

		// Print dependencies
		if len(dependencies) > 0 {
			fmt.Println("\n  ├─ dependencies")
			for _, comp := range components {
				if dependencies[comp.Name] {
					if longFormat {
						printComponentDetails(comp)
					} else {
						fmt.Printf("  │  ├─ %s (type=%s, domain=%s, enabled=%v, envs=%d)\n",
							comp.Name, comp.Type, comp.Domain, comp.Enabled, len(comp.Instances))
					}
				}
			}
		}

		// Print dependents
		if len(dependents) > 0 {
			fmt.Println("\n  └─ dependents")
			for _, comp := range components {
				if dependents[comp.Name] {
					if longFormat {
						printComponentDetails(comp)
					} else {
						fmt.Printf("     ├─ %s (type=%s, domain=%s, enabled=%v, envs=%d)\n",
							comp.Name, comp.Type, comp.Domain, comp.Enabled, len(comp.Instances))
					}
				}
			}
		}

		if !longFormat {
			fmt.Println("\n" + styleTip("Tip: run 'gluon component <name>' for detailed information"))
		}
		return nil
	}

	fmt.Println("\n" + styleTitle("Component list"))
	for _, comp := range components {
		// Skip if --changed flag and component hasn't changed
		if changedOnly && !changedComps[comp.Name] {
			continue
		}

		if longFormat {
			printComponentDetails(comp)
		} else {
			fmt.Printf("  ├─ %s (type=%s, domain=%s, enabled=%v, envs=%d)\n",
				comp.Name, comp.Type, comp.Domain, comp.Enabled, len(comp.Instances))
		}
	}

	if !longFormat {
		fmt.Println("\n" + styleTip("Tip: run 'gluon component <name>' for detailed information"))
	}

	return nil
}

func printComponentDetails(comp *expand.ComponentMerged) {
	fmt.Printf("\n╭─ Component %s\n", comp.Name)
	fmt.Printf("│  type: %s\n", comp.Type)
	fmt.Printf("│  domain: %s\n", comp.Domain)
	fmt.Printf("│  enabled: %v\n", comp.Enabled)
	if comp.SourcePath != "" {
		fmt.Printf("│  source: %s\n", comp.SourcePath)
	}

	if len(comp.Dependencies) > 0 {
		fmt.Printf("│  dependencies: %s\n", strings.Join(comp.Dependencies, ", "))
	}

	fmt.Printf("│  instances (%d):\n", len(comp.Instances))
	for _, inst := range comp.Instances {
		fmt.Printf("│  ├─ [%s] path=%s\n", inst.Environment, inst.Path)
		if len(inst.Inputs) > 0 {
			fmt.Printf("│  │  inputs:\n")
			for k, v := range inst.Inputs {
				fmt.Printf("│  │    %s: %v\n", k, v)
			}
		}
	}
	fmt.Printf("╰─ end component %s\n", comp.Name)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func loadResolvedIntentFile(path string) (*model.Intent, *model.ComponentTree, error) {
	intent, tree, err := loader.LoadResolvedIntent(path)
	if err != nil {
		return nil, nil, err
	}
	if err := loader.WriteComponentTreeCache(path, tree); err != nil {
		fmt.Fprintf(os.Stderr, "⚠ warning: failed to write component tree cache: %v\n", err)
	}
	return intent, tree, nil
}

func changedFilesSet(options git.ChangeOptions) (map[string]struct{}, error) {
	detector := git.NewChangeDetectorWithOptions(options)
	files, err := detector.GetChangedFiles()
	if err != nil {
		return nil, err
	}

	result := make(map[string]struct{}, len(files))
	for _, file := range files {
		if file == "" {
			continue
		}
		result[file] = struct{}{}
	}

	return result, nil
}

func buildChangeOptions() (git.ChangeOptions, error) {
	options := git.ChangeOptions{
		Base:        strings.TrimSpace(baseBranch),
		Head:        strings.TrimSpace(headRef),
		Files:       changedFiles,
		Uncommitted: uncommitted,
		Untracked:   untracked,
	}

	if err := git.ValidateOptions(options); err != nil {
		return git.ChangeOptions{}, err
	}

	return options, nil
}

func isPathChanged(changedFiles map[string]struct{}, path string) bool {
	if path == "" || path == "./" {
		return len(changedFiles) > 0
	}

	path = strings.TrimSuffix(path, "/")
	prefix := path + "/"
	for file := range changedFiles {
		if file == path || strings.HasPrefix(file, prefix) {
			return true
		}
	}

	return false
}

func isIntentPathChanged(changedFiles map[string]struct{}, intentPath string) bool {
	return isFileChanged(changedFiles, intentPath)
}

func isFileChanged(changedFiles map[string]struct{}, targetPath string) bool {
	if targetPath == "" {
		return false
	}

	normalizedTarget := normalizeFilePath(targetPath)
	base := filepathBase(normalizedTarget)
	for file := range changedFiles {
		normalizedFile := normalizeFilePath(file)
		if normalizedFile == normalizedTarget || normalizedFile == base || strings.HasSuffix(normalizedFile, "/"+base) || strings.HasSuffix(normalizedTarget, "/"+normalizedFile) {
			return true
		}
	}

	return false
}

func filepathBase(path string) string {
	parts := strings.Split(normalizeFilePath(path), "/")
	if len(parts) == 0 {
		return path
	}
	return parts[len(parts)-1]
}

func normalizeFilePath(path string) string {
	return strings.TrimSuffix(strings.ReplaceAll(path, "\\", "/"), "/")
}
