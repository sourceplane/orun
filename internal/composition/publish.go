package composition

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sourceplane/orun/internal/model"
	"gopkg.in/yaml.v3"
)

// PublishPlan captures a fully-resolved publish target before any side-effecting work.
type PublishPlan struct {
	PackageRoot      string
	PackageName      string
	Version          string
	OCIRef           string
	Registry         string
	Repository       string
	FileCount        int
	InferredFromGit  bool
	InferredVersion  bool
	ManifestVersion  string
}

// FullRef returns the canonical <registry>/<repo>:<tag> form.
func (p PublishPlan) FullRef() string { return p.OCIRef }

// ResolvePackPlan loads the manifest at packageRoot and resolves a version, with no OCI target.
func ResolvePackPlan(packageRoot, versionOverride string) (*PublishPlan, error) {
	root, err := resolvePackageRoot(packageRoot)
	if err != nil {
		return nil, err
	}
	manifest, err := readPackageManifest(root)
	if err != nil {
		return nil, err
	}
	plan := &PublishPlan{
		PackageRoot:     root,
		PackageName:     manifest.Metadata.Name,
		ManifestVersion: strings.TrimSpace(manifest.Spec.Version),
	}
	plan.Version = pickVersion(plan, versionOverride, root)
	plan.InferredVersion = strings.TrimSpace(versionOverride) == ""
	files, err := countPackageFiles(root)
	if err != nil {
		return nil, err
	}
	plan.FileCount = files
	return plan, nil
}

// ResolvePublishPlan inspects packageRoot, optional explicit target, and the local git repo to assemble a PublishPlan.
// targetRef may be empty (then inferred from git remote), a registry-only host (ghcr.io), or a full <reg>/<repo>[:tag].
// versionOverride wins when non-empty.
func ResolvePublishPlan(packageRoot, targetRef, versionOverride string) (*PublishPlan, error) {
	plan, err := ResolvePackPlan(packageRoot, versionOverride)
	if err != nil {
		return nil, err
	}

	registry, repository, tag, err := resolveOCITarget(targetRef, plan.PackageRoot, plan.PackageName, plan.Version)
	if err != nil {
		return nil, err
	}
	plan.Registry = registry
	plan.Repository = repository
	plan.OCIRef = fmt.Sprintf("%s/%s:%s", registry, repository, tag)
	plan.InferredFromGit = strings.TrimSpace(targetRef) == "" || isRegistryOnly(targetRef)
	return plan, nil
}

func pickVersion(plan *PublishPlan, override, root string) string {
	if v := strings.TrimSpace(override); v != "" {
		return v
	}
	if v, ok := gitDescribeVersion(root); ok {
		return v
	}
	if plan.ManifestVersion != "" {
		return plan.ManifestVersion
	}
	return devVersion(root)
}

func resolvePackageRoot(packageRoot string) (string, error) {
	root := strings.TrimSpace(packageRoot)
	if root == "" {
		root = "."
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("failed to resolve package root: %w", err)
	}
	manifestPath := filepath.Join(abs, "orun.yaml")
	if _, err := os.Stat(manifestPath); err != nil {
		return "", fmt.Errorf("no orun.yaml found at %s (use --root to point at a composition package directory)", abs)
	}
	return abs, nil
}

func readPackageManifest(root string) (*model.CompositionPackage, error) {
	data, err := os.ReadFile(filepath.Join(root, "orun.yaml"))
	if err != nil {
		return nil, fmt.Errorf("failed to read orun.yaml: %w", err)
	}
	var manifest model.CompositionPackage
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse orun.yaml: %w", err)
	}
	if manifest.Kind != compositionPackageKind {
		return nil, fmt.Errorf("orun.yaml at %s must have kind %s (found %q)", root, compositionPackageKind, manifest.Kind)
	}
	if strings.TrimSpace(manifest.Metadata.Name) == "" {
		return nil, fmt.Errorf("orun.yaml at %s must set metadata.name", root)
	}
	return &manifest, nil
}

func resolveOCITarget(targetRef, root, packageName, version string) (registry, repository, tag string, err error) {
	target := strings.TrimSpace(targetRef)
	target = strings.TrimPrefix(target, "oci://")

	if target == "" || isRegistryOnly(target) {
		owner, repo, found := inferGitRepo(root)
		if !found {
			return "", "", "", fmt.Errorf("could not infer publish target from git remote; pass an explicit ref like ghcr.io/<owner>/<repo>")
		}
		registry = "ghcr.io"
		if target != "" {
			registry = trimSlash(target)
		}
		repository = strings.ToLower(owner + "/" + repo + "/" + packageName)
		tag = sanitizeTag(version)
		return registry, repository, tag, nil
	}

	registry, repository, tag = splitRefParts(target)
	if registry == "" || repository == "" {
		return "", "", "", fmt.Errorf("invalid OCI ref %q: expected <registry>/<repo>[:tag]", targetRef)
	}
	if tag == "" {
		tag = sanitizeTag(version)
	}
	return registry, strings.ToLower(repository), tag, nil
}

