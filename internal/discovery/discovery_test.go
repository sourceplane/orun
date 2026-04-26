package discovery

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sourceplane/gluon/internal/model"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create directory %s: %v", dir, err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write file %s: %v", path, err)
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed in %s: %v\n%s", dir, err, out)
	}
}

func TestFindIntentFile_InCurrentDir(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	writeFile(t, filepath.Join(root, "intent.yaml"), "kind: Intent")

	path, dir, err := FindIntentFile(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if filepath.Base(path) != "intent.yaml" {
		t.Errorf("expected intent.yaml, got %s", path)
	}
	absRoot, _ := filepath.Abs(root)
	if resolved, evalErr := filepath.EvalSymlinks(absRoot); evalErr == nil {
		absRoot = resolved
	}
	if dir != absRoot {
		t.Errorf("expected dir %s, got %s", absRoot, dir)
	}
}

func TestFindIntentFile_WalksUp(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	writeFile(t, filepath.Join(root, "intent.yaml"), "kind: Intent")

	subdir := filepath.Join(root, "services", "api", "src")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}

	path, _, err := FindIntentFile(subdir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if filepath.Base(path) != "intent.yaml" {
		t.Errorf("expected intent.yaml, got %s", path)
	}
}

func TestFindIntentFile_StopsAtGitRoot(t *testing.T) {
	// Create a parent dir with intent.yaml but NO git repo
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "intent.yaml"), "kind: Intent")

	// Create a sub-directory that IS a git repo (its own root)
	subRepo := filepath.Join(root, "sub")
	if err := os.MkdirAll(subRepo, 0755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, subRepo)

	// Verify sub is actually its own git root
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = subRepo
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("could not verify git root in sub dir: %v", err)
	}
	t.Logf("sub repo git root: %s", string(out))

	subdir := filepath.Join(subRepo, "deep", "nested")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}

	// FindIntentFile from deep/nested should stop at subRepo (the git root)
	// and NOT find the intent.yaml in root (which is above the git boundary)
	_, _, err = FindIntentFile(subdir)
	if err == nil {
		t.Fatal("expected error when intent.yaml is above git root")
	}
}

func TestFindIntentFile_NotFound(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)

	_, _, err := FindIntentFile(root)
	if err == nil {
		t.Fatal("expected error when no intent.yaml exists")
	}
}

func TestFindIntentFile_YmlVariant(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	writeFile(t, filepath.Join(root, "intent.yml"), "kind: Intent")

	path, _, err := FindIntentFile(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if filepath.Base(path) != "intent.yml" {
		t.Errorf("expected intent.yml, got %s", path)
	}
}

func TestFindIntentFile_PrefersYamlOverYml(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	writeFile(t, filepath.Join(root, "intent.yaml"), "kind: Intent")
	writeFile(t, filepath.Join(root, "intent.yml"), "kind: Intent")

	path, _, err := FindIntentFile(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if filepath.Base(path) != "intent.yaml" {
		t.Errorf("expected intent.yaml to be preferred, got %s", path)
	}
}

func TestDetectComponentContext_ExactMatch(t *testing.T) {
	components := []model.Component{
		{Name: "api", Path: "services/api"},
	}

	name, err := DetectComponentContext("/repo/services/api", "/repo", components)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "api" {
		t.Errorf("expected 'api', got %q", name)
	}
}

func TestDetectComponentContext_SubdirMatch(t *testing.T) {
	components := []model.Component{
		{Name: "api", Path: "services/api"},
	}

	name, err := DetectComponentContext("/repo/services/api/src/handlers", "/repo", components)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "api" {
		t.Errorf("expected 'api', got %q", name)
	}
}

func TestDetectComponentContext_LongestPrefix(t *testing.T) {
	components := []model.Component{
		{Name: "services", Path: "services"},
		{Name: "api", Path: "services/api"},
	}

	name, err := DetectComponentContext("/repo/services/api/src", "/repo", components)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "api" {
		t.Errorf("expected 'api' (longest prefix), got %q", name)
	}
}

func TestDetectComponentContext_SkipsDotSlash(t *testing.T) {
	components := []model.Component{
		{Name: "root-comp", Path: "./"},
		{Name: "api", Path: "services/api"},
	}

	name, err := DetectComponentContext("/repo/other", "/repo", components)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "" {
		t.Errorf("expected empty (root-comp should be skipped), got %q", name)
	}
}

func TestDetectComponentContext_NoMatch(t *testing.T) {
	components := []model.Component{
		{Name: "api", Path: "services/api"},
		{Name: "web", Path: "services/web"},
	}

	name, err := DetectComponentContext("/repo/infra/network", "/repo", components)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "" {
		t.Errorf("expected empty string for no match, got %q", name)
	}
}

func TestDetectComponentContext_CwdOutsideIntentDir(t *testing.T) {
	components := []model.Component{
		{Name: "api", Path: "services/api"},
	}

	name, err := DetectComponentContext("/other/path", "/repo", components)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "" {
		t.Errorf("expected empty string for CWD outside intent dir, got %q", name)
	}
}

func TestDetectComponentContext_EmptyComponents(t *testing.T) {
	name, err := DetectComponentContext("/repo/services/api", "/repo", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "" {
		t.Errorf("expected empty string for no components, got %q", name)
	}
}
