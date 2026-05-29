package services

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/expand"
	"github.com/sourceplane/orun/internal/git"
	"github.com/sourceplane/orun/internal/loader"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/normalize"
	"github.com/sourceplane/orun/internal/planner"
	"github.com/sourceplane/orun/internal/preset"
	"github.com/sourceplane/orun/internal/render"
	"github.com/sourceplane/orun/internal/state"
	"github.com/sourceplane/orun/internal/trigger"
)

// GeneratePlan compiles a plan from the current intent by calling Orun
// planning internals directly. It mirrors the CLI's generatePlan() path
// (cmd/orun/main.go) but never shells out, never writes the composition
// lock file, and never emits stdout — all results land in PlanResult.
//
// The implementation honours context cancellation at each meaningful
// stage boundary and is safe to call from a tea.Cmd goroutine.
//
// ChangedOnly support: only the explicit Base/Head/files path is wired
// (no CI auto-detection, no semantic intent diff, no --uncommitted /
// --untracked). The missing seam is documented in
// ai/proposals/task-0146-changed-plan-service-seam.md.
func (s *LiveOrunService) GeneratePlan(ctx context.Context, req PlanRequest) (*PlanResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	intentFile := firstNonEmpty(req.IntentFile, s.cfg.IntentFile)
	if intentFile == "" {
		return nil, errors.New("GeneratePlan: no intent file configured")
	}
	configDir := firstNonEmpty(req.ConfigDir, s.cfg.ConfigDir)

	// --- load intent ---
	intent, _, err := loader.LoadResolvedIntent(intentFile)
	if err != nil {
		return nil, fmt.Errorf("load intent: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// --- resolve compositions ---
	compositionRegistry, err := loader.LoadCompositionsForIntent(intent, intentFile, configDir)
	if err != nil {
		return nil, fmt.Errorf("resolve compositions: %w", err)
	}

	// --- resolve intent presets ---
	if len(intent.Extends) > 0 {
		if err := preset.ValidateExtendsRefs(intent); err != nil {
			return nil, err
		}
		resolvedPresets, err := preset.LoadPresetsForIntent(intent, compositionRegistry.SourceRoots)
		if err != nil {
			return nil, fmt.Errorf("load intent presets: %w", err)
		}
		mergeResult, err := preset.MergePresets(intent, resolvedPresets)
		if err != nil {
			return nil, fmt.Errorf("merge intent presets: %w", err)
		}
		intent = mergeResult.Intent
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// --- composition info map for the planner ---
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

	// --- normalize ---
	normalized, err := normalize.NormalizeIntent(intent)
	if err != nil {
		return nil, fmt.Errorf("normalize intent: %w", err)
	}
	if err := compositionRegistry.ValidateAllComponents(normalized); err != nil {
		return nil, fmt.Errorf("component validation: %w", err)
	}
	if errs := trigger.ValidateProfileRules(intent); len(errs) > 0 {
		return nil, trigger.FormatErrors(errs)
	}
	if errs := trigger.ValidateDependencyRules(intent); len(errs) > 0 {
		return nil, trigger.FormatErrors(errs)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// --- trigger resolution ---
	var (
		triggerCtx        model.TriggerContext
		triggerResolution *model.TriggerResolution
		triggerActiveEnvs []string
	)
	if req.TriggerName != "" {
		triggerCtx = model.TriggerContext{
			Mode:        "named-trigger",
			TriggerName: req.TriggerName,
		}
		if err := trigger.ValidateTriggerContext(intent, triggerCtx); err != nil {
			return nil, err
		}
		triggerActiveEnvs, triggerResolution, err = trigger.ResolveActiveEnvironments(intent, triggerCtx, req.Environment)
		if err != nil {
			return nil, err
		}
	} else {
		triggerCtx = model.TriggerContext{Mode: "none"}
	}

	// --- expand ---
	expander := expand.NewExpander(normalized).WithRegistry(compositionRegistry)
	if triggerResolution != nil {
		expander = expander.WithMatchedTriggers(triggerResolution.MatchedTriggerNames)
	}
	instances, err := expander.Expand()
	if err != nil {
		return nil, fmt.Errorf("expand intent: %w", err)
	}

	// --- env filter ---
	if len(triggerActiveEnvs) > 0 {
		envSet := stringSet(triggerActiveEnvs)
		instances = filterInstancesByEnv(instances, envSet)
		if len(instances) == 0 {
			return nil, fmt.Errorf("trigger-activated environments %v produced no component instances", triggerActiveEnvs)
		}
	} else if req.Environment != "" {
		envFilters := parseCSV(req.Environment)
		instances = filterInstancesByEnv(instances, stringSet(envFilters))
		if len(instances) == 0 {
			return nil, fmt.Errorf("no environment matched %q", req.Environment)
		}
	}

	// --- component filter ---
	if len(req.Components) > 0 {
		compSet := stringSet(req.Components)
		for envName, envInsts := range instances {
			filtered := make([]*model.ComponentInstance, 0, len(envInsts))
			for _, inst := range envInsts {
				if _, ok := compSet[inst.ComponentName]; ok {
					filtered = append(filtered, inst)
				}
			}
			if len(filtered) == 0 {
				delete(instances, envName)
			} else {
				instances[envName] = filtered
			}
		}
		if len(instances) == 0 {
			return nil, fmt.Errorf("no components matched %v", req.Components)
		}
	}

	// --- changed-only filter (safe subset) ---
	var warnings []string
	if req.ChangedOnly {
		base := req.BaseBranch
		head := req.HeadRef
		options := git.ChangeOptions{Base: base, Head: head}
		if err := git.ValidateOptions(options); err != nil {
			return nil, fmt.Errorf("change options: %w", err)
		}
		changed, err := changedFilesFromGit(options)
		if err != nil {
			return nil, fmt.Errorf("detect changed files: %w", err)
		}
		changedComps := changedComponentsFromFiles(normalized, instances, changed, intentFile)
		resolver := expand.NewDependencyResolver(normalized)
		included := resolver.ResolveComponentSet(changedComps)
		for envName := range instances {
			filtered := make([]*model.ComponentInstance, 0)
			for _, inst := range instances[envName] {
				if included[inst.ComponentName] {
					filtered = append(filtered, inst)
				}
			}
			instances[envName] = filtered
		}
		warnings = append(warnings,
			"changed-only used the safe TUI subset (no CI auto-detect, no semantic intent diff, no --uncommitted/--untracked)")
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// --- plan jobs + promotion deps + DAG sort ---
	jobPlanner := planner.NewJobPlanner(compositionInfos)
	jobInstances, err := jobPlanner.PlanJobs(instances)
	if err != nil {
		return nil, fmt.Errorf("plan jobs: %w", err)
	}
	if err := planner.ResolvePromotionDependencies(jobInstances, instances, normalized.Environments); err != nil {
		return nil, fmt.Errorf("resolve promotion deps: %w", err)
	}

	dag := planner.NewJobGraph(jobInstances)
	if err := dag.DetectCycles(); err != nil {
		return nil, fmt.Errorf("cycle detection: %w", err)
	}
	sorted, err := dag.TopologicalSort()
	if err != nil {
		return nil, fmt.Errorf("topological sort: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// --- render ---
	jobBindings := make(map[string]string)
	for typeName, composition := range compositionRegistry.Types {
		if composition.JobRegistryName != "" {
			jobBindings[typeName] = composition.JobRegistryName
		}
	}
	renderer := render.NewRenderer()
	plan := renderer.RenderPlanWithOrder(intent.Metadata, jobInstances, jobBindings, sorted)
	plan.Spec.CompositionSources = compositionRegistry.Sources

	if triggerResolution != nil {
		plan.Metadata.Trigger = &model.PlanTrigger{
			Mode:               triggerCtx.Mode,
			MatchedBindings:    triggerResolution.MatchedTriggerNames,
			ActiveEnvironments: triggerResolution.ActiveEnvironments,
			Scope:              triggerResolution.PlanScope,
			Base:               triggerResolution.Base,
			Head:               triggerResolution.Head,
		}
	}

	// --- compute checksum + persist if requested ---
	checksum := state.PlanChecksumShort(plan)
	plan.Metadata.Checksum = checksum

	if req.NamedPlan != "" {
		if s.cfg.Store != nil {
			if err := s.cfg.Store.SavePlan(plan, req.NamedPlan); err != nil {
				return nil, fmt.Errorf("save named plan %q: %w", req.NamedPlan, err)
			}
		} else {
			warnings = append(warnings,
				fmt.Sprintf("NamedPlan %q ignored: no state store configured", req.NamedPlan))
		}
	}

	// --- summary metadata ---
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

	return &PlanResult{
		Plan:        plan,
		Checksum:    checksum,
		JobCount:    len(plan.Jobs),
		Components:  compNames,
		Warnings:    warnings,
		GeneratedAt: time.Now(),
	}, nil
}

// --- helpers ---

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func parseCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, v := range values {
		set[v] = struct{}{}
	}
	return set
}

func filterInstancesByEnv(instances map[string][]*model.ComponentInstance, envSet map[string]struct{}) map[string][]*model.ComponentInstance {
	filtered := make(map[string][]*model.ComponentInstance, len(envSet))
	for envName, envInsts := range instances {
		if _, ok := envSet[envName]; ok {
			filtered[envName] = envInsts
		}
	}
	return filtered
}

func changedFilesFromGit(options git.ChangeOptions) (map[string]struct{}, error) {
	detector := git.NewChangeDetectorWithOptions(options)
	files, err := detector.GetChangedFiles()
	if err != nil {
		return nil, err
	}
	result := make(map[string]struct{}, len(files))
	for _, f := range files {
		if f == "" {
			continue
		}
		result[f] = struct{}{}
	}
	return result, nil
}

// changedComponentsFromFiles is a safe subset of the CLI's collectChangedComponents:
// it matches component SourcePath / instance Path against the changed-files set
// using the intent file's directory as the root. It does NOT semantically diff
// intent.yaml (Phase 3 follow-up).
func changedComponentsFromFiles(
	normalized *model.NormalizedIntent,
	instances map[string][]*model.ComponentInstance,
	changedFiles map[string]struct{},
	intentPath string,
) map[string]bool {
	out := make(map[string]bool)
	if normalized == nil {
		return out
	}
	intentDir := dirPath(intentPath)

	for _, comp := range normalized.Components {
		if isPathInChanged(changedFiles, joinRel(intentDir, comp.SourcePath)) {
			out[comp.Name] = true
			continue
		}
		for _, envInstances := range instances {
			for _, inst := range envInstances {
				if inst.ComponentName != comp.Name || inst.Path == "" || inst.Path == "./" {
					continue
				}
				if isPathInChanged(changedFiles, joinRel(intentDir, inst.Path)) {
					out[comp.Name] = true
					break
				}
			}
			if out[comp.Name] {
				break
			}
		}
	}
	return out
}

func dirPath(path string) string {
	normalized := strings.ReplaceAll(path, "\\", "/")
	idx := strings.LastIndex(normalized, "/")
	if idx < 0 {
		return "."
	}
	return normalized[:idx]
}

func joinRel(dir, path string) string {
	if dir == "" || dir == "." {
		return path
	}
	if path == "" {
		return dir
	}
	return dir + "/" + path
}

func isPathInChanged(changed map[string]struct{}, target string) bool {
	if target == "" {
		return false
	}
	target = strings.TrimPrefix(strings.TrimSuffix(strings.ReplaceAll(target, "\\", "/"), "/"), "./")
	for f := range changed {
		normalized := strings.TrimPrefix(strings.TrimSuffix(strings.ReplaceAll(f, "\\", "/"), "/"), "./")
		if normalized == target || strings.HasPrefix(normalized, target+"/") {
			return true
		}
	}
	return false
}
