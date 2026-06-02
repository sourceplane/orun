package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/artifactstore/github"
	"github.com/sourceplane/orun/internal/ci"
	"github.com/sourceplane/orun/internal/expand"
	"github.com/sourceplane/orun/internal/git"
	"github.com/sourceplane/orun/internal/loader"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/normalize"
	"github.com/sourceplane/orun/internal/planner"
	"github.com/sourceplane/orun/internal/preset"
	"github.com/sourceplane/orun/internal/render"
	"github.com/sourceplane/orun/internal/revision"
	"github.com/sourceplane/orun/internal/runbundle"
	"github.com/sourceplane/orun/internal/state"
	"github.com/sourceplane/orun/internal/statestore"
	"github.com/sourceplane/orun/internal/trigger"
	"github.com/sourceplane/orun/internal/triggerctx"
	"github.com/sourceplane/orun/internal/ui"
)

func generatePlan() error {
	if debugMode {
		fmt.Println("□ Loading intent...")
	}
	intent, _, err := loadResolvedIntentFile(intentFile)
	if err != nil {
		return fmt.Errorf("failed to load intent: %w", err)
	}

	if debugMode {
		fmt.Println("□ Loading compositions...")
	}
	compositionRegistry, err := loader.LoadCompositionsForIntent(intent, intentFile, configDir)
	if err != nil {
		return fmt.Errorf("failed to resolve compositions: %w", err)
	}
	if err := loader.WriteCompositionLockFile(intentFile, compositionRegistry.Sources); err != nil {
		fmt.Fprintf(os.Stderr, "⚠ warning: failed to write composition lock file: %v\n", err)
	}

	// Resolve and merge intent presets from composition sources
	if len(intent.Extends) > 0 {
		if debugMode {
			fmt.Printf("□ Resolving %d intent presets...\n", len(intent.Extends))
		}
		if err := preset.ValidateExtendsRefs(intent); err != nil {
			return err
		}
		resolvedPresets, err := preset.LoadPresetsForIntent(intent, compositionRegistry.SourceRoots)
		if err != nil {
			return fmt.Errorf("failed to load intent presets: %w", err)
		}
		mergeResult, err := preset.MergePresets(intent, resolvedPresets)
		if err != nil {
			return fmt.Errorf("failed to merge intent presets: %w", err)
		}
		intent = mergeResult.Intent
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
			Type:              composition.Name,
			DefaultJob:        defaultJob,
			ExecutionProfiles: composition.ExecutionProfiles,
			JobMap:            composition.JobMap,
		}
	}

	if debugMode {
		fmt.Println("□ Normalizing intent...")
	}
	normalized, err := normalize.NormalizeIntent(intent)
	if err != nil {
		return fmt.Errorf("failed to normalize intent: %w", err)
	}

	if debugMode {
		fmt.Println("□ Validating components against composition schemas...")
	}
	if err := compositionRegistry.ValidateAllComponents(normalized); err != nil {
		return fmt.Errorf("component validation failed: %w", err)
	}

	if errs := trigger.ValidateProfileRules(intent); len(errs) > 0 {
		return trigger.FormatErrors(errs)
	}
	if errs := trigger.ValidateDependencyRules(intent); len(errs) > 0 {
		return trigger.FormatErrors(errs)
	}

	// Resolve trigger context and determine active environments
	triggerCtx, err := buildTriggerContext()
	if err != nil {
		return fmt.Errorf("trigger resolution failed: %w", err)
	}

	var triggerResolution *model.TriggerResolution
	var triggerActiveEnvs []string

	if triggerCtx.Mode != "none" && triggerCtx.Mode != "" {
		if debugMode {
			fmt.Printf("□ Resolving trigger (%s)...\n", triggerCtx.Mode)
		}

		if err := trigger.ValidateTriggerContext(intent, triggerCtx); err != nil {
			return err
		}

		triggerActiveEnvs, triggerResolution, err = trigger.ResolveActiveEnvironments(intent, triggerCtx, environment)
		if err != nil {
			return err
		}

		if debugMode {
			fmt.Printf("  Matched triggers: %s\n", strings.Join(triggerResolution.MatchedTriggerNames, ", "))
			fmt.Printf("  Active environments: %s\n", strings.Join(triggerResolution.ActiveEnvironments, ", "))
			fmt.Printf("  Plan scope: %s\n", triggerResolution.PlanScope)
		}

		// Apply trigger-resolved scope: if scope=changed, enable changed mode with resolved base/head
		if triggerResolution.PlanScope == "changed" && !changedOnly {
			changedOnly = true
			if baseBranch == "" && triggerResolution.Base != "" {
				baseBranch = triggerResolution.Base
			}
			if headRef == "" && triggerResolution.Head != "" {
				headRef = triggerResolution.Head
			}
		}
	}

	if debugMode {
		fmt.Println("□ Expanding (env × component)...")
	}
	expander := expand.NewExpander(normalized).WithRegistry(compositionRegistry)
	if triggerResolution != nil {
		expander = expander.WithMatchedTriggers(triggerResolution.MatchedTriggerNames)
	}
	instances, err := expander.Expand()
	if err != nil {
		return fmt.Errorf("failed to expand intent: %w", err)
	}

	// Filter by trigger-resolved environments or --env flag
	if len(triggerActiveEnvs) > 0 {
		envSet := make(map[string]struct{}, len(triggerActiveEnvs))
		for _, env := range triggerActiveEnvs {
			envSet[env] = struct{}{}
		}
		filtered := make(map[string][]*model.ComponentInstance)
		for envName, envInsts := range instances {
			if _, ok := envSet[envName]; ok {
				filtered[envName] = envInsts
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("trigger-activated environments %v produced no component instances", triggerActiveEnvs)
		}
		instances = filtered
	} else if environment != "" {
		envFilters := parseCommaSeparated(environment)
		filtered := make(map[string][]*model.ComponentInstance)
		for envName, envInsts := range instances {
			if matchesAnyFilter(envName, envFilters) {
				filtered[envName] = envInsts
			}
		}
		if len(filtered) == 0 {
			available := make([]string, 0, len(instances))
			for envName := range instances {
				available = append(available, envName)
			}
			sort.Strings(available)
			return formatEnvNotFoundError(environment, available)
		}
		instances = filtered
	}

	// Filter by --component flag
	if len(planComponents) > 0 {
		for envName, envInsts := range instances {
			var filtered []*model.ComponentInstance
			for _, inst := range envInsts {
				if matchesAnyFilter(inst.ComponentName, planComponents) {
					filtered = append(filtered, inst)
				}
			}
			instances[envName] = filtered
		}
		// Remove empty environments
		for envName, envInsts := range instances {
			if len(envInsts) == 0 {
				delete(instances, envName)
			}
		}
		if len(instances) == 0 {
			available := make([]string, 0)
			// Re-expand to get all component names for suggestion
			allInstances, _ := expander.Expand()
			if allInstances != nil {
				compSet := make(map[string]struct{})
				for _, envInsts := range allInstances {
					for _, inst := range envInsts {
						compSet[inst.ComponentName] = struct{}{}
					}
				}
				for name := range compSet {
					available = append(available, name)
				}
				sort.Strings(available)
			}
			return formatComponentNotFoundError(strings.Join(planComponents, ", "), available)
		}
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
		changedComps := collectChangedComponents(normalized, instances, changedSet, intentFile, changeOptions)

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

	if debugMode {
		fmt.Println("□ Binding jobs and resolving dependencies...")
	}
	jobPlanner := planner.NewJobPlanner(compositionInfos)
	jobInstances, err := jobPlanner.PlanJobs(instances)
	if err != nil {
		return fmt.Errorf("failed to plan jobs: %w", err)
	}

	// Resolve environment promotion dependencies into DAG edges or gates
	if err := planner.ResolvePromotionDependencies(jobInstances, instances, normalized.Environments); err != nil {
		return fmt.Errorf("failed to resolve promotion dependencies: %w", err)
	}

	if debugMode {
		fmt.Println("□ Detecting cycles...")
	}
	dag := planner.NewJobGraph(jobInstances)
	if err := dag.DetectCycles(); err != nil {
		return fmt.Errorf("cycle detection failed: %w", err)
	}

	if debugMode {
		fmt.Println("□ Topologically sorting...")
	}
	sorted, err := dag.TopologicalSort()
	if err != nil {
		return fmt.Errorf("topological sort failed: %w", err)
	}

	if debugMode {
		fmt.Printf("  Sorted %d jobs\n", len(sorted))
		fmt.Println("□ Rendering plan...")
	}

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

	// --- M5A: always resolve TriggerOccurrence and write a PlanRevision.
	// The new pipeline replaces the legacy `state.SavePlan` call below.
	// triggerCtx (legacy) was already populated above; we translate its
	// branches into triggerctx.ResolveOptions so the canonical path runs
	// regardless of whether the user passed --trigger / --from-ci or not.
	resolveOpts := triggerctx.ResolveOptions{
		Kind:               triggerctx.ResolveKindSystem,
		SystemFlavor:       triggerctx.SystemManual,
		ActivationMode:     "",
		ActiveEnvironments: nil,
		ChangedComponents:  nil,
	}
	if triggerResolution != nil {
		resolveOpts.ActiveEnvironments = triggerResolution.ActiveEnvironments
		resolveOpts.PlanScopeMode = triggerResolution.PlanScope
		resolveOpts.PlanBase = triggerResolution.Base
		resolveOpts.PlanHead = triggerResolution.Head
	}
	if triggerCtx.Mode == "named-trigger" && triggerName != "" {
		resolveOpts.Kind = triggerctx.ResolveKindDeclaredByName
		resolveOpts.TriggerName = triggerName
	} else if triggerCtx.Mode == "event-file" && triggerCtx.Event != nil {
		resolveOpts.Kind = triggerctx.ResolveKindFromCI
		resolveOpts.ProviderEvent = triggerCtx.Event
		resolveOpts.Action = triggerCtx.Event.Action
	} else if changedOnly {
		resolveOpts.SystemFlavor = triggerctx.SystemManualChanged
	}
	trig, err := triggerctx.ResolveTriggerContext(resolveOpts, intent, nil)
	if err != nil {
		return fmt.Errorf("trigger resolution failed: %w", err)
	}

	// Embed PlanTrigger metadata (Type/Name + scope/bindings) so plan.json
	// is self-describing per cli-surface.md §1.3.
	planTrigger := &model.PlanTrigger{
		Type:               trig.TriggerType,
		Name:               trig.TriggerName,
		Mode:               trig.Mode,
		Provider:           trig.Provider,
		Event:              trig.Event,
		Action:             trig.Action,
		MatchedBindings:    append([]string(nil), trig.MatchedBindings...),
		ActiveEnvironments: append([]string(nil), trig.PlanScope.ActiveEnvironments...),
		Scope:              trig.PlanScope.Mode,
		Base:               trig.PlanScope.Base,
		Head:               trig.PlanScope.Head,
	}
	if planTrigger.MatchedBindings == nil {
		planTrigger.MatchedBindings = []string{}
	}
	if planTrigger.ActiveEnvironments == nil {
		planTrigger.ActiveEnvironments = []string{}
	}
	plan.Metadata.Trigger = planTrigger

	// Embed the intent directory as a workspace-relative path so that
	// orun run can resolve component paths correctly when auto-discovery
	// cannot walk up to find the intent (e.g. intent in a repo subdirectory).
	if intentRoot != "" {
		if cwd, err := os.Getwd(); err == nil {
			if rel, err := filepath.Rel(cwd, intentRoot); err == nil && !strings.HasPrefix(rel, "..") {
				plan.Metadata.WorkDir = filepath.ToSlash(rel)
			}
		}
	}

	if debugMode {
		fmt.Println("\n" + renderer.DebugDump(plan))
	}

	// Resolve catalog context before computing the plan hash so source/catalog
	// metadata is included in the hash. A plan compiled against a different
	// catalog should produce a different revision key.
	catRes, err := resolvePlanCatalog(context.Background(), planCatalogOptions{
		NoRefresh:      planNoCatalogRefresh,
		Strict:         planCatalogStrict,
		SourceSelector: planCatalogSource,
		SnapshotKey:    planCatalogSnapshot,
	})
	if err != nil {
		return fmt.Errorf("resolve plan catalog: %w", err)
	}

	// Stamp source/catalog metadata into the plan when resolved.
	if catRes.Resolved && catRes.Source != nil {
		plan.Metadata.Source = &model.PlanSourceMeta{
			SnapshotKey:  catRes.Source.SourceSnapshotKey,
			Ref:          catRes.Source.Ref,
			HeadRevision: catRes.Source.HeadRevision,
			TreeHash:     catRes.Source.TreeHash,
			WorkingTree:  catRes.Source.WorkingTree,
			DirtyHash:    catRes.Source.DirtyHash,
		}
	}
	if catRes.Resolved && catRes.Catalog != nil {
		plan.Metadata.Catalog = &model.PlanCatalogMeta{
			SnapshotKey:       catRes.Catalog.CatalogSnapshotKey,
			CatalogHash:       catRes.Catalog.CatalogHash,
			SourceSnapshotKey: catRes.Catalog.SourceSnapshotKey,
		}
	} else if catRes.Skipped {
		plan.Metadata.Catalog = &model.PlanCatalogMeta{Skipped: true}
	}

	// Compute planHash from the plan with metadata.revision and
	// metadata.checksum cleared — that is the spec-canonical "plan content
	// without its self-reference" (data-model.md §3.1). We then embed
	// metadata.revision pointing at the resulting key and marshal the final
	// plan bytes that will be persisted as plan.json. The checksum field
	// retains the legacy "sha256-<hex>" form computed by the renderer for
	// back-compat with tooling that reads .orun/plans/.
	planHash, err := computePlanHashForRevision(plan)
	if err != nil {
		return fmt.Errorf("compute plan hash: %w", err)
	}
	revKey, err := revision.RevisionKey(trig, planHash)
	if err != nil {
		return fmt.Errorf("derive revision key: %w", err)
	}
	plan.Metadata.Revision = &model.PlanRevisionMeta{
		Key:      revKey,
		PlanHash: planHash,
	}

	// Final canonical plan.json bytes (with metadata.revision embedded).
	planBytes, err := canonicalPlanJSON(plan)
	if err != nil {
		return fmt.Errorf("marshal canonical plan: %w", err)
	}

	// Write plan via revision.WriteRevision (canonical layout) plus optional
	// compatibility writes (.orun/plans/<checksum>.json + latest.json) and
	// optional named alias.
	store := state.NewStore(storeDir())
	planID := state.PlanChecksumShort(plan)
	color := ui.ColorEnabledForWriter(os.Stdout)

	absStoreRoot, err := filepath.Abs(filepath.Join(storeDir(), ".orun"))
	if err != nil {
		return fmt.Errorf("resolve store root: %w", err)
	}
	stateStore, err := statestore.NewLocalStore(statestore.LocalConfig{Root: absStoreRoot})
	if err != nil {
		return fmt.Errorf("open state store: %w", err)
	}

	revCfg := revision.Config{
		Store:         stateStore,
		JobCount:      len(plan.Jobs),
		CatalogParent: catRes.Parent,
	}.WithCompatibilityWrites(true)

	rev, err := revision.WriteRevision(context.Background(), revCfg, trig, planBytes, planHash)
	if err != nil {
		return fmt.Errorf("write plan revision: %w", err)
	}
	if err := revision.WriteManifest(context.Background(), revCfg, rev, trig); err != nil {
		return fmt.Errorf("write revision manifest: %w", err)
	}
	if planName != "" {
		if err := revision.WriteLegacyNamedPlan(context.Background(), stateStore, planName, planBytes); err != nil {
			return fmt.Errorf("write named plan alias: %w", err)
		}
	}

	// `-o/--output` writes an additional copy to the user-specified path on
	// top of the canonical layout (cli-surface.md §1.1).
	if outputFile != "" {
		if err := renderer.WritePlan(plan, outputFile); err != nil {
			return fmt.Errorf("failed to write plan to %s: %w", outputFile, err)
		}
	}
	_ = store // legacy state.Store retained for any downstream plan resolution; SavePlan superseded by revision pipeline.

	// Additionally write the content-addressed object graph when the
	// ORUN_OBJECT_MODEL flag is set (M5b). Best-effort and isolated under
	// .orun/objectmodel/; a no-op when the flag is unset.
	writeObjectModelPlan(absStoreRoot, plan, planBytes, planHash, rev.RevisionKey, trig, catRes)

	// On-success M5.a summary block (cli-surface.md §1.1). Printed before the
	// legacy "components × envs → jobs" detail line so existing tooling that
	// scans for that line keeps working.
	canonicalPlanPath := filepath.Join(absStoreRoot, "revisions", rev.RevisionKey, "plan.json")
	triggerScope := trig.PlanScope.Mode
	if triggerScope == "" {
		triggerScope = "manual"
	}
	headRev := trig.Source.HeadRevision
	if len(headRev) > 7 {
		headRev = headRev[:7]
	}
	if headRev == "" {
		headRev = "no-head"
	}
	fmt.Println()
	fmt.Println(ui.Green(color, "✓") + " Plan revision created")
	fmt.Println()
	fmt.Printf("  Revision: %s\n", rev.RevisionKey)
	fmt.Printf("  Trigger:  %s / %s / %s\n", trig.TriggerName, triggerScope, headRev)
	fmt.Printf("  Jobs:     %d\n", len(plan.Jobs))
	fmt.Printf("  Path:     %s\n", canonicalPlanPath)
	if outputFile != "" {
		fmt.Printf("  Output:   %s\n", outputFile)
	}

	// Modern compact summary — derive component count from filtered instances
	compSet := make(map[string]struct{})
	for _, envInsts := range instances {
		for _, inst := range envInsts {
			compSet[inst.ComponentName] = struct{}{}
		}
	}
	compNames := make([]string, 0, len(compSet))
	for name := range compSet {
		compNames = append(compNames, name)
	}
	sort.Strings(compNames)

	numComponents := len(compNames)
	numEnvs := len(instances)
	numJobs := len(plan.Jobs)

	fmt.Printf("\n  %s %d components %s %d envs %s %s\n",
		ui.Dim(color, "│"),
		numComponents,
		ui.Dim(color, "×"),
		numEnvs,
		ui.Dim(color, "→"),
		ui.Bold(color, fmt.Sprintf("%d jobs", numJobs)),
	)
	if numComponents > 0 {
		const maxShow = 4
		displayed := compNames
		if len(displayed) > maxShow {
			displayed = displayed[:maxShow]
		}
		label := strings.Join(displayed, ", ")
		if len(compNames) > maxShow {
			label += fmt.Sprintf(" (+%d more)", len(compNames)-maxShow)
		}
		fmt.Printf("  %s components: %s\n", ui.Dim(color, "│"), label)
	}
	if changedOnly {
		fmt.Printf("  %s mode: %s\n", ui.Dim(color, "│"), ui.Cyan(color, "changed-only"))
	}
	if planName != "" {
		fmt.Printf("  %s name: %s\n", ui.Dim(color, "│"), planName)
	}
	fmt.Printf("  %s plan: %s\n", ui.Dim(color, "│"), ui.Dim(color, planID))
	if outputFile != "" {
		fmt.Printf("  %s file: %s\n", ui.Dim(color, "│"), outputFile)
	}
	fmt.Println()

	// Actionable hint
	if numJobs > 0 {
		if planID != "" {
			fmt.Printf("  %s orun run %s\n", ui.Dim(color, "→"), planID)
		}
		if planName != "" {
			fmt.Printf("  %s orun run %s\n", ui.Dim(color, "→"), planName)
		}
	}

	// Handle --view flag
	if viewPlan != "" {
		viewer := render.NewPlanViewer(plan).SetColor(ui.ColorEnabledForWriter(os.Stdout))
		var output string

		switch {
		case viewPlan == "dag":
			viewer.SetLong(planLong)
			output = viewer.ViewDAG()
		case viewPlan == "dag:long":
			viewer.SetLong(true)
			output = viewer.ViewDAG()
		case viewPlan == "dependencies":
			output = viewer.ViewDependencies()
		case strings.HasPrefix(viewPlan, "component="):
			componentName := strings.TrimPrefix(viewPlan, "component=")
			output = viewer.ViewByComponent(componentName)
		default:
			viewer.SetLong(planLong)
			output = viewer.ViewDAG()
		}

		fmt.Println("\n" + output)
	}

	// Artifact upload for CI: write plan shard and upload via --artifact flag
	if artifactBackend == "github" && os.Getenv("GITHUB_ACTIONS") == "true" && plan != nil {
		runID := os.Getenv("GITHUB_RUN_ID")
		runAttempt := os.Getenv("GITHUB_RUN_ATTEMPT")
		if runAttempt == "" {
			runAttempt = "1"
		}
		shortSHA := planID
		if len(shortSHA) > 12 {
			shortSHA = shortSHA[:12]
		}
		execID := runbundle.ExecID(runID, runAttempt, shortSHA)

		// Create shard source metadata
		source := runbundle.ShardSource{
			Type:       "github-actions",
			Repository: os.Getenv("GITHUB_REPOSITORY"),
			RunID:      runID,
			RunAttempt: runAttempt,
			Workflow:   os.Getenv("GITHUB_WORKFLOW"),
			SHA:        os.Getenv("GITHUB_SHA"),
			Ref:        os.Getenv("GITHUB_REF"),
			EventName:  os.Getenv("GITHUB_EVENT_NAME"),
		}

		// Write plan shard to temp directory
		shardDir, err := os.MkdirTemp("", "orun-plan-shard-*")
		if err != nil {
			fmt.Fprintf(os.Stderr, "⚠ warning: failed to create plan shard temp dir: %v\n", err)
		} else {
			defer os.RemoveAll(shardDir)

			shard, err := runbundle.WritePlanShard(context.Background(), runbundle.WritePlanShardOptions{
				ExecID:    execID,
				Plan:      plan,
				Source:    source,
				OutputDir: shardDir,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "⚠ warning: failed to write plan shard: %v\n", err)
			} else if shard != nil {
				// Upload plan shard via GitHub store
				repo := os.Getenv("GITHUB_REPOSITORY")
				if repo == "" {
					fmt.Fprintf(os.Stderr, "⚠ warning: GITHUB_REPOSITORY not set, cannot upload artifact\n")
				} else {
					ghClient, err := github.NewClient(context.Background(), repo)
					if err != nil {
						fmt.Fprintf(os.Stderr, "⚠ warning: failed to create GitHub client: %v\n", err)
					} else {
						result, err := ghClient.UploadShard(context.Background(), shard)
						if err != nil {
							fmt.Fprintf(os.Stderr, "⚠ warning: failed to upload plan artifact: %v\n", err)
						} else if result != nil {
							fmt.Fprintf(os.Stderr, "✓ uploaded plan artifact: %s (%d bytes)\n", result.Name, result.Size)
						}
					}
				}
			}
		}

		// --github-output: write matrix, plan_id, exec_id to $GITHUB_OUTPUT
		if githubOutput && os.Getenv("GITHUB_OUTPUT") != "" {
			outputPath := os.Getenv("GITHUB_OUTPUT")
			var outputLines []string

			// Build matrix JSON from plan jobs
			var matrixEntries []string
			for _, job := range plan.Jobs {
				entry := fmt.Sprintf(`{"id":%q,"uid":%q,"component":%q,"env":%q,"composition":%q,"profile":%q}`,
					job.ID, job.UID, job.Component, job.Environment, job.Composition, job.Profile)
				matrixEntries = append(matrixEntries, entry)
			}
			matrixJSON := fmt.Sprintf("{\"include\":[%s]}", strings.Join(matrixEntries, ","))
			outputLines = append(outputLines, fmt.Sprintf("matrix<<EOF\n%s\nEOF", matrixJSON))
			outputLines = append(outputLines, fmt.Sprintf("plan_id=%s", planID))
			outputLines = append(outputLines, fmt.Sprintf("exec_id=%s", execID))

			f, err := os.OpenFile(outputPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				fmt.Fprintf(os.Stderr, "⚠ warning: failed to open %s: %v\n", outputPath, err)
			} else {
				_, err = f.WriteString(strings.Join(outputLines, "\n") + "\n")
				f.Close()
				if err != nil {
					fmt.Fprintf(os.Stderr, "⚠ warning: failed to write %s: %v\n", outputPath, err)
				}
			}
		}
	}

	return nil
}

// computePlanHashForRevision returns the canonical "sha256:<hex>" digest of
// the plan's content with self-referential metadata (checksum, revision)
// cleared. This matches data-model.md §3.1: the plan hash is stable across
// re-runs of the same intent on the same SHA, irrespective of which alias
// it was previously persisted under.
func computePlanHashForRevision(plan *model.Plan) (string, error) {
	if plan == nil {
		return "", fmt.Errorf("plan is nil")
	}
	clone := *plan
	clone.Metadata.Checksum = ""
	clone.Metadata.Revision = nil
	payload, err := json.Marshal(&clone)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// canonicalPlanJSON marshals plan as deterministic indented JSON suitable
// for persistence as canonical plan.json (and for byte-identical compat
// aliases under .orun/plans/).
func canonicalPlanJSON(plan *model.Plan) ([]byte, error) {
	if plan == nil {
		return nil, fmt.Errorf("plan is nil")
	}
	return json.MarshalIndent(plan, "", "  ")
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

	if errs := trigger.ValidateProfileRules(intent); len(errs) > 0 {
		return trigger.FormatErrors(errs)
	}
	if errs := trigger.ValidateDependencyRules(intent); len(errs) > 0 {
		return trigger.FormatErrors(errs)
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
		fmt.Printf("  - %s: policies=%v, parameterDefaults=%v\n", name, group.Policies, group.ParameterDefaults)
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
		fmt.Println("\n" + styleTip("Tip: run 'orun composition <name>' for detailed information"))
	}

	return nil
}

func listComponents(args []string) error {
	intent, _, err := loadResolvedIntentFile(intentFile)
	if err != nil {
		return fmt.Errorf("failed to load intent: %w", err)
	}

	normalized, err := normalize.NormalizeIntent(intent)
	if err != nil {
		return fmt.Errorf("failed to normalize intent: %w", err)
	}

	analyzer := expand.NewComponentAnalyzer(normalized)
	components, err := analyzer.ListAll()
	if err != nil {
		return fmt.Errorf("failed to analyze components: %w", err)
	}

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
		changedComps = collectChangedComponents(normalized, instancesFromMergedComponents(components), changedSet, intentFile, changeOptions)

		if len(changedComps) == 0 {
			color := ui.ColorEnabledForWriter(os.Stdout)
			fmt.Println(ui.Dim(color, "No changed components."))
			return nil
		}
	}

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

	if len(components) == 0 {
		color := ui.ColorEnabledForWriter(os.Stdout)
		fmt.Println(ui.Dim(color, "No components found."))
		return nil
	}

	color := ui.ColorEnabledForWriter(os.Stdout)

	if changedOnly && len(changedComps) > 0 {
		resolver := expand.NewDependencyResolver(normalized)
		changed, dependencies, dependents := resolver.CategorizeDependencies(changedComps)

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

		if len(changed) > 0 {
			fmt.Printf("\n%s\n", ui.Bold(color, "Changed"))
			for _, comp := range components {
				if changed[comp.Name] {
					if longFormat {
						printComponentDetails(comp)
					} else {
						printComponentCompact(comp, color)
					}
				}
			}
		}

		if len(dependencies) > 0 {
			fmt.Printf("\n%s\n", ui.Bold(color, "Dependencies"))
			for _, comp := range components {
				if dependencies[comp.Name] {
					if longFormat {
						printComponentDetails(comp)
					} else {
						printComponentCompact(comp, color)
					}
				}
			}
		}

		if len(dependents) > 0 {
			fmt.Printf("\n%s\n", ui.Bold(color, "Dependents"))
			for _, comp := range components {
				if dependents[comp.Name] {
					if longFormat {
						printComponentDetails(comp)
					} else {
						printComponentCompact(comp, color)
					}
				}
			}
		}

		return nil
	}

	// Default listing
	visible := components
	if changedOnly {
		visible = nil
		for _, comp := range components {
			if changedComps[comp.Name] {
				visible = append(visible, comp)
			}
		}
	}

	fmt.Printf("%s components\n\n", ui.Bold(color, fmt.Sprintf("%d", len(visible))))

	if longFormat {
		for _, comp := range visible {
			printComponentDetails(comp)
		}
	} else {
		for _, comp := range visible {
			printComponentCompact(comp, color)
		}
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
		if len(inst.Parameters) > 0 {
			fmt.Printf("│  │  parameters:\n")
			for k, v := range inst.Parameters {
				fmt.Printf("│  │    %s: %v\n", k, v)
			}
		}
	}
	fmt.Printf("╰─ end component %s\n", comp.Name)
}

func printComponentCompact(comp *expand.ComponentMerged, color bool) {
	envNames := make([]string, 0, len(comp.Instances))
	for _, inst := range comp.Instances {
		envNames = append(envNames, inst.Environment)
	}
	sort.Strings(envNames)
	envStr := strings.Join(envNames, ",")

	enabledMark := ui.Green(color, "✓")
	if !comp.Enabled {
		enabledMark = ui.Dim(color, "–")
	}

	fmt.Fprintf(os.Stdout, "  %s %-24s %-28s %s\n",
		enabledMark, comp.Name, ui.Dim(color, comp.Type), ui.Dim(color, envStr))
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "✕ %v\n", err)
		var coder interface{ ExitCode() int }
		if errors.As(err, &coder) {
			os.Exit(coder.ExitCode())
		}
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
	base := strings.TrimSpace(baseBranch)
	head := strings.TrimSpace(headRef)

	// Auto-detect CI refs when no explicit source flags are provided.
	if base == "" && head == "" && len(changedFiles) == 0 && !uncommitted && !untracked {
		detected := ci.DetectRefs(os.Getenv, os.ReadFile)
		if detected.Provider != ci.ProviderNone {
			base = detected.Base
			head = detected.Head
			printCIDetectionBanner(detected)
		}
		if explainChanged {
			printExplainInfo(detected, strings.TrimSpace(baseBranch), strings.TrimSpace(headRef))
		}
	} else if explainChanged {
		printExplainInfo(ci.DetectedRefs{}, strings.TrimSpace(baseBranch), strings.TrimSpace(headRef))
	}

	options := git.ChangeOptions{
		Base:        base,
		Head:        head,
		Files:       changedFiles,
		Uncommitted: uncommitted,
		Untracked:   untracked,
	}

	if err := git.ValidateOptions(options); err != nil {
		return git.ChangeOptions{}, err
	}

	return options, nil
}

