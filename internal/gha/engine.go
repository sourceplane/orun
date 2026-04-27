package gha

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	actexpr "github.com/nektos/act/pkg/exprparser"
	actmodel "github.com/nektos/act/pkg/model"
	"github.com/sourceplane/gluon/internal/model"
)

const (
	containerWorkspacePath = "/github/workspace"
	containerWorkflowPath  = "/github/workflow"
	containerActionPath    = "/github/action"
	containerToolCachePath = "/github/toolcache"
)

type Options struct {
	CacheDir     string
	ToolCacheDir string
	HTTPClient   *http.Client
}

type ExecContext struct {
	Context      context.Context
	WorkspaceDir string
	WorkDir      string
	BaseEnv      map[string]string
	JobEnv       map[string]string
	StepEnv      map[string]string
	Env          map[string]string
}

type Engine struct {
	cacheDir     string
	toolCacheDir string
	httpClient   *http.Client

	mu           sync.Mutex
	jobs         map[string]*jobState
	pulledImages map[string]struct{}
	builtImages  map[string]string
}

type jobState struct {
	id              string
	workspaceDir    string
	workDir         string
	tempDir         string
	fileCommandsDir string
	toolCacheDir    string
	globalEnv       map[string]string
	extraPath       []string
	stepResults     map[string]*actmodel.StepResult
	github          *actmodel.GithubContext
	runnerContext   map[string]interface{}
	secrets         map[string]string
	vars            map[string]string
	needs           map[string]actexpr.Needs
	posts           []*postAction
	actionNames     map[string]int
	masks           []string
	summary         strings.Builder
	basePath        string
}

type scope struct {
	job              *jobState
	baseActionDir    string
	workDir          string
	steps            map[string]*actmodel.StepResult
	inputs           map[string]interface{}
	actionRepository string
	actionRef        string
	actionPath       string
}

type actionInvocation struct {
	stepID      string
	displayName string
	actionName  string
	reference   ActionReference
	actionDir   string
	metadata    *ActionMetadata
	inputs      map[string]interface{}
	state       map[string]string
	workDir     string
}

type postAction struct {
	ifExpression string
	scope        *scope
	invocation   *actionInvocation
}

type stepSpec struct {
	ID               string
	Name             string
	Run              string
	Use              string
	With             map[string]interface{}
	Env              map[string]interface{}
	Shell            string
	WorkingDirectory string
	If               string
	ContinueOnError  bool
}

type stepExecutionResult struct {
	Output  string
	Outputs map[string]string
	State   map[string]string
}

type workflowCommandResult struct {
	Env     map[string]string
	Outputs map[string]string
	State   map[string]string
	Paths   []string
	Masks   []string
	Output  string
}

var actionNamePattern = regexp.MustCompile(`[^A-Za-z0-9_]+`)

func NewEngine(options Options) *Engine {
	cacheDir := strings.TrimSpace(options.CacheDir)
	if cacheDir == "" {
		cacheDir = defaultRootDir("actions")
	}
	toolCacheDir := strings.TrimSpace(options.ToolCacheDir)
	if toolCacheDir == "" {
		toolCacheDir = defaultRootDir("tool-cache")
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Engine{
		cacheDir:     cacheDir,
		toolCacheDir: toolCacheDir,
		httpClient:   httpClient,
		jobs:         map[string]*jobState{},
		pulledImages: map[string]struct{}{},
		builtImages:  map[string]string{},
	}
}

func (e *Engine) Prepare(ctx ExecContext) error {
	for _, dirPath := range []string{e.cacheDir, e.toolCacheDir} {
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			return fmt.Errorf("create gha directory %s: %w", dirPath, err)
		}
	}
	return nil
}

func (e *Engine) Cleanup(ctx ExecContext) error {
	return nil
}

func (e *Engine) RunStep(ctx ExecContext, job model.PlanJob, step model.PlanStep) (string, error) {
	state, err := e.ensureJobState(ctx, job)
	if err != nil {
		return "", err
	}

	result, err := e.runStep(ctx, state, state.rootScope(ctx.WorkDir), planStepToSpec(step), nil)
	return strings.TrimRight(result.Output, "\n"), err
}

func (e *Engine) FinalizeJob(ctx ExecContext, job model.PlanJob) (string, error) {
	state := e.getJobState(job.ID)
	if state == nil {
		return "", nil
	}
	defer e.deleteJobState(job.ID)
	defer os.RemoveAll(state.tempDir)

	outputs := make([]string, 0, len(state.posts))
	errs := make([]error, 0)
	for index := len(state.posts) - 1; index >= 0; index-- {
		output, err := e.runPostAction(ctx, state, state.posts[index])
		if strings.TrimSpace(output) != "" {
			outputs = append(outputs, output)
		}
		if err != nil {
			errs = append(errs, err)
		}
	}

	return strings.Join(outputs, "\n"), errors.Join(errs...)
}

func (e *Engine) ensureJobState(ctx ExecContext, job model.PlanJob) (*jobState, error) {
	if state := e.getJobState(job.ID); state != nil {
		return state, nil
	}

	state, err := e.newJobState(ctx, job)
	if err != nil {
		return nil, err
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if current := e.jobs[job.ID]; current != nil {
		_ = os.RemoveAll(state.tempDir)
		return current, nil
	}
	e.jobs[job.ID] = state
	return state, nil
}

func (e *Engine) getJobState(jobID string) *jobState {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.jobs[jobID]
}

func (e *Engine) deleteJobState(jobID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.jobs, jobID)
}

