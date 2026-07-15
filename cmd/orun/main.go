package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/artifactstore/github"
	"github.com/sourceplane/orun/internal/ci"
	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/expand"
	"github.com/sourceplane/orun/internal/git"
	"github.com/sourceplane/orun/internal/loader"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/normalize"
	"github.com/sourceplane/orun/internal/objrun"
	"github.com/sourceplane/orun/internal/planner"
	"github.com/sourceplane/orun/internal/preset"
	"github.com/sourceplane/orun/internal/render"
	"github.com/sourceplane/orun/internal/revkey"
	"github.com/sourceplane/orun/internal/runbundle"
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
			// A CI event that maps to no configured trigger binding (e.g. a
			// pull request whose base branch is not bound) is not a failure:
			// there is simply nothing to plan. Emit an empty matrix so the
			// workflow's matrix guard skips the execute job, and exit cleanly
			// rather than failing the pipeline.
			if triggerCtx.Mode == "event-file" && errors.Is(err, trigger.ErrNoTriggerMatch) {
				fmt.Fprintf(os.Stderr, "○ %v — nothing to plan\n", err)
				writeEmptyGithubMatrix()
				return nil
			}
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

		// The unified change-detection engine over the full object-model catalog
		// is the single --changed selection path (catalog-state CS5). It
		// refreshes-if-needed, then selects DirectlyChanged ∪ the include:always
		// forward closure — the selection the CS8 parity gate locks against the
		// retired legacy selector.
		includedComps, eerr := engineChangedSelection(context.Background(), changeOptions)
		if eerr != nil {
			return fmt.Errorf("failed to compute changed components: %w", eerr)
		}

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

	// Record the environment/component selection that scoped this plan
	// (env-scoping "Z" model), including any dependency edges pruned because
	// their endpoint was filtered out of this scoped plan.
	planScoped := environment != "" || len(planComponents) > 0 || changedOnly || len(triggerActiveEnvs) > 0
	planSelection := computePlanSelection(instances, planScoped, allEnvs)
	planSelection.PrunedEdges = computePrunedEdges(instances, normalized.Environments)
	warnPrunedEdges(planSelection.PrunedEdges)

	if debugMode {
		fmt.Println("□ Binding jobs and resolving dependencies...")
	}
	jobPlanner := planner.NewJobPlanner(compositionInfos)
	// Tenancy scope for mapping composition secretBindings → secret:// references
	// (orun-secrets SEC4). project defaults to the repo (OV2 bijection) when the
	// intent does not override it.
	bindWorkspace, bindProject, _ := intentScope(intent)
	if bindProject == "" {
		if wsRoot, werr := catalogWorkspaceRoot(); werr == nil {
			bindProject = shortRepoName("", wsRoot)
		}
	}
	jobPlanner.Workspace = bindWorkspace
	jobPlanner.Project = bindProject
	// `workflow:` step references resolve (and are digest-pinned) against the
	// intent file's directory (orun-workflows §5).
	jobPlanner.WorkflowBaseDir = filepath.Dir(intentFile)
	jobInstances, err := jobPlanner.PlanJobs(instances)
	if err != nil {
		return fmt.Errorf("failed to plan jobs: %w", err)
	}

	// Resolve environment promotion dependencies into in-plan DAG edges (the
	// enforced mechanism) or advisory cross-plan gates.
	if err := planner.ResolvePromotionDependencies(jobInstances, instances, normalized.Environments); err != nil {
		return fmt.Errorf("failed to resolve promotion dependencies: %w", err)
	}
	// Surface any recorded-but-not-enforced cross-plan promotion gates so users
	// don't assume cross-pipeline gating is active (env-scoping ES3).
	noticePromotionGates(jobInstances)

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
	plan.Metadata.Selection = planSelection

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
		NoRefresh: planNoCatalogRefresh,
		Strict:    planCatalogStrict,
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

	// Derive the static x-orun-secrets facet (requirements) onto the resolved
	// component entities before the object-model catalog is persisted
	// (orun-secrets SEC4). No-op unless a composition declares secretBindings.
	enrichCatalogSecretsFacet(catRes.View, compositionRegistry)

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
	revKey, err := revkey.RevisionKey(trig, planHash)
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

	// Plans are written to the content-addressed object model only
	// (writeObjectModelPlan, below); the legacy revision-layout write was
	// retired (specs/orun-legacy-retirement 1D).
	planID := execmodel.PlanChecksumShort(plan)
	color := ui.ColorEnabledForWriter(os.Stdout)

	absStoreRoot, err := filepath.Abs(filepath.Join(storeDir(), ".orun"))
	if err != nil {
		return fmt.Errorf("resolve store root: %w", err)
	}

	// `-o/--output` writes a copy to the user-specified path (cli-surface.md §1.1).
	if outputFile != "" {
		if err := renderer.WritePlan(plan, outputFile); err != nil {
			return fmt.Errorf("failed to write plan to %s: %w", outputFile, err)
		}
	}

	// Write the content-addressed object graph. Best-effort and isolated under
	// .orun/objectmodel/.
	writeObjectModelPlan(absStoreRoot, plan, planBytes, planHash, revKey, trig, catRes)

	// On-success summary block (cli-surface.md §1.1). Printed before the
	// "components × envs → jobs" detail line so existing tooling that scans for
	// that line keeps working.
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
	fmt.Printf("  Revision: %s\n", revKey)
	fmt.Printf("  Trigger:  %s / %s / %s\n", trig.TriggerName, triggerScope, headRev)
	fmt.Printf("  Jobs:     %d\n", len(plan.Jobs))
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