func splitRefParts(ref string) (registry, repository, tag string) {
	main := ref
	if lastSlash := strings.LastIndex(ref, "/"); lastSlash >= 0 {
		if colon := strings.LastIndex(ref[lastSlash:], ":"); colon >= 0 {
			tag = ref[lastSlash+colon+1:]
			main = ref[:lastSlash+colon]
		}
	} else if colon := strings.LastIndex(ref, ":"); colon >= 0 {
		tag = ref[colon+1:]
		main = ref[:colon]
	}
	parts := strings.SplitN(main, "/", 2)
	if len(parts) == 2 && (strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":") || parts[0] == "localhost") {
		registry = parts[0]
		repository = parts[1]
		return
	}
	if len(parts) == 2 {
		registry = "ghcr.io"
		repository = main
		return
	}
	return
}

func isRegistryOnly(target string) bool {
	target = strings.TrimSpace(target)
	target = strings.TrimPrefix(target, "oci://")
	if target == "" {
		return false
	}
	if strings.Contains(target, "/") {
		return false
	}
	return strings.Contains(target, ".") || target == "localhost" || strings.HasPrefix(target, "ghcr") || strings.HasPrefix(target, "docker") || strings.HasPrefix(target, "quay")
}

func trimSlash(s string) string { return strings.TrimRight(s, "/") }

var gitURLPattern = regexp.MustCompile(`^(?:git@|ssh://git@)([^:/]+)[:/]+(.+?)(?:\.git)?/?$`)

func inferGitRepo(startDir string) (owner, repo string, ok bool) {
	dir, err := findGitDir(startDir)
	if err != nil {
		return "", "", false
	}
	cmd := exec.Command("git", "-C", dir, "config", "--get", "remote.origin.url")
	out, err := cmd.Output()
	if err != nil {
		return "", "", false
	}
	return parseGitRemote(strings.TrimSpace(string(out)))
}

func parseGitRemote(remote string) (owner, repo string, ok bool) {
	if remote == "" {
		return "", "", false
	}
	if matches := gitURLPattern.FindStringSubmatch(remote); len(matches) == 3 {
		return splitOwnerRepo(matches[2])
	}
	if u, err := url.Parse(remote); err == nil && u.Host != "" {
		path := strings.TrimSuffix(strings.TrimPrefix(u.Path, "/"), ".git")
		return splitOwnerRepo(path)
	}
	return "", "", false
}

func splitOwnerRepo(path string) (string, string, bool) {
	path = strings.Trim(path, "/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], strings.TrimSuffix(parts[1], ".git"), true
}

func findGitDir(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no git repository above %s", startDir)
		}
		dir = parent
	}
}

func gitDescribeVersion(dir string) (string, bool) {
	gitDir, err := findGitDir(dir)
	if err != nil {
		return "", false
	}
	cmd := exec.Command("git", "-C", gitDir, "describe", "--tags", "--exact-match")
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	tag := strings.TrimSpace(string(out))
	if tag == "" {
		return "", false
	}
	return tag, true
}

func devVersion(dir string) string {
	sha := shortSHA(dir)
	if sha == "" {
		return "0.1.0-dev"
	}
	return "0.1.0-dev+" + sha
}

func shortSHA(dir string) string {
	gitDir, err := findGitDir(dir)
	if err != nil {
		return ""
	}
	cmd := exec.Command("git", "-C", gitDir, "rev-parse", "--short", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

var tagSanitize = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func sanitizeTag(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "latest"
	}
	cleaned := tagSanitize.ReplaceAllString(v, "-")
	cleaned = strings.Trim(cleaned, "-.")
	if cleaned == "" {
		return "latest"
	}
	if len(cleaned) > 128 {
		cleaned = cleaned[:128]
	}
	return cleaned
}

func countPackageFiles(root string) (int, error) {
	count := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		count++
		return nil
	})
	return count, err
}

// NormalizeOCIRef expands short-form references in intent.yaml to a full <registry>/<repo>:<tag>.
// "sourceplane/devops-compositions"        -> "ghcr.io/sourceplane/devops-compositions:latest"
// "ghcr.io/acme/x"                          -> "ghcr.io/acme/x:latest"
// "oci://ghcr.io/acme/x:v1"                 -> "ghcr.io/acme/x:v1"
func NormalizeOCIRef(ref string) string { return normalizeOCIRef(ref) }

func normalizeOCIRef(ref string) string {
	ref = strings.TrimSpace(ref)
	ref = strings.TrimPrefix(ref, "oci://")
	if ref == "" {
		return ""
	}
	registry, repository, tag := splitRefParts(ref)
	if registry == "" || repository == "" {
		return ref
	}
	if tag == "" {
		tag = "latest"
	}
	return fmt.Sprintf("%s/%s:%s", registry, repository, tag)
}

// LoginToRegistry shells out to `oras login` to store credentials.
func LoginToRegistry(registry, username, password string, passwordStdin bool) error {
	registry = strings.TrimSpace(registry)
	if registry == "" {
		return fmt.Errorf("registry is required (e.g. ghcr.io)")
	}
	if _, err := exec.LookPath("oras"); err != nil {
		return fmt.Errorf("oras CLI is required for login; install from https://oras.land")
	}

	args := []string{"login", registry}
	if strings.TrimSpace(username) != "" {
		args = append(args, "--username", username)
	}
	if passwordStdin {
		args = append(args, "--password-stdin")
	} else if strings.TrimSpace(password) != "" {
		args = append(args, "--password", password)
	}

	cmd := exec.Command("oras", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("oras login failed: %w", err)
	}
	return nil
}
