package composition

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/sourceplane/gluon/internal/expand"
	"github.com/sourceplane/gluon/internal/model"
	"github.com/sourceplane/gluon/internal/normalize"
	"github.com/sourceplane/gluon/internal/planner"
	"gopkg.in/yaml.v3"
)

type packageJob struct {
	defaultJob string
	runsOn     string
	stepRun    string
}

func TestLoadRegistrySelectsCompositionByPrecedence(t *testing.T) {
	rootDir := t.TempDir()
	writeCompositionPackage(t, filepath.Join(rootDir, "core"), "core", map[string]packageJob{
		"helm": {defaultJob: "deploy", runsOn: "ubuntu-core", stepRun: "echo core"},
	})
	writeCompositionPackage(t, filepath.Join(rootDir, "overrides"), "overrides", map[string]packageJob{
		"helm": {defaultJob: "ship", runsOn: "ubuntu-override", stepRun: "echo override"},
	})

	intent := &model.Intent{
		Metadata: model.Metadata{Name: "precedence-test"},
		Compositions: model.CompositionConfig{
			Sources: []model.CompositionSource{
				{Name: "core", Kind: "dir", Path: "./core"},
				{Name: "overrides", Kind: "dir", Path: "./overrides"},
			},
			Resolution: model.CompositionResolution{
				Precedence: []string{"overrides", "core"},
			},
		},
		Components: []model.Component{{Name: "api", Type: "helm"}},
	}

	registry, err := LoadRegistry(intent, filepath.Join(rootDir, "intent.yaml"), "")
	if err != nil {
		t.Fatalf("LoadRegistry returned error: %v", err)
	}

	resolved := registry.Types["helm"]
	if resolved == nil {
		t.Fatalf("expected default helm composition to be resolved")
	}
	if resolved.SourceName != "overrides" {
		t.Fatalf("expected precedence to pick overrides, got %q", resolved.SourceName)
	}
	if intent.Components[0].ResolvedComposition != "overrides:helm" {
		t.Fatalf("expected component to be annotated with overrides:helm, got %q", intent.Components[0].ResolvedComposition)
	}
	if got := len(registry.Sources); got != 2 {
		t.Fatalf("expected 2 resolved sources, got %d", got)
	}
}

func TestLoadRegistrySupportsArchiveSource(t *testing.T) {
	rootDir := t.TempDir()
	packageDir := filepath.Join(rootDir, "package")
	archivePath := filepath.Join(rootDir, "dist", "core.tgz")

	writeCompositionPackage(t, packageDir, "core", map[string]packageJob{
		"helm": {defaultJob: "deploy", runsOn: "ubuntu-latest", stepRun: "echo archive"},
	})
	writeTarGz(t, packageDir, archivePath)

	intent := &model.Intent{
		Metadata: model.Metadata{Name: "archive-test"},
		Compositions: model.CompositionConfig{
			Sources: []model.CompositionSource{{Name: "core", Kind: "archive", Path: "./dist/core.tgz"}},
		},
		Components: []model.Component{{Name: "api", Type: "helm"}},
	}

	registry, err := LoadRegistry(intent, filepath.Join(rootDir, "intent.yaml"), "")
	if err != nil {
		t.Fatalf("LoadRegistry returned error: %v", err)
	}

	resolved := registry.Types["helm"]
	if resolved == nil {
		t.Fatalf("expected helm composition from archive source")
	}
	if resolved.SourceKind != "archive" {
		t.Fatalf("expected archive source kind, got %q", resolved.SourceKind)
	}
	if resolved.SourceName != "core" {
		t.Fatalf("expected source name core, got %q", resolved.SourceName)
	}
}

