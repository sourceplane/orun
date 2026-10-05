package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/intentpolicy"
)

// copyExamplesFixture copies the repo's examples/ intent, its compositions,
// and every component.yaml into a temp dir, so tests can introduce policy
// violations without touching the checked-in fixture.
func copyExamplesFixture(t *testing.T) string {
	t.Helper()
	src, err := filepath.Abs(filepath.Join("..", "..", "examples"))
	if err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	err = filepath.WalkDir(src, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, _ := filepath.Rel(src, path)
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", ".orun", ".turbo", ".terraform":
				return filepath.SkipDir
			}
			return nil
		}
		if rel != "intent.yaml" && d.Name() != "component.yaml" && !strings.HasPrefix(rel, "compositions"+string(filepath.Separator)) {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		if merr := os.MkdirAll(filepath.Dir(target), 0o755); merr != nil {
			return merr
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copy examples fixture: %v", err)
	}
	return dst
}

func editFixture(t *testing.T, path, old, new string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("%s does not contain %q", path, old)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), old, new, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func validateFixture(t *testing.T, dir string) error {
	t.Helper()
	prevIntent, prevRoot, prevConfig := intentFile, intentRoot, configDir
	t.Cleanup(func() { intentFile, intentRoot, configDir = prevIntent, prevRoot, prevConfig })
	intentFile = filepath.Join(dir, "intent.yaml")
	intentRoot = dir
	configDir = ""
	var err error
	_ = captureStdout(t, func() error { err = validateFiles(); return nil })
	return err
}

func requireViolation(t *testing.T, err error, component, policy string) {
	t.Helper()
	var perr *intentpolicy.Error
	if !errors.As(err, &perr) {
		t.Fatalf("want *intentpolicy.Error, got %v", err)
	}
	for _, v := range perr.Violations {
		if v.Component == component && v.Policy == policy {
			return
		}
	}
	t.Fatalf("no %s violation for %s in:\n%v", policy, component, err)
}

func TestValidateExamplesFixturePasses(t *testing.T) {
	if err := validateFixture(t, copyExamplesFixture(t)); err != nil {
		t.Fatalf("examples fixture must validate: %v", err)
	}
}

func TestValidateRejectsComponentOverridingPinnedParameter(t *testing.T) {
	dir := copyExamplesFixture(t)
	editFixture(t, filepath.Join(dir, "intent.yaml"), `        namespacePrefix: prod-
    policies:
      requireApproval: true`, `        namespacePrefix: prod-
    policies:
      requireApproval: true
      pinnedParameters:
        terraform:
          terraformVersion: 1.10.0`)

	requireViolation(t, validateFixture(t, dir), "network-foundation", "pinnedParameters.terraformVersion")
}

func TestValidateRejectsUnknownPolicy(t *testing.T) {
	dir := copyExamplesFixture(t)
	editFixture(t, filepath.Join(dir, "intent.yaml"), `requireApproval: true`, `requireAproval: true`)

	err := validateFixture(t, dir)
	var perr *intentpolicy.Error
	if !errors.As(err, &perr) || perr.Violations[0].Policy != "requireAproval" {
		t.Fatalf("an unknown policy key must fail validate: %v", err)
	}
}

// The examples' terraform.release profile declares
// requirePinnedTerraformVersion: a range on a component riding it fails.
func TestValidateRejectsUnpinnedTerraformVersionOnReleaseProfile(t *testing.T) {
	dir := copyExamplesFixture(t)
	editFixture(t, filepath.Join(dir, "infra", "infra-1", "component.yaml"), `terraformVersion: 1.9.8`, `terraformVersion: "~> 1.9"`)

	requireViolation(t, validateFixture(t, dir), "network-foundation", "requirePinnedTerraformVersion")
}

func TestValidateRejectsRequiredProfileMismatch(t *testing.T) {
	dir := copyExamplesFixture(t)
	editFixture(t, filepath.Join(dir, "intent.yaml"), `        namespacePrefix: prod-
    policies:
      requireApproval: true`, `        namespacePrefix: prod-
    policies:
      requireApproval: true
      requireProfile: [release, deploy, verify]`)
	if err := validateFixture(t, dir); err != nil {
		t.Fatalf("every production component resolves one of the allowed profiles: %v", err)
	}

	editFixture(t, filepath.Join(dir, "intent.yaml"), `requireProfile: [release, deploy, verify]`, `requireProfile: [release, deploy]`)
	requireViolation(t, validateFixture(t, dir), "platform-sdk", "requireProfile")
}
