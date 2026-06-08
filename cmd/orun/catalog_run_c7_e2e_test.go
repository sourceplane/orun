package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogstore"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/statestore"
)

func TestCatalogRunC7_E2EPlanRunHistory(t *testing.T) {
	dir := withTempIntentRoot(t)
	seedCatalogRunWorkspace(t, dir)
	resetCatalogFlags(t)
	resetCatalogRunE2EGlobals(t)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	planPath := filepath.Join(dir, "plan.json")
	intentFile = filepath.Join(dir, "intent.yaml")
	intentRoot = dir
	allFlag = true
	outputFile = planPath
	outputFormat = "json"

	_ = captureStdout(t, generatePlan)

	planRaw, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("read plan output: %v", err)
	}
	var plan model.Plan
	if err := json.Unmarshal(planRaw, &plan); err != nil {
		t.Fatalf("decode plan output: %v\n%s", err, planRaw)
	}
	if plan.Metadata.Revision == nil || plan.Metadata.Revision.Key == "" {
		t.Fatalf("plan missing revision metadata: %+v", plan.Metadata.Revision)
	}
	revKey := plan.Metadata.Revision.Key

	ghaExecID := "gh-123456789-2-abcdef0"
	runPlanRef = planPath
	runExecID = ghaExecID
	runRunner = "local"
	runDryRun = false
	runWorkDir = "."
	runIsolation = "none"
	runComponentConcurrency = 1

	_ = captureStdout(t, runPlan)

	stateStore, _, err := openLocalStateStore()
	if err != nil {
		t.Fatalf("open state store: %v", err)
	}
	revIdx, _, err := statestore.ReadRevisionIndex(context.Background(), stateStore, revKey)
	if err != nil {
		t.Fatalf("ReadRevisionIndex: %v", err)
	}
	if revIdx.SourceSnapshotKey == "" || revIdx.CatalogSnapshotKey == "" {
		t.Fatalf("revision index missing catalog parent: %+v", revIdx)
	}

	execIdx, _, err := statestore.ReadExecutionIndex(context.Background(), stateStore, ghaExecID)
	if err != nil {
		t.Fatalf("ReadExecutionIndex: %v", err)
	}
	if execIdx.ExecutionKey != ghaExecID {
		t.Fatalf("execution key was rewritten: got %q want %q", execIdx.ExecutionKey, ghaExecID)
	}
	if execIdx.SourceSnapshotKey != revIdx.SourceSnapshotKey || execIdx.CatalogSnapshotKey != revIdx.CatalogSnapshotKey {
		t.Fatalf("execution index parent mismatch: exec=%+v rev=%+v", execIdx, revIdx)
	}

	catExecPath, err := catalogstore.CatalogExecutionDocPath(
		revIdx.SourceSnapshotKey,
		revIdx.CatalogSnapshotKey,
		revKey,
		ghaExecID,
	)
	if err != nil {
		t.Fatalf("CatalogExecutionDocPath: %v", err)
	}
	execRaw, _, err := stateStore.Read(context.Background(), catExecPath)
	if err != nil {
		t.Fatalf("catalog-owned execution missing at %s: %v", catExecPath, err)
	}
	if !strings.Contains(string(execRaw), `"executionKey": "`+ghaExecID+`"`) ||
		!strings.Contains(string(execRaw), `"sourceSnapshotKey": "`+revIdx.SourceSnapshotKey+`"`) ||
		!strings.Contains(string(execRaw), `"catalogSnapshotKey": "`+revIdx.CatalogSnapshotKey+`"`) {
		t.Fatalf("catalog execution missing expected lineage:\n%s", execRaw)
	}
	// (The legacy .orun/executions/<id>/state.json mirror was removed at the
	// M12 cutover; the runner no longer writes legacy execution state.)

	catalogJSONFlag = true
	historyOut := captureStdout(t, func() error {
		return runCatalogHistory(context.Background(), "svc-a")
	})
	var env catalogEnvelope
	var rows []catalogmodel.ComponentExecutionRow
	env.Data = &rows
	if err := json.Unmarshal([]byte(historyOut), &env); err != nil {
		t.Fatalf("decode history envelope: %v\n%s", err, historyOut)
	}
	if len(rows) != 1 {
		t.Fatalf("history rows = %+v; want one execution", rows)
	}
	// History now reads the object-model execution graph (the catalogstore
	// retirement). The lineage keys are object-model ids (not the legacy
	// rev-/cat- keys), and the executionKey is the object-model run handle
	// derived from the execution id; profile/environment/triggerName are not
	// recorded on the object-model execution (a documented v1 gap).
	row := rows[0]
	if row.ComponentKey != "sourceplane/orun/svc-a" {
		t.Errorf("history row componentKey = %q, want sourceplane/orun/svc-a", row.ComponentKey)
	}
	if !strings.HasPrefix(row.ExecutionKey, "run-") {
		t.Errorf("history row executionKey = %q, want a run- handle", row.ExecutionKey)
	}
	if row.RevisionKey == "" || row.SourceSnapshotKey == "" || row.CatalogSnapshotKey == "" {
		t.Errorf("history row missing object-model lineage: %+v", row)
	}
	if row.Status != "succeeded" {
		t.Errorf("history row status = %q, want succeeded", row.Status)
	}
}