func (e *Engine) newJobState(ctx ExecContext, job model.PlanJob) (*jobState, error) {
	workspaceDir := strings.TrimSpace(ctx.WorkspaceDir)
	if workspaceDir == "" {
		workspaceDir = "."
	}
	resolvedWorkspace, err := filepath.Abs(workspaceDir)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace %s: %w", workspaceDir, err)
	}

	tempDir, err := os.MkdirTemp("", "gluon-gha-"+sanitizePathComponent(job.ID)+"-")
	if err != nil {
		return nil, fmt.Errorf("create gha temp directory: %w", err)
	}
	fileCommandsDir := filepath.Join(tempDir, "file_commands")
	if err := os.MkdirAll(fileCommandsDir, 0755); err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("create file-command directory: %w", err)
	}

	homeDir := filepath.Join(tempDir, "home")
	if err := os.MkdirAll(homeDir, 0755); err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("create job home directory: %w", err)
	}

	baseEnv := copyStringMap(ctx.BaseEnv)
	repository, owner := detectRepository(ctx.Context, resolvedWorkspace)
	ref := firstNonEmpty(baseEnv["GITHUB_REF"], detectRef(ctx.Context, resolvedWorkspace), "refs/heads/main")
	sha := firstNonEmpty(baseEnv["GITHUB_SHA"], detectSHA(ctx.Context, resolvedWorkspace))
	refName, refType := deriveRefParts(ref)
	eventPath, eventPayload, err := ensureEventFile(tempDir, baseEnv["GITHUB_EVENT_PATH"], repository, owner, ref, sha, refName)
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, err
	}

	githubContext := &actmodel.GithubContext{
		Event:            eventPayload,
		EventPath:        eventPath,
		Workflow:         firstNonEmpty(baseEnv["GITHUB_WORKFLOW"], "gluon"),
		RunAttempt:       firstNonEmpty(baseEnv["GITHUB_RUN_ATTEMPT"], "1"),
		RunID:            firstNonEmpty(baseEnv["GITHUB_RUN_ID"], strconv.FormatInt(time.Now().UnixNano(), 10)),
		RunNumber:        firstNonEmpty(baseEnv["GITHUB_RUN_NUMBER"], "1"),
		Actor:            firstNonEmpty(baseEnv["GITHUB_ACTOR"], os.Getenv("USER"), "gluon"),
		Repository:       firstNonEmpty(baseEnv["GITHUB_REPOSITORY"], repository),
		EventName:        firstNonEmpty(baseEnv["GITHUB_EVENT_NAME"], "workflow_dispatch"),
		Sha:              sha,
		Ref:              ref,
		RefName:          firstNonEmpty(baseEnv["GITHUB_REF_NAME"], refName),
		RefType:          firstNonEmpty(baseEnv["GITHUB_REF_TYPE"], refType),
		HeadRef:          baseEnv["GITHUB_HEAD_REF"],
		BaseRef:          baseEnv["GITHUB_BASE_REF"],
		Token:            baseEnv["GITHUB_TOKEN"],
		Workspace:        firstNonEmpty(baseEnv["GITHUB_WORKSPACE"], resolvedWorkspace),
		Action:           "",
		ActionPath:       "",
		ActionRepository: "",
		ActionRef:        "",
		Job:              job.ID,
		JobName:          job.ID,
		RepositoryOwner:  firstNonEmpty(baseEnv["GITHUB_REPOSITORY_OWNER"], owner),
		RetentionDays:    firstNonEmpty(baseEnv["GITHUB_RETENTION_DAYS"], "0"),
		RunnerPerflog:    firstNonEmpty(baseEnv["RUNNER_PERFLOG"], "/dev/null"),
		RunnerTrackingID: baseEnv["RUNNER_TRACKING_ID"],
		ServerURL:        firstNonEmpty(baseEnv["GITHUB_SERVER_URL"], "https://github.com"),
		APIURL:           firstNonEmpty(baseEnv["GITHUB_API_URL"], "https://api.github.com"),
		GraphQLURL:       firstNonEmpty(baseEnv["GITHUB_GRAPHQL_URL"], "https://api.github.com/graphql"),
	}

	runnerContext := map[string]interface{}{
		"os":         runnerOS(),
		"arch":       runnerArch(),
		"temp":       tempDir,
		"tool_cache": e.toolCacheDir,
		"name":       "gluon",
	}

	globalEnv := copyStringMap(baseEnv)
	for key, value := range githubEnv(githubContext) {
		globalEnv[key] = value
	}
	globalEnv["RUNNER_TEMP"] = tempDir
	globalEnv["RUNNER_TOOL_CACHE"] = e.toolCacheDir
	globalEnv["RUNNER_OS"] = runnerOS()
	globalEnv["RUNNER_ARCH"] = runnerArch()
	if runtime.GOOS == "windows" {
		globalEnv["USERPROFILE"] = homeDir
		globalEnv["HOMEPATH"] = homeDir
	} else {
		globalEnv["HOME"] = homeDir
	}
	globalEnv["CI"] = "true"
	globalEnv["GITHUB_ACTIONS"] = "true"
	globalEnv["ACTIONS_RUNTIME_URL"] = firstNonEmpty(globalEnv["ACTIONS_RUNTIME_URL"], "https://gluon.invalid/runtime")
	globalEnv["ACTIONS_RESULTS_URL"] = firstNonEmpty(globalEnv["ACTIONS_RESULTS_URL"], globalEnv["ACTIONS_RUNTIME_URL"])
	if globalEnv["ACTIONS_RUNTIME_TOKEN"] == "" {
		token, tokenErr := randomToken(16)
		if tokenErr != nil {
			_ = os.RemoveAll(tempDir)
			return nil, tokenErr
		}
		globalEnv["ACTIONS_RUNTIME_TOKEN"] = token
	}

	state := &jobState{
		id:              job.ID,
		workspaceDir:    resolvedWorkspace,
		workDir:         firstNonEmpty(ctx.WorkDir, resolvedWorkspace),
		tempDir:         tempDir,
		fileCommandsDir: fileCommandsDir,
		toolCacheDir:    e.toolCacheDir,
		globalEnv:       globalEnv,
		stepResults:     map[string]*actmodel.StepResult{},
		github:          githubContext,
		runnerContext:   runnerContext,
		secrets:         buildSecrets(globalEnv),
		vars:            map[string]string{},
		needs:           map[string]actexpr.Needs{},
		posts:           []*postAction{},
		actionNames:     map[string]int{},
		basePath:        globalEnv["PATH"],
	}

	jobEvaluator := state.evaluator(state.rootScope(state.workDir), globalEnv, githubContext)
	for key, value := range interpolateStringMap(ctx.JobEnv, jobEvaluator) {
		state.globalEnv[key] = value
	}
	if state.globalEnv["PATH"] != "" {
		state.basePath = state.globalEnv["PATH"]
	}

	return state, nil
}

func (state *jobState) rootScope(workDir string) *scope {
	return &scope{
		job:           state,
		baseActionDir: state.workspaceDir,
		workDir:       firstNonEmpty(workDir, state.workDir, state.workspaceDir),
		steps:         state.stepResults,
		inputs:        nil,
	}
}

func (current *scope) cloneWithAction(actionDir string, repository string, ref string, inputs map[string]interface{}, workDir string) *scope {
	return &scope{
		job:              current.job,
		baseActionDir:    current.baseActionDir,
		workDir:          firstNonEmpty(workDir, current.workDir),
		steps:            current.steps,
		inputs:           inputs,
		actionRepository: repository,
		actionRef:        ref,
		actionPath:       actionDir,
	}
}

func (current *scope) childCompositeScope(actionDir string, repository string, ref string, inputs map[string]interface{}, workDir string) *scope {
	return &scope{
		job:              current.job,
		baseActionDir:    actionDir,
		workDir:          firstNonEmpty(workDir, current.workDir),
		steps:            map[string]*actmodel.StepResult{},
		inputs:           inputs,
		actionRepository: repository,
		actionRef:        ref,
		actionPath:       actionDir,
	}
}

func (state *jobState) evaluator(scope *scope, env map[string]string, github *actmodel.GithubContext) *Evaluator {
	return NewEvaluator(EvaluationInput{
		Github:    github,
		Env:       env,
		JobStatus: scopeStatus(scope.steps),
		Steps:     scope.steps,
		Runner:    state.runnerContext,
		Secrets:   state.secrets,
		Vars:      state.vars,
		Inputs:    scope.inputs,
		Needs:     state.needs,
	})
}

func (state *jobState) githubForScope(scope *scope, actionName string) *actmodel.GithubContext {
	copy := *state.github
	copy.Action = actionName
	copy.ActionPath = scope.actionPath
	copy.ActionRepository = scope.actionRepository
	copy.ActionRef = scope.actionRef
	return &copy
}