func printCIDetectionBanner(detected ci.DetectedRefs) {
	color := ui.ColorEnabledForWriter(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s %s (%s), base: %s, head: %s\n",
		ui.Cyan(color, "ci:"),
		string(detected.Provider),
		detected.EventType,
		ui.Bold(color, detected.Base),
		ui.Bold(color, detected.Head),
	)
}

func printExplainInfo(detected ci.DetectedRefs, explicitBase, explicitHead string) {
	color := ui.ColorEnabledForWriter(os.Stderr)
	fmt.Fprintf(os.Stderr, "\n%s --changed ref resolution\n", ui.Bold(color, "explain:"))
	fmt.Fprintf(os.Stderr, "  explicit --base flag: %s\n", valueOrNotSet(explicitBase))
	fmt.Fprintf(os.Stderr, "  explicit --head flag: %s\n", valueOrNotSet(explicitHead))
	if detected.Provider != ci.ProviderNone {
		fmt.Fprintf(os.Stderr, "  CI auto-detect: %s (%s)\n", detected.Provider, detected.EventType)
		for k, v := range detected.EnvVars {
			fmt.Fprintf(os.Stderr, "    %s=%s\n", k, v)
		}
		fmt.Fprintf(os.Stderr, "  resolved base: %s\n", detected.Base)
		fmt.Fprintf(os.Stderr, "  resolved head: %s\n\n", detected.Head)
	} else {
		fmt.Fprintf(os.Stderr, "  CI auto-detect: none (local or unrecognized CI)\n")
		fmt.Fprintf(os.Stderr, "  using defaults: base=main, head=HEAD\n\n")
	}
}