func TestLoadRegistryErrorsOnAmbiguousExportsWithoutResolution(t *testing.T) {
	rootDir := t.TempDir()
	writeCompositionPackage(t, filepath.Join(rootDir, "core"), "core", map[string]packageJob{
		"helm": {defaultJob: "deploy", runsOn: "ubuntu-core", stepRun: "echo core"},
	})
	writeCompositionPackage(t, filepath.Join(rootDir, "team"), "team", map[string]packageJob{
		"helm": {defaultJob: "ship", runsOn: "ubuntu-team", stepRun: "echo team"},
	})

	intent := &model.Intent{
		Metadata: model.Metadata{Name: "conflict-test"},
		Compositions: model.CompositionConfig{
			Sources: []model.CompositionSource{
				{Name: "core", Kind: "dir", Path: "./core"},
				{Name: "team", Kind: "dir", Path: "./team"},
			},
		},
		Components: []model.Component{{Name: "api", Type: "helm"}},
	}

	_, err := LoadRegistry(intent, filepath.Join(rootDir, "intent.yaml"), "")
	if err == nil {
		t.Fatalf("expected LoadRegistry to fail on ambiguous composition exports")
	}
	if !strings.Contains(err.Error(), "exported by multiple sources") {
		t.Fatalf("expected ambiguity error, got %v", err)
	}
}

func TestWriteLockFilePersistsResolvedSources(t *testing.T) {
	rootDir := t.TempDir()
	writeCompositionPackage(t, filepath.Join(rootDir, "core"), "core", map[string]packageJob{
		"helm": {defaultJob: "deploy", runsOn: "ubuntu-latest", stepRun: "echo lock"},
	})

	intent := &model.Intent{
		Metadata: model.Metadata{Name: "lock-test"},
		Compositions: model.CompositionConfig{
			Sources: []model.CompositionSource{{Name: "core", Kind: "dir", Path: "./core"}},
		},
		Components: []model.Component{{Name: "api", Type: "helm"}},
	}

	intentPath := filepath.Join(rootDir, "intent.yaml")
	registry, err := LoadRegistry(intent, intentPath, "")
	if err != nil {
		t.Fatalf("LoadRegistry returned error: %v", err)
	}

	if err := WriteLockFile(intentPath, registry.Sources); err != nil {
		t.Fatalf("WriteLockFile returned error: %v", err)
	}

	lockData, err := os.ReadFile(LockFilePath(intentPath))
	if err != nil {
		t.Fatalf("failed to read lock file: %v", err)
	}

	var lock model.CompositionLock
	if err := yaml.Unmarshal(lockData, &lock); err != nil {
		t.Fatalf("failed to parse lock file: %v", err)
	}
	if len(lock.Sources) != 1 {
		t.Fatalf("expected 1 source in lock file, got %d", len(lock.Sources))
	}
	if lock.Sources[0].Name != "core" {
		t.Fatalf("expected core source in lock file, got %q", lock.Sources[0].Name)
	}
	if len(lock.Sources[0].Exports) != 1 || lock.Sources[0].Exports[0] != "helm" {
		t.Fatalf("expected helm export in lock file, got %v", lock.Sources[0].Exports)
	}
}