func (state *jobState) nextActionName(base string) string {
	name := strings.TrimSpace(base)
	if name == "" {
		name = "action"
	}
	name = actionNamePattern.ReplaceAllString(name, "")
	if name == "" {
		name = "action"
	}
	count := state.actionNames[name]
	count++
	state.actionNames[name] = count
	if count == 1 {
		return name
	}
	return fmt.Sprintf("%s_%d", name, count)
}

func (e *Engine) runStep(ctx ExecContext, state *jobState, scope *scope, spec stepSpec, inheritedEnv map[string]string) (stepExecutionResult, error) {
	stepID := firstNonEmpty(strings.TrimSpace(spec.ID), strings.TrimSpace(spec.Name), strings.TrimSpace(spec.Use))
	if stepID == "" {
		stepID = "unnamed-step"
	}

	result := &actmodel.StepResult{
		Outputs:    map[string]string{},
		Outcome:    actmodel.StepStatusSuccess,
		Conclusion: actmodel.StepStatusSuccess,
	}
	scope.steps[stepID] = result

	if strings.TrimSpace(spec.If) != "" {
		githubContext := state.githubForScope(scope, state.nextActionName("eval"))
		ifEvaluator := state.evaluator(scope, mergeStringMaps(copyStringMap(state.globalEnv), inheritedEnv, githubEnv(githubContext)), githubContext)
		run, err := ifEvaluator.EvalBool(spec.If, actexpr.DefaultStatusCheckSuccess)
		if err != nil {
			result.Outcome = actmodel.StepStatusFailure
			result.Conclusion = actmodel.StepStatusFailure
			return stepExecutionResult{}, err
		}
		if !run {
			result.Outcome = actmodel.StepStatusSkipped
			result.Conclusion = actmodel.StepStatusSkipped
			return stepExecutionResult{}, nil
		}
	}

	var execution stepExecutionResult
	var err error
	if strings.TrimSpace(spec.Run) != "" {
		execution, err = e.executeRunStep(ctx, state, scope, spec, inheritedEnv)
	} else {
		execution, err = e.executeUsesStep(ctx, state, scope, spec, inheritedEnv)
	}

	if len(execution.Outputs) > 0 {
		result.Outputs = execution.Outputs
	}
	if err != nil {
		result.Outcome = actmodel.StepStatusFailure
		if spec.ContinueOnError {
			result.Conclusion = actmodel.StepStatusSuccess
			return execution, nil
		}
		result.Conclusion = actmodel.StepStatusFailure
		return execution, err
	}

	result.Outcome = actmodel.StepStatusSuccess
	result.Conclusion = actmodel.StepStatusSuccess
	return execution, nil
}

func (e *Engine) executeRunStep(ctx ExecContext, state *jobState, scope *scope, spec stepSpec, inheritedEnv map[string]string) (stepExecutionResult, error) {
	requireShell := scope.actionPath != ""
	actionName := state.nextActionName("__run")
	githubContext := state.githubForScope(scope, actionName)
	baseEnv := mergeStringMaps(copyStringMap(state.globalEnv), inheritedEnv, githubEnv(githubContext))
	evaluator := state.evaluator(scope, baseEnv, githubContext)
	stepEnv := interpolateInterfaceMap(spec.Env, evaluator)
	commandEnv := mergeStringMaps(copyStringMap(state.globalEnv), inheritedEnv, githubEnv(githubContext), stepEnv)
	applyExtraPath(commandEnv, state.extraPath)

	files, err := NewStepFiles(state.fileCommandsDir)
	if err != nil {
		return stepExecutionResult{}, err
	}
	commandEnv["GITHUB_ENV"] = files.HostEnv
	commandEnv["GITHUB_OUTPUT"] = files.HostOutput
	commandEnv["GITHUB_PATH"] = files.HostPath
	commandEnv["GITHUB_STATE"] = files.HostState
	commandEnv["GITHUB_STEP_SUMMARY"] = files.HostSummary

	workingDirectory := resolveWorkingDirectory(scope.workDir, spec.WorkingDirectory, evaluator)
	command, arguments, cleanup, err := prepareShellCommand(spec.Shell, evaluator.Interpolate(spec.Run), workingDirectory, state.tempDir, commandEnv, requireShell)
	if err != nil {
		return stepExecutionResult{}, err
	}
	defer cleanup()

	rawOutput, runErr := runProcess(firstNonEmptyContext(ctx.Context), command, arguments, workingDirectory, commandEnv)
	legacy := processWorkflowCommands(rawOutput, state.masks)
	state.masks = append(state.masks, legacy.Masks...)
	fileCommands, err := files.Parse()
	if err != nil {
		return stepExecutionResult{Output: legacy.Output, Outputs: legacy.Outputs, State: legacy.State}, err
	}

	state.applyCommandEnv(fileCommands.Env)
	state.applyCommandEnv(legacy.Env)
	state.extraPath = append(state.extraPath, fileCommands.Paths...)
	state.extraPath = append(state.extraPath, legacy.Paths...)
	state.summary.WriteString(fileCommands.Summary)

	outputs := mergeStringMaps(legacy.Outputs, fileCommands.Outputs)
	return stepExecutionResult{Output: legacy.Output, Outputs: outputs, State: mergeStringMaps(legacy.State, fileCommands.State)}, runErr
}

func (e *Engine) executeUsesStep(ctx ExecContext, state *jobState, scope *scope, spec stepSpec, inheritedEnv map[string]string) (stepExecutionResult, error) {
	baseEnv := mergeStringMaps(copyStringMap(state.globalEnv), inheritedEnv)
	useEvaluator := state.evaluator(scope, baseEnv, state.githubForScope(scope, state.nextActionName("resolve")))
	useValue := useEvaluator.Interpolate(spec.Use)

	resolved, err := e.resolveAction(firstNonEmptyContext(ctx.Context), scope.baseActionDir, state.github.APIURL, state.github.Token, useValue)
	if err != nil {
		return stepExecutionResult{}, err
	}

	if resolved.Reference.Kind == referenceKindDocker {
		return e.executeDockerURLStep(ctx, state, scope, spec, inheritedEnv, resolved.Reference)
	}

	actionScope := scope.cloneWithAction(resolved.ActionDir, resolved.Reference.Repository(), resolved.Reference.Ref, nil, scope.workDir)
	actionName := state.nextActionName(actionBaseName(spec, resolved.Reference))
	githubContext := state.githubForScope(actionScope, actionName)
	actionEnvBase := mergeStringMaps(copyStringMap(state.globalEnv), inheritedEnv, githubEnv(githubContext))
	actionEvaluator := state.evaluator(actionScope, actionEnvBase, githubContext)
	stepEnv := interpolateInterfaceMap(spec.Env, actionEvaluator)
	actionEnvBase = mergeStringMaps(actionEnvBase, stepEnv)
	actionEvaluator = state.evaluator(actionScope, actionEnvBase, githubContext)
	inputs, inputEnv, err := prepareInputs(spec.With, resolved.Metadata.Inputs, actionEvaluator)
	if err != nil {
		return stepExecutionResult{}, err
	}
	actionScope.inputs = inputs

	invocation := &actionInvocation{
		stepID:      firstNonEmpty(strings.TrimSpace(spec.ID), strings.TrimSpace(spec.Name), strings.TrimSpace(spec.Use), actionName),
		displayName: firstNonEmpty(strings.TrimSpace(spec.Name), strings.TrimSpace(spec.Use), actionName),
		actionName:  actionName,
		reference:   resolved.Reference,
		actionDir:   resolved.ActionDir,
		metadata:    resolved.Metadata,
		inputs:      inputs,
		state:       map[string]string{},
		workDir:     scope.workDir,
	}
	sharedEnv := mergeStringMaps(stepEnv, inputEnv)

	switch resolved.Metadata.Runs.Using {
	case "node12", "node16", "node20", "node24":
		return e.executeNodeAction(ctx, state, actionScope, invocation, sharedEnv)
	case "composite":
		return e.executeCompositeAction(ctx, state, scope, resolved, invocation, sharedEnv)
	case "docker":
		return e.executeDockerAction(ctx, state, actionScope, invocation, sharedEnv)
	default:
		return stepExecutionResult{}, fmt.Errorf("unsupported action runtime %q in %s", resolved.Metadata.Runs.Using, resolved.ActionDir)
	}
}