func valueOrNotSet(s string) string {
	if s == "" {
		return "(not set)"
	}
	return s
}

func semanticIntentDiff(options git.ChangeOptions, intentPath string) git.IntentDiffResult {
	normalizedPath := strings.TrimPrefix(normalizeFilePath(intentPath), "./")
	if normalizedPath == "" {
		normalizedPath = "intent.yaml"
	}

	base := options.Base
	head := options.Head
	if base == "" {
		base = "main"
	}
	if head == "" {
		head = "HEAD"
	}

	// When --files is provided, we cannot retrieve git content for semantic diff
	if len(options.Files) > 0 {
		return git.IntentDiffResult{
			Mode:   git.IntentDiffGlobal,
			Reason: "cannot perform semantic diff with --files (no git refs available)",
		}
	}

	baseYAML, err := git.GetFileAtRef(base, normalizedPath)
	if err != nil {
		return git.IntentDiffResult{
			Mode:   git.IntentDiffGlobal,
			Reason: fmt.Sprintf("cannot read base intent at %s:%s: %v", base, normalizedPath, err),
		}
	}

	headYAML, err := git.GetFileAtRef(head, normalizedPath)
	if err != nil {
		return git.IntentDiffResult{
			Mode:   git.IntentDiffGlobal,
			Reason: fmt.Sprintf("cannot read head intent at %s:%s: %v", head, normalizedPath, err),
		}
	}

	return git.DiffIntent(baseYAML, headYAML)
}

