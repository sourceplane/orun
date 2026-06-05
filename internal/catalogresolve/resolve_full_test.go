package catalogresolve

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// --- Top-level Resolve happy path ---------------------------------------

func TestResolve_E2E_HappyPath(t *testing.T) {
	root := fixturePath(t, "resolve_e2e")
	rc, issues, err := Resolve(context.Background(), Options{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("Resolve unexpected hard error (issues=%v): %v", issues, err)
	}
	if rc == nil {
		t.Fatal("rc is nil")
	}
	if rc.Namespace != "sourceplane" {
		t.Errorf("Namespace = %q, want sourceplane", rc.Namespace)
	}
	if rc.Repo != "resolve_e2e" {
		t.Errorf("Repo = %q, want resolve_e2e", rc.Repo)
	}
	if got, want := len(rc.Manifests), 2; got != want {
		t.Fatalf("manifests = %d, want %d", got, want)
	}

	keys := make([]string, len(rc.Manifests))
	for i, m := range rc.Manifests {
		keys[i] = m.Identity.ComponentKey
	}
	wantKeys := []string{"sourceplane/resolve_e2e/api-edge", "sourceplane/resolve_e2e/identity-worker"}
	for i, k := range wantKeys {
		if keys[i] != k {
			t.Errorf("manifest[%d].ComponentKey = %q, want %q", i, keys[i], k)
		}
	}

	// api-edge inference outcomes.
	api := findByName(rc.Manifests, "api-edge")
	if api == nil {
		t.Fatal("api-edge manifest missing")
	}
	if !contains(api.Runtime.Inferred.Languages, "javascript") {
		t.Errorf("Languages missing javascript: %v", api.Runtime.Inferred.Languages)
	}
	if !contains(api.Runtime.Inferred.Languages, "typescript") {
		t.Errorf("Languages missing typescript: %v", api.Runtime.Inferred.Languages)
	}
	if !contains(api.Runtime.Inferred.PackageManagers, "pnpm") {
		t.Errorf("PackageManagers missing pnpm: %v", api.Runtime.Inferred.PackageManagers)
	}
	if !contains(api.Runtime.Inferred.Frameworks, "hono") {
		t.Errorf("Frameworks missing hono: %v", api.Runtime.Inferred.Frameworks)
	}
	if !contains(api.Runtime.Inferred.Infra, "docker") {
		t.Errorf("Infra missing docker: %v", api.Runtime.Inferred.Infra)
	}
	if api.Runtime.Files.Dockerfile == nil || *api.Runtime.Files.Dockerfile == "" {
		t.Errorf("Files.Dockerfile not set")
	}
	if api.Metadata.Description == "" {
		t.Errorf("Description not back-filled from README")
	}

	// identity-worker should pick up terraform + helm.
	iw := findByName(rc.Manifests, "identity-worker")
	if iw == nil {
		t.Fatal("identity-worker missing")
	}
	if !contains(iw.Runtime.Inferred.Infra, "terraform") {
		t.Errorf("identity-worker Infra missing terraform: %v", iw.Runtime.Inferred.Infra)
	}
	if !contains(iw.Runtime.Inferred.Infra, "helm") {
		t.Errorf("identity-worker Infra missing helm: %v", iw.Runtime.Inferred.Infra)
	}

	// Dependency resolution: api-edge → identity-worker resolved; ghost missing.
	if got, want := len(api.Spec.Dependencies.Components), 2; got != want {
		t.Fatalf("api-edge deps = %d, want %d", got, want)
	}
	var resolved, missing *catalogmodel.ComponentDependency
	for i := range api.Spec.Dependencies.Components {
		d := &api.Spec.Dependencies.Components[i]
		if d.Name == "identity-worker" {
			resolved = d
		}
		if d.Name == "ghost-service" {
			missing = d
		}
	}
	if resolved == nil || resolved.Key != "sourceplane/resolve_e2e/identity-worker" {
		t.Errorf("identity-worker dep not resolved: %+v", resolved)
	}
	if missing == nil {
		t.Errorf("ghost dep not preserved")
	}

	// Validate-stage: missing dep is a warning under default mode.
	hasMissing := false
	for _, i := range issues {
		if i.Code == "component.dependency.missing" {
			hasMissing = true
			if i.Severity != SeverityWarning {
				t.Errorf("dependency.missing severity = %v, want Warning", i.Severity)
			}
		}
	}
	if !hasMissing {
		t.Errorf("expected component.dependency.missing issue")
	}

	// manifestHash: every manifest carries a non-empty sha256:hex.
	for _, m := range rc.Manifests {
		if !strings.HasPrefix(m.Source.ManifestHash, "sha256:") {
			t.Errorf("manifestHash %q lacks sha256: prefix", m.Source.ManifestHash)
		}
	}
}

func TestResolve_Determinism(t *testing.T) {
	root := fixturePath(t, "resolve_e2e")
	a, _, err := Resolve(context.Background(), Options{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	b, _, err := Resolve(context.Background(), Options{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	enc := func(v any) string {
		s, e := catalogmodel.CanonicalEncodeString(v)
		if e != nil {
			t.Fatal(e)
		}
		return s
	}
	if enc(a) != enc(b) {
		t.Fatalf("non-deterministic ResolvedCatalog")
	}
	// And manifestHash is stable.
	for i := range a.Manifests {
		if a.Manifests[i].Source.ManifestHash != b.Manifests[i].Source.ManifestHash {
			t.Errorf("manifestHash flip on rerun: %q vs %q",
				a.Manifests[i].Source.ManifestHash, b.Manifests[i].Source.ManifestHash)
		}
	}
}

func TestResolve_ProvenanceDoesNotChangeManifestHash(t *testing.T) {
	root := fixturePath(t, "resolve_e2e")
	rc, _, err := Resolve(context.Background(), Options{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	api := findByName(rc.Manifests, "api-edge")
	if api == nil {
		t.Fatal("api-edge missing")
	}
	before := api.Source.ManifestHash
	// Mutate provenance only — must not change hash.
	api.Resolution.InheritedFrom["spec.lifecycle"] = "different-source.yaml"
	api.Resolution.InferredFrom["x"] = []string{"some/other/path"}
	after, err := manifestHash(api)
	if err != nil {
		t.Fatalf("manifestHash: %v", err)
	}
	if before != after {
		t.Errorf("provenance change altered manifestHash: %q → %q", before, after)
	}
}

func TestResolve_ManifestHashChangesOnSpecEdit(t *testing.T) {
	root := fixturePath(t, "resolve_e2e")
	rc, _, err := Resolve(context.Background(), Options{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	api := findByName(rc.Manifests, "api-edge")
	before, _ := manifestHash(api)
	api.Spec.Lifecycle = "rewritten"
	after, _ := manifestHash(api)
	if before == after {
		t.Errorf("spec edit did not change manifestHash")
	}
}

// --- Strict mode + cycle detection --------------------------------------

func TestResolve_StrictPromotesWarnings(t *testing.T) {
	root := fixturePath(t, "resolve_e2e")
	_, issues, err := Resolve(context.Background(), Options{WorkspaceRoot: root, Strict: true})
	// Strict promotes ghost-dep-missing (warn → error), so resolver
	// returns a typed ValidationIssue error.
	if err == nil {
		t.Fatal("expected strict-mode error")
	}
	var vi ValidationIssue
	if !errors.As(err, &vi) {
		t.Fatalf("error type = %T, want ValidationIssue: %v", err, err)
	}
	for _, i := range issues {
		if i.Code == "component.dependency.missing" && i.Severity != SeverityError {
			t.Errorf("strict missing-dep severity = %v, want Error", i.Severity)
		}
	}
}

func TestResolve_DeployAfterCycleAlwaysError(t *testing.T) {
	root := fixturePath(t, "resolve_cycle")
	_, issues, err := Resolve(context.Background(), Options{WorkspaceRoot: root})
	if err == nil {
		t.Fatal("expected cycle error in default mode (deploy-after is always Error)")
	}
	hasCycle := false
	for _, i := range issues {
		if i.Code == "component.dependency.cycle" && i.Severity == SeverityError {
			hasCycle = true
		}
	}
	if !hasCycle {
		t.Errorf("expected cycle Error issue: %v", issues)
	}
}

// --- Validation rule coverage -------------------------------------------

func TestResolve_OwnerAndLifecycleWarnings_NoIntent(t *testing.T) {
	root := fixturePath(t, "no_intent")
	_, issues, err := Resolve(context.Background(), Options{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	wantCodes := []string{"component.metadata.owner.missing", "component.spec.lifecycle.missing"}
	for _, want := range wantCodes {
		found := false
		for _, i := range issues {
			if i.Code == want && i.Severity == SeverityWarning {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing warn for %s in %v", want, issuesCodes(issues))
		}
	}
}

func TestResolve_DuplicateComponentKey(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/intent.yaml", "catalog:\n  namespace: ns\n")
	mustWrite(t, root+"/apps/a/component.yaml", validYAML("dup"))
	mustWrite(t, root+"/apps/b/component.yaml", validYAML("dup"))
	_, issues, err := Resolve(context.Background(), Options{WorkspaceRoot: root, Repo: "r"})
	if err == nil {
		t.Fatal("expected duplicate-key error")
	}
	hasDup := false
	for _, i := range issues {
		if i.Code == "component.key.duplicate" && i.Severity == SeverityError {
			hasDup = true
		}
	}
	if !hasDup {
		t.Errorf("expected component.key.duplicate Error: %v", issuesCodes(issues))
	}
}

func TestResolve_NameInvalid_StrictPromotes(t *testing.T) {
	// metadata.name allowed by yaml but with chars rejected by component
	// key regex (uppercase).
	root := t.TempDir()
	mustWrite(t, root+"/intent.yaml", "catalog:\n  namespace: ns\n")
	mustWrite(t, root+"/apps/a/component.yaml", validYAML("BadName"))
	_, issues, err := Resolve(context.Background(), Options{WorkspaceRoot: root, Repo: "r", Strict: true})
	if err == nil {
		t.Fatal("expected strict error on uppercase name")
	}
	hasInvalid := false
	for _, i := range issues {
		if i.Code == "component.name.invalid" && i.Severity == SeverityError {
			hasInvalid = true
		}
	}
	if !hasInvalid {
		t.Errorf("expected component.name.invalid Error: %v", issuesCodes(issues))
	}
}

// --- Targeted unit-style coverage ---------------------------------------

func TestResolveInferenceConfig_DefaultsAndOverrides(t *testing.T) {
	cfg := resolveInferenceConfig(nil)
	if !cfg.Enabled || !cfg.PackageJSON || !cfg.Dockerfile || !cfg.Terraform || !cfg.Helm || !cfg.Readme {
		t.Errorf("default cfg not all-on: %+v", cfg)
	}
	off := false
	on := true
	cfg2 := resolveInferenceConfig(&intentInference{
		Enabled:     &off,
		PackageJSON: &on, // ignored when master off
	})
	if cfg2.Enabled || cfg2.PackageJSON || cfg2.Dockerfile {
		t.Errorf("master-off did not zero per-flag: %+v", cfg2)
	}
	cfg3 := resolveInferenceConfig(&intentInference{Helm: &off, Readme: &off})
	if !cfg3.Enabled || !cfg3.PackageJSON {
		t.Errorf("master defaults clobbered: %+v", cfg3)
	}
	if cfg3.Helm || cfg3.Readme {
		t.Errorf("explicit-off ignored: %+v", cfg3)
	}
}

func TestResolveDepKey_Variants(t *testing.T) {
	keyIndex := map[string]struct{}{"ns/repo/a": {}, "ns/repo2/b": {}}
	short := map[depShortRef]string{
		{"ns", "repo", "a"}:  "ns/repo/a",
		{"ns", "repo2", "b"}: "ns/repo2/b",
	}
	if k, ok := resolveDepKey("a", "ns", "repo", keyIndex, short); !ok || k != "ns/repo/a" {
		t.Errorf("short same repo: got (%q,%v)", k, ok)
	}
	if k, ok := resolveDepKey("ns/repo2/b", "ns", "repo", keyIndex, short); !ok || k != "ns/repo2/b" {
		t.Errorf("FQ cross repo: got (%q,%v)", k, ok)
	}
	if _, ok := resolveDepKey("nope", "ns", "repo", keyIndex, short); ok {
		t.Errorf("unresolved short returned ok")
	}
	if _, ok := resolveDepKey("ns/repo/missing", "ns", "repo", keyIndex, short); ok {
		t.Errorf("unresolved FQ returned ok")
	}
}

func TestSeverity_String(t *testing.T) {
	if SeverityWarning.String() != "warning" {
		t.Errorf("warning string: %q", SeverityWarning.String())
	}
	if SeverityError.String() != "error" {
		t.Errorf("error string: %q", SeverityError.String())
	}
	if Severity(99).String() == "" {
		t.Errorf("unknown severity must return non-empty")
	}
}

func TestValidationIssue_Error(t *testing.T) {
	v := ValidationIssue{
		Severity: SeverityWarning,
		Code:     "x.y",
		File:     "f.yaml",
		Pointer:  "/a",
		Message:  "m",
	}
	got := v.Error()
	for _, sub := range []string{"warning", "x.y", "f.yaml", "/a", "m"} {
		if !strings.Contains(got, sub) {
			t.Errorf("Error() %q missing %q", got, sub)
		}
	}
}

func TestErrorTypes_Strings(t *testing.T) {
	tests := []error{
		&ErrComponentInvalid{Path: "p", Reason: "r"},
		&ErrDuplicateComponent{Key: "k", Paths: []string{"a", "b"}},
		&ErrDependencyMissing{From: "a", To: "b"},
		&ErrCycle{Path: []string{"a", "b"}, EdgeType: "calls"},
		&ErrInferenceFailed{Path: "p", Reason: "r", Underlying: errors.New("x")},
		&ErrInferenceFailed{Path: "p", Reason: "r"},
		&ErrResolverInternal{Stage: 9, Underlying: errors.New("x")},
	}
	for _, e := range tests {
		if e.Error() == "" {
			t.Errorf("empty Error() for %T", e)
		}
	}
	// Unwrap targets.
	if errors.Unwrap(&ErrInferenceFailed{Underlying: errInternalSentinel()}) == nil {
		t.Errorf("ErrInferenceFailed Unwrap nil")
	}
	if errors.Unwrap(&ErrResolverInternal{Underlying: errInternalSentinel()}) == nil {
		t.Errorf("ErrResolverInternal Unwrap nil")
	}
}

func TestClock_Default(t *testing.T) {
	c := defaultClock()
	if c == nil {
		t.Fatal("defaultClock nil")
	}
	if c.Now().IsZero() {
		t.Error("system clock returned zero time")
	}
	fc := FixedClock{}
	_ = fc.Now() // exercise method receiver
}

func TestFirstError_HasError(t *testing.T) {
	if firstError(nil) != nil {
		t.Errorf("nil → nil expected")
	}
	if hasError(nil) {
		t.Errorf("nil → false expected")
	}
	is := []ValidationIssue{
		{Severity: SeverityWarning, Code: "w"},
		{Severity: SeverityError, Code: "e"},
	}
	if !hasError(is) {
		t.Errorf("hasError missed Error")
	}
	if firstError(is).Code != "e" {
		t.Errorf("firstError wrong code")
	}
}

// --- helpers -------------------------------------------------------------

func findByName(ms []*catalogmodel.ComponentManifest, name string) *catalogmodel.ComponentManifest {
	for _, m := range ms {
		if m.Identity.Name == name {
			return m
		}
	}
	return nil
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func issuesCodes(is []ValidationIssue) []string {
	out := make([]string, len(is))
	for i, x := range is {
		out[i] = x.Code + "(" + x.Severity.String() + ")"
	}
	return out
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func validYAML(name string) string {
	return "apiVersion: orun.io/v1alpha1\nkind: Component\nmetadata:\n  name: " + name + "\nspec:\n  type: worker\n  lifecycle: production\n  owner: team/x\n"
}

func TestResolve_CarriesChangeWatches(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/intent.yaml", "catalog:\n  namespace: ns\n")
	mustWrite(t, root+"/apps/a/component.yaml",
		"apiVersion: orun.io/v1alpha1\nkind: Component\nmetadata:\n  name: a\nspec:\n  type: worker\n  change:\n    watches: [env, groups]\n")
	mustWrite(t, root+"/apps/b/component.yaml", validYAML("b")) // no watches

	rc, _, err := Resolve(context.Background(), Options{WorkspaceRoot: root, Repo: "r"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	var a, b *catalogmodel.ComponentManifest
	for _, m := range rc.Manifests {
		switch m.Identity.Name {
		case "a":
			a = m
		case "b":
			b = m
		}
	}
	if a == nil || a.Spec.Change == nil {
		t.Fatalf("component a missing resolved change block: %+v", a)
	}
	if len(a.Spec.Change.Watches) != 2 || a.Spec.Change.Watches[0] != "env" || a.Spec.Change.Watches[1] != "groups" {
		t.Errorf("a watches = %v, want [env groups]", a.Spec.Change.Watches)
	}
	// A watch-less component keeps Change nil so its manifest hash is unchanged.
	if b == nil || b.Spec.Change != nil {
		t.Errorf("component b should have nil Change, got %+v", b.Spec.Change)
	}
}

func errInternalSentinel() error { return errors.New("sentinel") }