func (e *Engine) executeNodeAction(ctx ExecContext, state *jobState, scope *scope, invocation *actionInvocation, sharedEnv map[string]string) (stepExecutionResult, error) {
	if invocation.metadata.Runs.Post != "" {
		state.posts = append(state.posts, &postAction{ifExpression: invocation.metadata.Runs.PostIf, scope: scope, invocation: invocation})
	}

	if strings.TrimSpace(invocation.metadata.Runs.Pre) != "" {
		if _, _, _, err := e.runNodeStage(ctx, state, scope, invocation, sharedEnv, invocation.metadata.Runs.Pre); err != nil {
			return stepExecutionResult{}, err
		}
	}

	output, outputs, stateValues, err := e.runNodeStage(ctx, state, scope, invocation, sharedEnv, invocation.metadata.Runs.Main)
	for key, value := range stateValues {
		invocation.state[key] = value
	}
	return stepExecutionResult{Output: output, Outputs: outputs, State: stateValues}, err
}

func (e *Engine) runNodeStage(ctx ExecContext, state *jobState, scope *scope, invocation *actionInvocation, sharedEnv map[string]string, entry string) (string, map[string]string, map[string]string, error) {
	if strings.TrimSpace(entry) == "" {
		return "", nil, nil, fmt.Errorf("node action %s does not define an entrypoint", invocation.displayName)
	}

	githubContext := state.githubForScope(scope, invocation.actionName)
	env := mergeStringMaps(copyStringMap(state.globalEnv), sharedEnv, githubEnv(githubContext), inputEnv(invocation.inputs), stateEnv(invocation.state))
	applyExtraPath(env, state.extraPath)

	files, err := NewStepFiles(state.fileCommandsDir)
	if err != nil {
		return "", nil, nil, err
	}
	env["GITHUB_ENV"] = files.HostEnv
	env["GITHUB_OUTPUT"] = files.HostOutput
	env["GITHUB_PATH"] = files.HostPath
	env["GITHUB_STATE"] = files.HostState
	env["GITHUB_STEP_SUMMARY"] = files.HostSummary

	nodePath, err := lookPathWithEnv("node", env)
	if err != nil {
		return "", nil, nil, fmt.Errorf("node runtime is required for %s: %w", invocation.displayName, err)
	}

	entryPath := filepath.Join(invocation.actionDir, filepath.FromSlash(entry))
	rawOutput, runErr := runProcess(firstNonEmptyContext(ctx.Context), nodePath, []string{entryPath}, invocation.actionDir, env)
	legacy := processWorkflowCommands(rawOutput, state.masks)
	state.masks = append(state.masks, legacy.Masks...)
	fileCommands, err := files.Parse()
	if err != nil {
		return legacy.Output, legacy.Outputs, legacy.State, err
	}

	state.applyCommandEnv(fileCommands.Env)
	state.applyCommandEnv(legacy.Env)
	state.extraPath = append(state.extraPath, fileCommands.Paths...)
	state.extraPath = append(state.extraPath, legacy.Paths...)
	state.summary.WriteString(fileCommands.Summary)

	return legacy.Output, mergeStringMaps(legacy.Outputs, fileCommands.Outputs), mergeStringMaps(legacy.State, fileCommands.State), runErr
}

func (e *Engine) executeCompositeAction(ctx ExecContext, state *jobState, parentScope *scope, resolved *resolvedAction, invocation *actionInvocation, sharedEnv map[string]string) (stepExecutionResult, error) {
	compositeScope := parentScope.childCompositeScope(resolved.ActionDir, resolved.Reference.Repository(), resolved.Reference.Ref, invocation.inputs, parentScope.workDir)

	var combinedOutput []string
	for index, child := range resolved.Metadata.Runs.Steps {
		spec := compositeStepToSpec(child, index)
		result, err := e.runStep(ctx, state, compositeScope, spec, sharedEnv)
		if strings.TrimSpace(result.Output) != "" {
			combinedOutput = append(combinedOutput, result.Output)
		}
		if err != nil {
			return stepExecutionResult{Output: strings.Join(combinedOutput, "\n")}, err
		}
	}

	githubContext := state.githubForScope(compositeScope, invocation.actionName)
	evaluator := state.evaluator(compositeScope, mergeStringMaps(copyStringMap(state.globalEnv), sharedEnv, githubEnv(githubContext)), githubContext)
	outputs := map[string]string{}
	for name, output := range resolved.Metadata.Outputs {
		outputs[name] = evaluator.Interpolate(output.Value)
	}

	return stepExecutionResult{Output: strings.Join(combinedOutput, "\n"), Outputs: outputs}, nil
}

func (e *Engine) executeDockerURLStep(ctx ExecContext, state *jobState, scope *scope, spec stepSpec, inheritedEnv map[string]string, reference ActionReference) (stepExecutionResult, error) {
	githubContext := state.githubForScope(scope, state.nextActionName("docker"))
	evaluator := state.evaluator(scope, mergeStringMaps(copyStringMap(state.globalEnv), inheritedEnv, githubEnv(githubContext)), githubContext)
	stepEnv := interpolateInterfaceMap(spec.Env, evaluator)
	args := dockerArgs(spec.With, evaluator)
	output, outputs, stateValues, err := e.runDockerContainer(ctx, state, githubContext, "docker://"+reference.Image, reference.Image, "", args, mergeStringMaps(inheritedEnv, stepEnv), nil)
	return stepExecutionResult{Output: output, Outputs: outputs, State: stateValues}, err
}

func (e *Engine) executeDockerAction(ctx ExecContext, state *jobState, scope *scope, invocation *actionInvocation, sharedEnv map[string]string) (stepExecutionResult, error) {
	metadata := invocation.metadata
	if metadata.Runs.PostEntrypoint != "" {
		state.posts = append(state.posts, &postAction{ifExpression: metadata.Runs.PostIf, scope: scope, invocation: invocation})
	}

	if strings.TrimSpace(metadata.Runs.PreEntrypoint) != "" {
		if _, _, _, err := e.runDockerActionStage(ctx, state, scope, invocation, sharedEnv, metadata.Runs.PreEntrypoint); err != nil {
			return stepExecutionResult{}, err
		}
	}

	output, outputs, stateValues, err := e.runDockerActionStage(ctx, state, scope, invocation, sharedEnv, metadata.Runs.Entrypoint)
	for key, value := range stateValues {
		invocation.state[key] = value
	}
	return stepExecutionResult{Output: output, Outputs: outputs, State: stateValues}, err
}