func resetCatalogRunE2EGlobals(t *testing.T) {
	t.Helper()
	prev := struct {
		intentFile, intentRoot, configDir, outputFile, outputFormat string
		allFlag, debugMode, changedOnly                             bool
		baseBranch, headRef, environment                            string
		changedFiles                                                []string
		uncommitted, untracked, explainChanged                      bool
		intentImpact                                                string
		planName                                                    string
		planComponents                                              []string
		planNoCatalogRefresh                                        bool
		planCatalogSource, planCatalogSnapshot                      string
		planCatalogStrict                                           bool
		runPlanRef, runResolvedRevisionArg                          string
		runDryRun, runVerbose                                       bool
		runWorkDir                                                  string
		runUseWorkDirOverride                                       bool
		runJobID                                                    string
		runRetry                                                    bool
		runRunner                                                   string
		runGHACompat                                                bool
		runExecID                                                   string
		runConcurrency, runComponentConcurrency                     int
		runComponent                                                []string
		runEnv                                                      string
		runJSON                                                     bool
		runIsolation                                                string
		runKeepWorkspaces, runBackground                            bool
		catalogHistoryTrigger, catalogHistoryProfile                string
		catalogHistoryEnv                                           string
		catalogHistoryLimit                                         int
	}{
		intentFile, intentRoot, configDir, outputFile, outputFormat,
		allFlag, debugMode, changedOnly,
		baseBranch, headRef, environment,
		changedFiles,
		uncommitted, untracked, explainChanged,
		intentImpact,
		planName,
		planComponents,
		planNoCatalogRefresh,
		planCatalogSource, planCatalogSnapshot,
		planCatalogStrict,
		runPlanRef, runResolvedRevisionArg,
		runDryRun, runVerbose,
		runWorkDir,
		runUseWorkDirOverride,
		runJobID,
		runRetry,
		runRunner,
		runGHACompat,
		runExecID,
		runConcurrency, runComponentConcurrency,
		runComponent,
		runEnv,
		runJSON,
		runIsolation,
		runKeepWorkspaces, runBackground,
		catalogHistoryTriggerFlag, catalogHistoryProfileFlag,
		catalogHistoryEnvFlag,
		catalogHistoryLimitFlag,
	}
	t.Cleanup(func() {
		intentFile, intentRoot, configDir, outputFile, outputFormat = prev.intentFile, prev.intentRoot, prev.configDir, prev.outputFile, prev.outputFormat
		allFlag, debugMode, changedOnly = prev.allFlag, prev.debugMode, prev.changedOnly
		baseBranch, headRef, environment = prev.baseBranch, prev.headRef, prev.environment
		changedFiles = prev.changedFiles
		uncommitted, untracked, explainChanged = prev.uncommitted, prev.untracked, prev.explainChanged
		intentImpact = prev.intentImpact
		planName = prev.planName
		planComponents = prev.planComponents
		planNoCatalogRefresh = prev.planNoCatalogRefresh
		planCatalogSource, planCatalogSnapshot = prev.planCatalogSource, prev.planCatalogSnapshot
		planCatalogStrict = prev.planCatalogStrict
		runPlanRef, runResolvedRevisionArg = prev.runPlanRef, prev.runResolvedRevisionArg
		runDryRun, runVerbose = prev.runDryRun, prev.runVerbose
		runWorkDir = prev.runWorkDir
		runUseWorkDirOverride = prev.runUseWorkDirOverride
		runJobID = prev.runJobID
		runRetry = prev.runRetry
		runRunner = prev.runRunner
		runGHACompat = prev.runGHACompat
		runExecID = prev.runExecID
		runConcurrency, runComponentConcurrency = prev.runConcurrency, prev.runComponentConcurrency
		runComponent = prev.runComponent
		runEnv = prev.runEnv
		runJSON = prev.runJSON
		runIsolation = prev.runIsolation
		runKeepWorkspaces, runBackground = prev.runKeepWorkspaces, prev.runBackground
		catalogHistoryTriggerFlag, catalogHistoryProfileFlag = prev.catalogHistoryTrigger, prev.catalogHistoryProfile
		catalogHistoryEnvFlag = prev.catalogHistoryEnv
		catalogHistoryLimitFlag = prev.catalogHistoryLimit
	})

	intentFile = ""
	intentRoot = ""
	configDir = ""
	outputFile = ""
	outputFormat = "json"
	allFlag = false
	debugMode = false
	changedOnly = false
	baseBranch = ""
	headRef = ""
	environment = ""
	changedFiles = nil
	uncommitted = false
	untracked = false
	explainChanged = false
	intentImpact = "watch"
	planName = ""
	planComponents = nil
	planNoCatalogRefresh = false
	planCatalogSource = ""
	planCatalogSnapshot = ""
	planCatalogStrict = false
	runPlanRef = ""
	runResolvedRevisionArg = ""
	runDryRun = false
	runVerbose = false
	runWorkDir = "."
	runUseWorkDirOverride = false
	runJobID = ""
	runRetry = false
	runRunner = ""
	runGHACompat = false
	runExecID = ""
	runConcurrency = 0
	runComponentConcurrency = 1
	runComponent = nil
	runEnv = ""
	runJSON = false
	runIsolation = "auto"
	runKeepWorkspaces = false
	runBackground = false
	catalogHistoryTriggerFlag = ""
	catalogHistoryProfileFlag = ""
	catalogHistoryEnvFlag = ""
	catalogHistoryLimitFlag = 50
}