func TestPlannerUsesPerComponentCompositionOverride(t *testing.T) {
	rootDir := t.TempDir()
	writeCompositionPackage(t, filepath.Join(rootDir, "core"), "core", map[string]packageJob{
		"helm": {defaultJob: "deploy", runsOn: "ubuntu-core", stepRun: "echo core"},
	})
	writeCompositionPackage(t, filepath.Join(rootDir, "overrides"), "overrides", map[string]packageJob{
		"helm": {defaultJob: "ship", runsOn: "ubuntu-override", stepRun: "echo override"},
	})

	intent := &model.Intent{
		Metadata: model.Metadata{Name: "override-plan-test"},
		Environments: map[string]model.Environment{
			"dev": {},
		},
		Compositions: model.CompositionConfig{
			Sources: []model.CompositionSource{
				{Name: "core", Kind: "dir", Path: "./core"},
				{Name: "overrides", Kind: "dir", Path: "./overrides"},
			},
			Resolution: model.CompositionResolution{
				Bindings: map[string]string{"helm": "core"},
			},
		},
		Components: []model.Component{{
			Name: "api",
			Type: "helm",
			Subscribe: model.ComponentSubscribe{
				Environments: []string{"dev"},
			},
			CompositionRef: &model.ComponentCompositionRef{Source: "overrides", Name: "helm"},
		}},
	}

	registry, err := LoadRegistry(intent, filepath.Join(rootDir, "intent.yaml"), "")
	if err != nil {
		t.Fatalf("LoadRegistry returned error: %v", err)
	}

	normalized, err := normalize.NormalizeIntent(intent)
	if err != nil {
		t.Fatalf("NormalizeIntent returned error: %v", err)
	}
	if err := registry.ValidateAllComponents(normalized); err != nil {
		t.Fatalf("ValidateAllComponents returned error: %v", err)
	}

	instances, err := expand.NewExpander(normalized).Expand()
	if err != nil {
		t.Fatalf("Expand returned error: %v", err)
	}

	compositionInfos := make(map[string]*planner.CompositionInfo)
	for compositionKey, resolved := range registry.ByKey {
		defaultJob := resolved.JobMap[resolved.DefaultJobName]
		if defaultJob == nil && len(resolved.Jobs) > 0 {
			defaultJob = &resolved.Jobs[0]
		}
		compositionInfos[compositionKey] = &planner.CompositionInfo{Type: resolved.Name, DefaultJob: defaultJob}
	}

	jobs, err := planner.NewJobPlanner(compositionInfos).PlanJobs(instances)
	if err != nil {
		t.Fatalf("PlanJobs returned error: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job instance, got %d", len(jobs))
	}

	var job *model.JobInstance
	for _, plannedJob := range jobs {
		job = plannedJob
	}
	if job == nil {
		t.Fatalf("expected planned job instance")
	}
	if job.Name != "ship" {
		t.Fatalf("expected overridden default job ship, got %q", job.Name)
	}
	if len(job.Steps) == 0 || job.Steps[0].Run != "echo override" {
		t.Fatalf("expected override step to be planned, got %#v", job.Steps)
	}
}

func writeCompositionPackage(t *testing.T, rootDir, packageName string, compositions map[string]packageJob) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(rootDir, "compositions"), 0o755); err != nil {
		t.Fatalf("failed to create package directory: %v", err)
	}

	names := make([]string, 0, len(compositions))
	for name := range compositions {
		names = append(names, name)
	}
	sort.Strings(names)

	exports := make([]string, 0, len(names))
	for _, name := range names {
		exports = append(exports, fmt.Sprintf("  - composition: %s\n    path: compositions/%s.yaml", name, name))
		writeFile(t, filepath.Join(rootDir, "compositions", name+".yaml"), compositionDocumentYAML(name, compositions[name]))
	}

	writeFile(t, filepath.Join(rootDir, "gluon.yaml"), fmt.Sprintf(`apiVersion: sourceplane.io/v1alpha1
kind: CompositionPackage
metadata:
  name: %s
spec:
  version: 1.0.0
  exports:
%s
`, packageName, strings.Join(exports, "\n")))
}

func compositionDocumentYAML(name string, job packageJob) string {
	return fmt.Sprintf(`apiVersion: sourceplane.io/v1alpha1
kind: Composition
metadata:
  name: %s
spec:
  type: %s
  description: %s composition
  defaultJob: %s
  inputSchema:
    $schema: http://json-schema.org/draft-07/schema#
    type: object
    properties:
      type:
        const: %s
      inputs:
        type: object
        properties:
          chart:
            type: string
        additionalProperties: true
  jobs:
    - name: %s
      description: %s job
      runsOn: %s
      timeout: 10m
      retries: 1
      steps:
        - name: %s
          run: %s
`, name, name, name, job.defaultJob, name, job.defaultJob, job.defaultJob, job.runsOn, job.defaultJob, job.stepRun)
}

func writeTarGz(t *testing.T, srcDir, archivePath string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		t.Fatalf("failed to create archive directory: %v", err)
	}

	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("failed to create archive: %v", err)
	}
	defer file.Close()

	gzipWriter := gzip.NewWriter(file)
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	err = filepath.Walk(srcDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)
		if relPath == "." {
			return nil
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = relPath
		if info.IsDir() {
			header.Name += "/"
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = tarWriter.Write(data)
		return err
	})
	if err != nil {
		t.Fatalf("failed to write archive: %v", err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed to create directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write file %s: %v", path, err)
	}
}