func (e *Engine) runDockerActionStage(ctx ExecContext, state *jobState, scope *scope, invocation *actionInvocation, sharedEnv map[string]string, entrypoint string) (string, map[string]string, map[string]string, error) {
	metadata := invocation.metadata
	githubContext := state.githubForScope(scope, invocation.actionName)
	baseEnv := mergeStringMaps(copyStringMap(state.globalEnv), sharedEnv, githubEnv(githubContext), inputEnv(invocation.inputs), stateEnv(invocation.state))
	evaluator := state.evaluator(scope, baseEnv, githubContext)
	actionEnv := interpolateStringMap(metadata.Runs.Env, evaluator)
	args := make([]string, 0, len(metadata.Runs.Args))
	for _, arg := range metadata.Runs.Args {
		args = append(args, evaluator.Interpolate(arg))
	}

	image := evaluator.Interpolate(metadata.Runs.Image)
	resolvedImage, mounts, err := e.resolveDockerImage(firstNonEmptyContext(ctx.Context), invocation.actionDir, image)
	if err != nil {
		return "", nil, nil, err
	}
	return e.runDockerContainer(ctx, state, githubContext, image, resolvedImage, entrypoint, args, mergeStringMaps(sharedEnv, actionEnv), mounts)
}

func (e *Engine) runDockerContainer(ctx ExecContext, state *jobState, githubContext *actmodel.GithubContext, imageLabel string, image string, entrypoint string, args []string, additionalEnv map[string]string, extraMounts []string) (string, map[string]string, map[string]string, error) {
	files, err := NewStepFiles(state.fileCommandsDir)
	if err != nil {
		return "", nil, nil, err
	}

	hostEventPath := state.github.EventPath
	eventMountDir := filepath.Dir(hostEventPath)
	eventName := filepath.Base(hostEventPath)
	containerEventPath := filepath.ToSlash(filepath.Join(containerWorkflowPath, eventName))

	env := mergeStringMaps(copyStringMap(state.globalEnv), additionalEnv, githubEnv(githubContext))
	env["GITHUB_ENV"] = files.ContainerEnv
	env["GITHUB_OUTPUT"] = files.ContainerOutput
	env["GITHUB_PATH"] = files.ContainerPath
	env["GITHUB_STATE"] = files.ContainerState
	env["GITHUB_STEP_SUMMARY"] = files.ContainerSummary
	env["GITHUB_WORKSPACE"] = containerWorkspacePath
	env["GITHUB_EVENT_PATH"] = containerEventPath
	env["RUNNER_TEMP"] = containerWorkflowPath
	env["RUNNER_TOOL_CACHE"] = containerToolCachePath
	if env["GITHUB_ACTION_PATH"] != "" {
		env["GITHUB_ACTION_PATH"] = containerActionPath
	}
	applyExtraPath(env, state.extraPath)

	if err := e.pullImage(firstNonEmptyContext(ctx.Context), image); err != nil {
		return "", nil, nil, err
	}

	dockerPath, err := lookPathWithEnv("docker", env)
	if err != nil {
		return "", nil, nil, fmt.Errorf("docker is required for docker-based actions: %w", err)
	}

	command := []string{"run", "--rm", "-v", state.workspaceDir + ":" + containerWorkspacePath, "-v", state.fileCommandsDir + ":" + containerFileCommandRoot, "-v", eventMountDir + ":" + containerWorkflowPath, "-v", state.toolCacheDir + ":" + containerToolCachePath, "-w", containerWorkspacePath}
	if githubContext.ActionPath != "" {
		command = append(command, "-v", githubContext.ActionPath+":"+containerActionPath)
	}
	command = append(command, extraMounts...)

	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		command = append(command, "-e", key+"="+env[key])
	}
	if strings.TrimSpace(entrypoint) != "" {
		command = append(command, "--entrypoint", entrypoint)
	}
	command = append(command, image)
	command = append(command, args...)

	rawOutput, runErr := runProcess(firstNonEmptyContext(ctx.Context), dockerPath, command, state.workspaceDir, nil)
	legacy := processWorkflowCommands(rawOutput, state.masks)
	state.masks = append(state.masks, legacy.Masks...)
	fileCommands, err := files.Parse()
	if err != nil {
		return legacy.Output, legacy.Outputs, legacy.State, err
	}

	state.applyCommandEnv(fileCommands.Env)
	state.applyCommandEnv(legacy.Env)
	state.extraPath = append(state.extraPath, fileCommands.Paths...)
	state.extraPath = append(state.extraPath, legacy.Paths...)
	state.summary.WriteString(fileCommands.Summary)

	return legacy.Output, mergeStringMaps(legacy.Outputs, fileCommands.Outputs), mergeStringMaps(legacy.State, fileCommands.State), runErr
}

func (e *Engine) runPostAction(ctx ExecContext, state *jobState, action *postAction) (string, error) {
	result := action.scope.steps[action.invocation.stepID]
	if result == nil || result.Conclusion == actmodel.StepStatusSkipped {
		return "", nil
	}

	githubContext := state.githubForScope(action.scope, action.invocation.actionName)
	evaluator := state.evaluator(action.scope, mergeStringMaps(copyStringMap(state.globalEnv), githubEnv(githubContext)), githubContext)
	run, err := evaluator.EvalBool(action.ifExpression, actexpr.DefaultStatusCheckAlways)
	if err != nil {
		return "", err
	}
	if !run {
		return "", nil
	}

	switch action.invocation.metadata.Runs.Using {
	case "node12", "node16", "node20", "node24":
		output, _, _, err := e.runNodeStage(ctx, state, action.scope, action.invocation, nil, action.invocation.metadata.Runs.Post)
		return output, err
	case "docker":
		output, _, _, err := e.runDockerActionStage(ctx, state, action.scope, action.invocation, nil, action.invocation.metadata.Runs.PostEntrypoint)
		return output, err
	default:
		return "", nil
	}
}

func (e *Engine) resolveDockerImage(ctx context.Context, actionDir string, image string) (string, []string, error) {
	trimmed := strings.TrimSpace(image)
	if trimmed == "" {
		return "", nil, fmt.Errorf("docker action image is empty")
	}
	if strings.HasPrefix(trimmed, "docker://") {
		resolved := strings.TrimSpace(strings.TrimPrefix(trimmed, "docker://"))
		if err := e.pullImage(ctx, resolved); err != nil {
			return "", nil, err
		}
		return resolved, nil, nil
	}

	localPath := filepath.Join(actionDir, filepath.FromSlash(trimmed))
	if info, err := os.Stat(localPath); err == nil && !info.IsDir() {
		imageID, buildErr := e.buildDockerfile(ctx, actionDir, localPath)
		if buildErr != nil {
			return "", nil, buildErr
		}
		return imageID, nil, nil
	}

	if err := e.pullImage(ctx, trimmed); err != nil {
		return "", nil, err
	}
	return trimmed, nil, nil
}