func seedCatalogRunWorkspace(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	mustWriteFile(t, filepath.Join(dir, "intent.yaml"), `apiVersion: orun.io/v1alpha1
kind: Intent
metadata:
  name: c7-demo
discovery:
  roots:
    - runtime
catalog:
  namespace: sourceplane
  defaults:
    owner: team/platform
    system: payments
compositions:
  sources:
    - name: core
      kind: dir
      path: ./core
environments:
  dev:
    selectors:
      components: [svc-a]
components:
  - name: svc-a
    type: noop
    path: .
    subscribe:
      environments:
        - name: dev
          profile: smoke
    parameters: {}
`)
	mustWriteFile(t, filepath.Join(dir, "runtime", ".keep"), "")
	mustWriteFile(t, filepath.Join(dir, "catalog", "svc-a", "component.yaml"), `apiVersion: orun.io/v1alpha1
kind: Component
metadata:
  name: svc-a
spec:
  type: noop
  owner: team/platform
  system: payments
  path: .
  environments:
    dev:
      profile: smoke
`)
	mustWriteFile(t, filepath.Join(dir, "core", "orun.yaml"), `apiVersion: sourceplane.io/v1alpha1
kind: CompositionPackage
metadata:
  name: core
spec:
  version: 1.0.0
  exports:
    - composition: noop
      path: compositions/noop.yaml
`)
	mustWriteFile(t, filepath.Join(dir, "core", "compositions", "noop.yaml"), `apiVersion: sourceplane.io/v1alpha1
kind: Composition
metadata:
  name: noop
spec:
  type: noop
  description: No-op test composition
  defaultJob: verify
  parameterSchema:
    $schema: http://json-schema.org/draft-07/schema#
    type: object
    properties:
      type:
        const: noop
      parameters:
        type: object
        additionalProperties: true
    additionalProperties: true
  jobs:
    - name: verify
      description: Verify
      runsOn: local
      timeout: 1m
      retries: 0
      steps:
        - id: echo
          name: echo
          run: echo c7-catalog-run
`)

	runGit("init", "-q")
	runGit("config", "user.email", "t@t.co")
	runGit("config", "user.name", "t")
	runGit("remote", "add", "origin", "https://github.com/sourceplane/orun.git")
	runGit("checkout", "-q", "-b", "main")
	runGit("add", "-A")
	runGit("commit", "-qm", "init")
}

func mustWriteFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