func printIntentDiffExplanation(result git.IntentDiffResult, normalized *model.NormalizedIntent) {
	color := ui.ColorEnabledForWriter(os.Stderr)
	fmt.Fprintf(os.Stderr, "\n%s intent.yaml semantic diff\n", ui.Bold(color, "explain:"))
	fmt.Fprintf(os.Stderr, "  intent changed: yes\n")
	fmt.Fprintf(os.Stderr, "  diff mode: %s\n", result.Mode)
	fmt.Fprintf(os.Stderr, "  reason: %s\n", result.Reason)
	if len(result.ChangedSections) > 0 {
		fmt.Fprintf(os.Stderr, "  changed sections: %s\n", strings.Join(result.ChangedSections, ", "))
	}
	if result.Mode == git.IntentDiffGlobal {
		fmt.Fprintf(os.Stderr, "  intent-impact: %s\n", intentImpact)
		if intentImpact == "watch" && normalized != nil {
			var matched, unmatched []string
			for _, comp := range normalized.Components {
				if watchesIntersect(comp.Change.Watches, result.ChangedSections) {
					matched = append(matched, comp.Name)
				} else {
					unmatched = append(unmatched, comp.Name)
				}
			}
			if len(matched) > 0 {
				fmt.Fprintf(os.Stderr, "  matched watches:\n")
				for _, name := range matched {
					comp := normalized.Components[name]
					fmt.Fprintf(os.Stderr, "    - %s (watches: %s)\n", name, strings.Join(comp.Change.Watches, ", "))
				}
			}
			if len(unmatched) > 0 {
				fmt.Fprintf(os.Stderr, "  unmatched:\n")
				for _, name := range unmatched {
					fmt.Fprintf(os.Stderr, "    - %s (no matching watches)\n", name)
				}
			}
		}
	}
	if len(result.Added) > 0 {
		fmt.Fprintf(os.Stderr, "  added: %s\n", strings.Join(result.Added, ", "))
	}
	if len(result.Modified) > 0 {
		fmt.Fprintf(os.Stderr, "  modified: %s\n", strings.Join(result.Modified, ", "))
	}
	if len(result.Removed) > 0 {
		fmt.Fprintf(os.Stderr, "  removed: %s\n", strings.Join(result.Removed, ", "))
	}
	fmt.Fprintln(os.Stderr)
}