// writeEmptyGithubMatrix writes an empty job matrix to $GITHUB_OUTPUT when
// --github-output is set, so a no-op plan (no trigger matched) still produces a
// well-formed `matrix` output. The workflow's execute job guards on
// matrix != '{"include":[]}', so an empty matrix cleanly skips downstream jobs.
func writeEmptyGithubMatrix() {
	if !githubOutput {
		return
	}
	outputPath := os.Getenv("GITHUB_OUTPUT")
	if outputPath == "" {
		return
	}
	lines := []string{
		"matrix<<EOF\n{\"include\":[]}\nEOF",
		"plan_id=",
		"exec_id=",
	}
	f, err := os.OpenFile(outputPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠ warning: failed to open %s: %v\n", outputPath, err)
		return
	}
	defer f.Close()
	if _, err := f.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		fmt.Fprintf(os.Stderr, "⚠ warning: failed to write %s: %v\n", outputPath, err)
	}
}

// computePlanHashForRevision returns the canonical "sha256:<hex>" digest of
// the plan's content with self-referential metadata (checksum, revision)
// cleared. This matches data-model.md §3.1: the plan hash is stable across
// re-runs of the same intent on the same SHA, irrespective of which alias
// it was previously persisted under. It delegates to objrun so the run path
// (CLI and TUI) and `orun plan` share one dedup-key implementation.
func computePlanHashForRevision(plan *model.Plan) (string, error) {
	return objrun.PlanHash(plan)
}

// canonicalPlanJSON marshals plan as deterministic indented JSON suitable
// for persistence as canonical plan.json (and for byte-identical compat
// aliases under .orun/plans/).
func canonicalPlanJSON(plan *model.Plan) ([]byte, error) {
	return objrun.CanonicalPlanJSON(plan)
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
		changeOptions, err := buildChangeOptions()
		if err != nil {
			return err
		}

		// Same engine path as plan/run --changed (catalog-state CS5): the object
		// catalog is the single source of the changed-component selection.
		changedComps, err = engineChangedSelection(context.Background(), changeOptions)
		if err != nil {
			return fmt.Errorf("failed to compute changed components: %w", err)
		}

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