func (e *Engine) pullImage(ctx context.Context, image string) error {
	e.mu.Lock()
	if _, ok := e.pulledImages[image]; ok {
		e.mu.Unlock()
		return nil
	}
	e.mu.Unlock()

	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("docker is not available: %w", err)
	}
	if _, err := runProcess(ctx, dockerPath, []string{"pull", image}, "", nil); err != nil {
		return fmt.Errorf("pull docker image %s: %w", image, err)
	}

	e.mu.Lock()
	e.pulledImages[image] = struct{}{}
	e.mu.Unlock()
	return nil
}

func (e *Engine) buildDockerfile(ctx context.Context, actionDir string, dockerfilePath string) (string, error) {
	e.mu.Lock()
	if imageID, ok := e.builtImages[dockerfilePath]; ok {
		e.mu.Unlock()
		return imageID, nil
	}
	e.mu.Unlock()

	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return "", fmt.Errorf("docker is not available: %w", err)
	}

	output, err := runProcess(ctx, dockerPath, []string{"build", "-q", "-f", dockerfilePath, actionDir}, actionDir, nil)
	if err != nil {
		return "", fmt.Errorf("build docker action image from %s: %w", dockerfilePath, err)
	}
	imageID := strings.TrimSpace(output)
	if imageID == "" {
		return "", fmt.Errorf("build docker action image from %s returned an empty image id", dockerfilePath)
	}

	e.mu.Lock()
	e.builtImages[dockerfilePath] = imageID
	e.mu.Unlock()
	return imageID, nil
}

func (state *jobState) applyCommandEnv(values map[string]string) {
	for key, value := range values {
		upper := strings.ToUpper(key)
		if upper == "NODE_OPTIONS" {
			continue
		}
		if strings.HasPrefix(upper, "GITHUB_") || strings.HasPrefix(upper, "RUNNER_") {
			if upper != "CI" {
				continue
			}
		}
		state.globalEnv[key] = value
		if key == "PATH" {
			state.basePath = value
		}
	}
}

func processWorkflowCommands(output string, existingMasks []string) workflowCommandResult {
	result := workflowCommandResult{
		Env:     map[string]string{},
		Outputs: map[string]string{},
		State:   map[string]string{},
		Paths:   []string{},
		Masks:   []string{},
	}
	masks := append([]string{}, existingMasks...)
	lines := strings.Split(output, "\n")
	sanitized := make([]string, 0, len(lines))
	for _, line := range lines {
		name, properties, arg, ok := parseWorkflowCommand(line)
		if ok {
			rendered := ""
			switch strings.ToLower(name) {
			case "add-mask":
				if arg != "" {
					result.Masks = append(result.Masks, arg)
					masks = append(masks, arg)
				}
			case "set-output":
				if outputName := properties["name"]; outputName != "" {
					result.Outputs[outputName] = arg
				}
			case "save-state":
				if stateName := properties["name"]; stateName != "" {
					result.State[stateName] = arg
				}
			case "add-path":
				if arg != "" {
					result.Paths = append(result.Paths, arg)
				}
			case "set-env":
				if envName := properties["name"]; envName != "" {
					result.Env[envName] = arg
				}
			case "notice", "warning", "error":
				if arg != "" {
					rendered = fmt.Sprintf("%s: %s", strings.ToLower(name), arg)
				}
			case "group", "endgroup", "debug", "echo":
				// Suppress workflow-command noise from compact console output.
			default:
				if arg != "" {
					rendered = arg
				}
			}
			if rendered != "" {
				sanitized = append(sanitized, applyMasks(rendered, masks))
			}
			continue
		}
		sanitized = append(sanitized, applyMasks(line, masks))
	}
	result.Output = strings.Join(sanitized, "\n")
	return result
}

func parseWorkflowCommand(line string) (string, map[string]string, string, bool) {
	if !strings.HasPrefix(line, "::") {
		return "", nil, "", false
	}
	payload := strings.TrimPrefix(line, "::")
	separator := strings.Index(payload, "::")
	if separator < 0 {
		return "", nil, "", false
	}
	header := payload[:separator]
	arg := decodeCommandValue(payload[separator+2:])
	name := header
	properties := map[string]string{}
	if space := strings.IndexByte(header, ' '); space >= 0 {
		name = header[:space]
		for _, part := range strings.Split(header[space+1:], ",") {
			key, value, ok := strings.Cut(part, "=")
			if !ok {
				continue
			}
			properties[key] = decodeCommandValue(value)
		}
	}
	return name, properties, arg, true
}

func decodeCommandValue(value string) string {
	replacer := strings.NewReplacer("%25", "%", "%0D", "\r", "%0d", "\r", "%0A", "\n", "%0a", "\n", "%3A", ":", "%3a", ":", "%2C", ",", "%2c", ",")
	return replacer.Replace(value)
}

func applyMasks(input string, masks []string) string {
	masked := input
	for _, mask := range masks {
		if mask == "" {
			continue
		}
		masked = strings.ReplaceAll(masked, mask, "***")
	}
	return masked
}

func planStepToSpec(step model.PlanStep) stepSpec {
	return stepSpec{
		ID:               step.ID,
		Name:             step.Name,
		Run:              step.Run,
		Use:              step.Use,
		With:             step.With,
		Env:              step.Env,
		Shell:            step.Shell,
		WorkingDirectory: step.WorkingDirectory,
	}
}

func compositeStepToSpec(step CompositeActionStep, index int) stepSpec {
	id := strings.TrimSpace(step.ID)
	if id == "" {
		id = fmt.Sprintf("composite-step-%d", index+1)
	}
	return stepSpec{
		ID:               id,
		Name:             step.Name,
		Run:              step.Run,
		Use:              step.Uses,
		With:             step.With,
		Env:              step.Env,
		Shell:            step.Shell,
		WorkingDirectory: step.WorkingDirectory,
		If:               step.If,
		ContinueOnError:  step.ContinueOnError,
	}
}

func prepareInputs(provided map[string]interface{}, defined map[string]ActionInput, evaluator *Evaluator) (map[string]interface{}, map[string]string, error) {
	values := map[string]string{}
	for key, value := range provided {
		values[key] = interpolateArbitraryValue(value, evaluator)
	}
	for key, input := range defined {
		if _, ok := values[key]; ok {
			continue
		}
		if input.Default != nil {
			values[key] = interpolateArbitraryValue(input.Default, evaluator)
			continue
		}
		if input.Required {
			return nil, nil, fmt.Errorf("required input %q is missing", key)
		}
		values[key] = ""
	}

	inputs := make(map[string]interface{}, len(values))
	inputEnvValues := make(map[string]string, len(values))
	for key, value := range values {
		inputs[key] = value
		inputEnvValues[inputEnvKey(key)] = value
	}
	return inputs, inputEnvValues, nil
}

func inputEnv(values map[string]interface{}) map[string]string {
	if len(values) == 0 {
		return nil
	}
	env := make(map[string]string, len(values))
	for key, value := range values {
		env[inputEnvKey(key)] = stringifyValue(value)
	}
	return env
}

func stateEnv(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	env := make(map[string]string, len(values))
	for key, value := range values {
		env["STATE_"+key] = value
	}
	return env
}