func watchesIntersect(watches, sections []string) bool {
	for _, w := range watches {
		for _, s := range sections {
			if w == s {
				return true
			}
		}
	}
	return false
}

func isPathChanged(changedFiles map[string]struct{}, path string) bool {
	if path == "" || path == "./" {
		return len(changedFiles) > 0
	}

	path = strings.TrimPrefix(path, "./")
	path = strings.TrimSuffix(path, "/")
	prefix := path + "/"
	for file := range changedFiles {
		normalizedFile := strings.TrimPrefix(file, "./")
		if normalizedFile == path || strings.HasPrefix(normalizedFile, prefix) {
			return true
		}
	}

	return false
}

func collectChangedComponents(
	normalized *model.NormalizedIntent,
	instances map[string][]*model.ComponentInstance,
	changedFiles map[string]struct{},
	intentPath string,
	changeOptions git.ChangeOptions,
) map[string]bool {
	changedComponents := make(map[string]bool)
	if normalized == nil {
		return changedComponents
	}

	if isIntentPathChanged(changedFiles, intentPath) {
		diffResult := semanticIntentDiff(changeOptions, intentPath)
		if explainChanged {
			printIntentDiffExplanation(diffResult, normalized)
		}
		switch diffResult.Mode {
		case git.IntentDiffGlobal:
			if intentImpact == "all" {
				for _, comp := range normalized.Components {
					changedComponents[comp.Name] = true
				}
				return changedComponents
			}
			if intentImpact != "none" {
				for _, comp := range normalized.Components {
					if watchesIntersect(comp.Change.Watches, diffResult.ChangedSections) {
						changedComponents[comp.Name] = true
					}
				}
			}
		case git.IntentDiffComponents:
			for _, name := range diffResult.Added {
				changedComponents[name] = true
			}
			for _, name := range diffResult.Modified {
				changedComponents[name] = true
			}
			for _, name := range diffResult.Removed {
				changedComponents[name] = true
			}
		case git.IntentDiffNone:
			// formatting/comment-only change — no components from intent
		}
	}

	intentDir := filepathDir(intentPath)
	// When intentPath is absolute (e.g. from auto-discovery), convert intentDir
	// to a CWD-relative path so it matches git diff output which is always relative.
	if filepath.IsAbs(intentDir) {
		if cwd, err := os.Getwd(); err == nil {
			if rel, err := filepath.Rel(cwd, intentDir); err == nil {
				intentDir = filepath.ToSlash(rel)
			}
		}
	}

	for _, comp := range normalized.Components {
		if isFileChanged(changedFiles, joinPath(intentDir, comp.SourcePath)) {
			changedComponents[comp.Name] = true
			continue
		}

		for _, envInstances := range instances {
			for _, inst := range envInstances {
				instPath := joinPath(intentDir, inst.Path)
				if inst.ComponentName == comp.Name && inst.Path != "" && inst.Path != "./" && isPathChanged(changedFiles, instPath) {
					changedComponents[comp.Name] = true
					break
				}
			}
			if changedComponents[comp.Name] {
				break
			}
		}
	}

	return changedComponents
}