func dockerArgs(values map[string]interface{}, evaluator *Evaluator) []string {
	if len(values) == 0 {
		return nil
	}
	raw, ok := values["args"]
	if !ok {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		arguments := make([]string, 0, len(typed))
		for _, value := range typed {
			arguments = append(arguments, evaluator.Interpolate(value))
		}
		return arguments
	case []interface{}:
		arguments := make([]string, 0, len(typed))
		for _, value := range typed {
			arguments = append(arguments, interpolateArbitraryValue(value, evaluator))
		}
		return arguments
	default:
		resolved := interpolateArbitraryValue(raw, evaluator)
		if resolved == "" {
			return nil
		}
		return strings.Fields(resolved)
	}
}

func interpolateStringMap(values map[string]string, evaluator *Evaluator) map[string]string {
	if len(values) == 0 {
		return nil
	}
	resolved := make(map[string]string, len(values))
	for key, value := range values {
		resolved[key] = evaluator.Interpolate(value)
	}
	return resolved
}

func interpolateInterfaceMap(values map[string]interface{}, evaluator *Evaluator) map[string]string {
	if len(values) == 0 {
		return nil
	}
	resolved := make(map[string]string, len(values))
	for key, value := range values {
		resolved[key] = interpolateArbitraryValue(value, evaluator)
	}
	return resolved
}

func interpolateArbitraryValue(value interface{}, evaluator *Evaluator) string {
	text := stringifyValue(value)
	if evaluator == nil {
		return text
	}
	return evaluator.Interpolate(text)
}

func prepareShellCommand(shell string, script string, workingDir string, tempDir string, env map[string]string, requireShell bool) (string, []string, func(), error) {
	resolvedShell := strings.TrimSpace(shell)
	if resolvedShell == "" {
		if requireShell {
			return "", nil, nil, fmt.Errorf("composite run steps must declare a shell")
		}
		if runtime.GOOS == "windows" {
			if _, err := lookPathWithEnv("pwsh", env); err == nil {
				resolvedShell = "pwsh"
			} else if _, err := lookPathWithEnv("powershell", env); err == nil {
				resolvedShell = "powershell"
			} else {
				resolvedShell = "cmd"
			}
		} else if _, err := lookPathWithEnv("bash", env); err == nil {
			resolvedShell = "bash"
		} else {
			resolvedShell = "sh"
		}
	}

	commandName, arguments, extension := shellCommandTemplate(resolvedShell)
	scriptFile, err := os.CreateTemp(tempDir, "gha-script-*"+extension)
	if err != nil {
		return "", nil, nil, fmt.Errorf("create step script: %w", err)
	}
	scriptPath := scriptFile.Name()
	if !strings.HasSuffix(script, "\n") {
		script += "\n"
	}
	if _, err := scriptFile.WriteString(script); err != nil {
		_ = scriptFile.Close()
		return "", nil, nil, fmt.Errorf("write step script %s: %w", scriptPath, err)
	}
	if err := scriptFile.Close(); err != nil {
		return "", nil, nil, fmt.Errorf("close step script %s: %w", scriptPath, err)
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(scriptPath, 0755)
	}

	for index := range arguments {
		arguments[index] = strings.ReplaceAll(arguments[index], "{0}", scriptPath)
	}
	if !containsPlaceholder(arguments) {
		arguments = append(arguments, scriptPath)
	}

	commandPath, err := lookPathWithEnv(commandName, env)
	if err != nil {
		return "", nil, nil, fmt.Errorf("resolve shell %s: %w", commandName, err)
	}

	return commandPath, arguments, func() { _ = os.Remove(scriptPath) }, nil
}

func shellCommandTemplate(shell string) (string, []string, string) {
	switch strings.TrimSpace(shell) {
	case "bash":
		return "bash", []string{"--noprofile", "--norc", "-eo", "pipefail", "{0}"}, ".sh"
	case "sh":
		return "sh", []string{"-e", "{0}"}, ".sh"
	case "pwsh":
		return "pwsh", []string{"-command", ". '{0}'"}, ".ps1"
	case "powershell":
		return "powershell", []string{"-command", ". '{0}'"}, ".ps1"
	case "cmd":
		return "cmd", []string{"/D", "/E:ON", "/V:OFF", "/S", "/C", "CALL \"{0}\""}, ".cmd"
	case "python":
		return "python", []string{"{0}"}, ".py"
	default:
		parts := strings.Fields(shell)
		if len(parts) == 0 {
			return "sh", []string{"-e", "{0}"}, ".sh"
		}
		return parts[0], parts[1:], ".sh"
	}
}

func containsPlaceholder(args []string) bool {
	for _, arg := range args {
		if strings.Contains(arg, "{0}") {
			return true
		}
	}
	return false
}

func runProcess(ctx context.Context, command string, args []string, workingDir string, env map[string]string) (string, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	if workingDir != "" {
		cmd.Dir = workingDir
	}
	if env != nil {
		cmd.Env = environmentList(env)
	}
	var buffer bytes.Buffer
	cmd.Stdout = &buffer
	cmd.Stderr = &buffer
	err := cmd.Run()
	return strings.TrimRight(buffer.String(), "\n"), err
}

func lookPathWithEnv(file string, env map[string]string) (string, error) {
	if strings.ContainsRune(file, os.PathSeparator) {
		return file, nil
	}
	pathValue := ""
	if env != nil {
		pathValue = env["PATH"]
	}
	if pathValue == "" {
		return exec.LookPath(file)
	}
	for _, directory := range filepath.SplitList(pathValue) {
		candidate := filepath.Join(directory, file)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			if runtime.GOOS == "windows" || info.Mode()&0111 != 0 {
				return candidate, nil
			}
		}
		if runtime.GOOS == "windows" {
			for _, ext := range strings.Split(strings.TrimPrefix(os.Getenv("PATHEXT"), ";"), ";") {
				pathCandidate := candidate + ext
				if info, err := os.Stat(pathCandidate); err == nil && !info.IsDir() {
					return pathCandidate, nil
				}
			}
		}
	}
	return "", fmt.Errorf("%s not found in PATH", file)
}

func environmentList(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	list := make([]string, 0, len(keys))
	for _, key := range keys {
		list = append(list, key+"="+values[key])
	}
	return list
}

func applyExtraPath(env map[string]string, paths []string) {
	if len(paths) == 0 {
		return
	}
	parts := make([]string, 0, len(paths)+1)
	for index := len(paths) - 1; index >= 0; index-- {
		if strings.TrimSpace(paths[index]) == "" {
			continue
		}
		parts = append(parts, paths[index])
	}
	if env["PATH"] != "" {
		parts = append(parts, env["PATH"])
	}
	env["PATH"] = strings.Join(parts, string(os.PathListSeparator))
}

func copyStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	copy := make(map[string]string, len(values))
	for key, value := range values {
		copy[key] = value
	}
	return copy
}

func mergeStringMaps(maps ...map[string]string) map[string]string {
	merged := map[string]string{}
	for _, values := range maps {
		for key, value := range values {
			merged[key] = value
		}
	}
	return merged
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstNonEmptyContext(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
}

func defaultRootDir(name string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "gluon", name)
	}
	return filepath.Join(home, ".gluon", name)
}

func sanitizePathComponent(value string) string {
	clean := actionNamePattern.ReplaceAllString(strings.TrimSpace(value), "-")
	clean = strings.Trim(clean, "-")
	if clean == "" {
		return "job"
	}
	return clean
}