func instancesFromMergedComponents(components []*expand.ComponentMerged) map[string][]*model.ComponentInstance {
	instances := make(map[string][]*model.ComponentInstance)
	for _, comp := range components {
		for _, inst := range comp.Instances {
			instances[inst.Environment] = append(instances[inst.Environment], inst)
		}
	}
	return instances
}

func isIntentPathChanged(changedFiles map[string]struct{}, intentPath string) bool {
	if isFileChanged(changedFiles, intentPath) {
		return true
	}

	normalizedIntent := strings.TrimPrefix(normalizeFilePath(intentPath), "./")
	if normalizedIntent == "" {
		return false
	}

	base := filepathBase(normalizedIntent)
	for file := range changedFiles {
		normalizedFile := strings.TrimPrefix(normalizeFilePath(file), "./")
		if normalizedFile == base || strings.HasSuffix(normalizedFile, "/"+base) {
			return true
		}
	}

	return false
}

func isFileChanged(changedFiles map[string]struct{}, targetPath string) bool {
	if targetPath == "" {
		return false
	}

	normalizedTarget := strings.TrimPrefix(normalizeFilePath(targetPath), "./")
	for file := range changedFiles {
		normalizedFile := strings.TrimPrefix(normalizeFilePath(file), "./")
		if normalizedFile == normalizedTarget {
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

func filepathDir(path string) string {
	parts := strings.Split(normalizeFilePath(path), "/")
	if len(parts) <= 1 {
		return "."
	}
	return strings.Join(parts[:len(parts)-1], "/")
}

func joinPath(dir, path string) string {
	if dir == "" || dir == "." {
		return path
	}
	if path == "" {
		return dir
	}
	return dir + "/" + path
}

func normalizeFilePath(path string) string {
	return strings.TrimSuffix(strings.ReplaceAll(path, "\\", "/"), "/")
}

func parseCommaSeparated(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func matchesAnyFilter(value string, filters []string) bool {
	for _, f := range filters {
		if f == value {
			return true
		}
		if strings.HasSuffix(f, "*") && strings.HasPrefix(value, strings.TrimSuffix(f, "*")) {
			return true
		}
	}
	return false
}

func formatEnvNotFoundError(filter string, available []string) error {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("no environment matched %q", filter))

	if suggestion := ui.SuggestMatch(filter, available); suggestion != "" {
		sb.WriteString(fmt.Sprintf("\n\ndid you mean:\n  %s", suggestion))
	}

	if len(available) > 0 {
		sb.WriteString("\n\navailable environments:\n")
		for _, env := range available {
			sb.WriteString(fmt.Sprintf("  %s\n", env))
		}
	}

	return fmt.Errorf("%s", sb.String())
}

func formatComponentNotFoundError(filter string, available []string) error {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("no component matched %q", filter))

	if suggestion := ui.SuggestMatch(filter, available); suggestion != "" {
		sb.WriteString(fmt.Sprintf("\n\ndid you mean:\n  %s", suggestion))
	}

	if len(available) > 0 {
		sb.WriteString("\n\navailable components:\n")
		for _, comp := range available {
			sb.WriteString(fmt.Sprintf("  %s\n", comp))
		}
	}

	return fmt.Errorf("%s", sb.String())
}

func buildTriggerContext() (model.TriggerContext, error) {
	if triggerName != "" && eventFile != "" {
		return model.TriggerContext{}, fmt.Errorf("--trigger and --event-file are mutually exclusive")
	}

	if triggerName != "" {
		return model.TriggerContext{
			Mode:        "named-trigger",
			TriggerName: triggerName,
		}, nil
	}

	if eventFile != "" {
		if fromCI == "" {
			return model.TriggerContext{}, fmt.Errorf("--from-ci is required when using --event-file")
		}

		data, err := os.ReadFile(eventFile)
		if err != nil {
			return model.TriggerContext{}, fmt.Errorf("failed to read event file %s: %w", eventFile, err)
		}

		eventName := os.Getenv("GITHUB_EVENT_NAME")
		var event *model.NormalizedEvent
		if eventName != "" {
			event, err = trigger.ParseEventFileWithName(fromCI, eventName, data)
		} else {
			event, err = trigger.ParseEventFile(fromCI, data)
		}
		if err != nil {
			return model.TriggerContext{}, err
		}

		return model.TriggerContext{
			Mode:  "event-file",
			Event: event,
		}, nil
	}

	return model.TriggerContext{Mode: "none"}, nil
}