func randomToken(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate runtime token: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

func scopeStatus(stepResults map[string]*actmodel.StepResult) string {
	for _, result := range stepResults {
		if result != nil && result.Conclusion == actmodel.StepStatusFailure {
			return "failure"
		}
	}
	return "success"
}

func actionBaseName(spec stepSpec, reference ActionReference) string {
	if reference.Kind == referenceKindRemote && reference.Repo != "" {
		return reference.Repo
	}
	if spec.Name != "" {
		return spec.Name
	}
	return "action"
}

func inputEnvKey(name string) string {
	upper := strings.ToUpper(name)
	upper = strings.NewReplacer(" ", "_", "-", "_", ".", "_", "/", "_", "\\", "_").Replace(upper)
	return "INPUT_" + upper
}

func resolveWorkingDirectory(base string, raw string, evaluator *Evaluator) string {
	resolved := strings.TrimSpace(raw)
	if evaluator != nil {
		resolved = strings.TrimSpace(evaluator.Interpolate(resolved))
	}
	if resolved == "" {
		return base
	}
	if filepath.IsAbs(resolved) {
		return resolved
	}
	return filepath.Clean(filepath.Join(base, resolved))
}

func githubEnv(github *actmodel.GithubContext) map[string]string {
	if github == nil {
		return nil
	}
	return map[string]string{
		"CI":                       "true",
		"GITHUB_WORKFLOW":          github.Workflow,
		"GITHUB_RUN_ATTEMPT":       github.RunAttempt,
		"GITHUB_RUN_ID":            github.RunID,
		"GITHUB_RUN_NUMBER":        github.RunNumber,
		"GITHUB_ACTION":            github.Action,
		"GITHUB_ACTION_PATH":       github.ActionPath,
		"GITHUB_ACTION_REPOSITORY": github.ActionRepository,
		"GITHUB_ACTION_REF":        github.ActionRef,
		"GITHUB_ACTIONS":           "true",
		"GITHUB_ACTOR":             github.Actor,
		"GITHUB_REPOSITORY":        github.Repository,
		"GITHUB_EVENT_NAME":        github.EventName,
		"GITHUB_EVENT_PATH":        github.EventPath,
		"GITHUB_WORKSPACE":         github.Workspace,
		"GITHUB_SHA":               github.Sha,
		"GITHUB_REF":               github.Ref,
		"GITHUB_REF_NAME":          github.RefName,
		"GITHUB_REF_TYPE":          github.RefType,
		"GITHUB_JOB":               github.Job,
		"GITHUB_REPOSITORY_OWNER":  github.RepositoryOwner,
		"GITHUB_RETENTION_DAYS":    github.RetentionDays,
		"RUNNER_PERFLOG":           github.RunnerPerflog,
		"RUNNER_TRACKING_ID":       github.RunnerTrackingID,
		"GITHUB_BASE_REF":          github.BaseRef,
		"GITHUB_HEAD_REF":          github.HeadRef,
		"GITHUB_SERVER_URL":        github.ServerURL,
		"GITHUB_API_URL":           github.APIURL,
		"GITHUB_GRAPHQL_URL":       github.GraphQLURL,
	}
}

func buildSecrets(env map[string]string) map[string]string {
	secrets := map[string]string{}
	if token := env["GITHUB_TOKEN"]; token != "" {
		secrets["GITHUB_TOKEN"] = token
	}
	return secrets
}

func runnerOS() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS"
	case "windows":
		return "Windows"
	default:
		return "Linux"
	}
}

func runnerArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "X64"
	case "arm64":
		return "ARM64"
	case "arm":
		return "ARM"
	default:
		return strings.ToUpper(runtime.GOARCH)
	}
}

func ensureEventFile(tempDir string, configuredPath string, repository string, owner string, ref string, sha string, defaultBranch string) (string, map[string]interface{}, error) {
	if configuredPath != "" {
		data, err := os.ReadFile(configuredPath)
		if err == nil {
			payload := map[string]interface{}{}
			_ = json.Unmarshal(data, &payload)
			return configuredPath, payload, nil
		}
	}

	payload := map[string]interface{}{
		"ref":     ref,
		"after":   sha,
		"deleted": false,
		"repository": map[string]interface{}{
			"default_branch": defaultBranch,
			"full_name":      repository,
			"name":           repositoryName(repository),
			"owner": map[string]interface{}{
				"login": owner,
			},
		},
	}
	path := filepath.Join(tempDir, "event.json")
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", nil, fmt.Errorf("serialize local event payload: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", nil, fmt.Errorf("write local event payload: %w", err)
	}
	return path, payload, nil
}

func repositoryName(repository string) string {
	if repository == "" {
		return "workspace"
	}
	parts := strings.Split(repository, "/")
	return parts[len(parts)-1]
}

func detectRepository(ctx context.Context, dir string) (string, string) {
	remote := gitOutput(ctx, dir, "config", "--get", "remote.origin.url")
	if repository, owner := parseRemoteRepository(remote); repository != "" {
		return repository, owner
	}
	name := filepath.Base(dir)
	return "local/" + name, "local"
}

func parseRemoteRepository(raw string) (string, string) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", ""
	}
	value = strings.TrimSuffix(value, ".git")
	value = strings.TrimPrefix(value, "ssh://")
	value = strings.TrimPrefix(value, "git@")
	value = strings.TrimPrefix(value, "https://")
	value = strings.TrimPrefix(value, "http://")
	value = strings.TrimPrefix(value, "github.com:")
	if index := strings.IndexByte(value, '@'); index >= 0 {
		value = value[index+1:]
	}
	if index := strings.IndexByte(value, ':'); index >= 0 && !strings.Contains(value[:index], "/") {
		value = value[index+1:]
	}
	if index := strings.Index(value, "github.com/"); index >= 0 {
		value = value[index+len("github.com/"):]
	}
	parts := strings.Split(value, "/")
	if len(parts) < 2 {
		return "", ""
	}
	repository := parts[len(parts)-2] + "/" + parts[len(parts)-1]
	return repository, parts[len(parts)-2]
}

func detectRef(ctx context.Context, dir string) string {
	branch := gitOutput(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	branch = strings.TrimSpace(branch)
	if branch != "" && branch != "HEAD" {
		return "refs/heads/" + branch
	}
	return ""
}

func detectSHA(ctx context.Context, dir string) string {
	return strings.TrimSpace(gitOutput(ctx, dir, "rev-parse", "HEAD"))
}

func deriveRefParts(ref string) (string, string) {
	if strings.HasPrefix(ref, "refs/tags/") {
		return strings.TrimPrefix(ref, "refs/tags/"), "tag"
	}
	if strings.HasPrefix(ref, "refs/heads/") {
		return strings.TrimPrefix(ref, "refs/heads/"), "branch"
	}
	if strings.HasPrefix(ref, "refs/pull/") {
		return strings.TrimPrefix(ref, "refs/pull/"), ""
	}
	return ref, ""
}

func gitOutput(ctx context.Context, dir string, args ...string) string {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return ""
	}
	command := exec.CommandContext(firstNonEmptyContext(ctx), gitPath, append([]string{"-C", dir}, args...)...)
	data, err := command.Output()
	if err != nil {
		return ""
	}
	return string(bytes.TrimSpace(data))
}
