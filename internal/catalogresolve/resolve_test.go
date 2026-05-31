package catalogresolve

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// fixturePath returns an absolute path to a testdata sub-fixture rooted
// at the test source's directory. Using runtime.Caller keeps the tests
// runnable regardless of cwd.
func fixturePath(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "testdata", name)
}

func TestDiscoverAndLoad_Canonical_HappyPath(t *testing.T) {
	root := fixturePath(t, "canonical")
	res, err := DiscoverAndLoad(context.Background(), Options{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("DiscoverAndLoad: %v", err)
	}
	if got, want := len(res.Manifests), 2; got != want {
		t.Fatalf("manifests = %d, want %d (got: %+v)", got, want, manifestPaths(res.Manifests))
	}
	if res.IntentPath != "intent.yaml" {
		t.Errorf("IntentPath = %q, want %q", res.IntentPath, "intent.yaml")
	}
	want := []string{
		"apps/api-edge/component.yaml",
		"apps/identity-worker/component.yaml",
	}
	if got := manifestPaths(res.Manifests); !reflect.DeepEqual(got, want) {
		t.Errorf("manifest paths:\n got %v\nwant %v", got, want)
	}
}

func TestDiscoverAndLoad_DefaultExcludes_NodeModules(t *testing.T) {
	root := fixturePath(t, "canonical")
	res, err := DiscoverAndLoad(context.Background(), Options{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("DiscoverAndLoad: %v", err)
	}
	for _, m := range res.Manifests {
		if strings.Contains(m.SourceFile, "node_modules") {
			t.Errorf("node_modules manifest leaked into discovery: %s", m.SourceFile)
		}
	}
}

func TestDiscoverAndLoad_IntentExcludeRespected(t *testing.T) {
	root := fixturePath(t, "canonical")
	res, err := DiscoverAndLoad(context.Background(), Options{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("DiscoverAndLoad: %v", err)
	}
	for _, m := range res.Manifests {
		if strings.HasPrefix(m.SourceFile, "fixtures-skip/") {
			t.Errorf("fixtures-skip not pruned by intent exclude: %s", m.SourceFile)
		}
	}
}

func TestDiscoverAndLoad_InheritFillsMissingFields(t *testing.T) {
	root := fixturePath(t, "canonical")
	res, err := DiscoverAndLoad(context.Background(), Options{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("DiscoverAndLoad: %v", err)
	}
	// identity-worker authored neither lifecycle nor owner — both come
	// from intent.yaml catalog.defaults.
	var iw *AuthoredManifest
	for i := range res.Manifests {
		if strings.HasSuffix(res.Manifests[i].SourceFile, "identity-worker/component.yaml") {
			iw = &res.Manifests[i]
		}
	}
	if iw == nil {
		t.Fatal("identity-worker manifest not found")
	}
	if iw.Component.Spec.Lifecycle != "experimental" {
		t.Errorf("Spec.Lifecycle = %q, want \"experimental\" (from intent defaults)", iw.Component.Spec.Lifecycle)
	}
	if iw.Component.Spec.Owner != "team/platform" {
		t.Errorf("Spec.Owner = %q, want \"team/platform\"", iw.Component.Spec.Owner)
	}
	// Provenance must point at the intent file.
	if p := iw.Provenance["spec.lifecycle"]; p.File != "intent.yaml" || p.Pointer != "/catalog/defaults/lifecycle" {
		t.Errorf("spec.lifecycle provenance = %+v, want intent.yaml /catalog/defaults/lifecycle", p)
	}
	// Authored label `tier: critical` survives; intent-default label
	// `repo: orun` is filled in.
	if got := iw.Component.Metadata.Labels["tier"]; got != "critical" {
		t.Errorf("authored label tier = %q, want \"critical\"", got)
	}
	if got := iw.Component.Metadata.Labels["repo"]; got != "orun" {
		t.Errorf("inherited label repo = %q, want \"orun\"", got)
	}
	if p := iw.Provenance["metadata.labels.repo"]; p.File != "intent.yaml" {
		t.Errorf("metadata.labels.repo provenance file = %q, want intent.yaml", p.File)
	}
	// The authored-tier label should retain authored provenance.
	if p := iw.Provenance["metadata.labels.tier"]; !strings.HasSuffix(p.File, "identity-worker/component.yaml") {
		t.Errorf("metadata.labels.tier provenance file = %q, want authored manifest", p.File)
	}
}

func TestDiscoverAndLoad_ExplicitWinsOverDefaults(t *testing.T) {
	root := fixturePath(t, "canonical")
	res, err := DiscoverAndLoad(context.Background(), Options{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("DiscoverAndLoad: %v", err)
	}
	// api-edge authored owner = "team/platform-edge"; intent default
	// owner = "team/platform". Authored must win.
	var ae *AuthoredManifest
	for i := range res.Manifests {
		if strings.HasSuffix(res.Manifests[i].SourceFile, "api-edge/component.yaml") {
			ae = &res.Manifests[i]
		}
	}
	if ae == nil {
		t.Fatal("api-edge manifest not found")
	}
	if ae.Component.Spec.Owner != "team/platform-edge" {
		t.Errorf("authored owner clobbered: got %q, want \"team/platform-edge\"", ae.Component.Spec.Owner)
	}
	// Provenance should point at the authored file, not the intent.
	if p := ae.Provenance["spec.owner"]; !strings.HasSuffix(p.File, "api-edge/component.yaml") {
		t.Errorf("spec.owner provenance file = %q, want authored manifest", p.File)
	}
}

func TestDiscoverAndLoad_MixedExtensionRejected(t *testing.T) {
	root := fixturePath(t, "mixed_extension")
	_, err := DiscoverAndLoad(context.Background(), Options{WorkspaceRoot: root})
	if err == nil {
		t.Fatal("expected ErrManifestMixedExtension, got nil")
	}
	var mix *ErrManifestMixedExtension
	if !errors.As(err, &mix) {
		t.Fatalf("error type = %T, want *ErrManifestMixedExtension: %v", err, err)
	}
	if mix.Dir != "apps/dup" {
		t.Errorf("Dir = %q, want \"apps/dup\"", mix.Dir)
	}
	if len(mix.Paths) != 2 {
		t.Errorf("Paths len = %d, want 2 (%v)", len(mix.Paths), mix.Paths)
	}
}

func TestDiscoverAndLoad_SchemaInvalidReportsPointer(t *testing.T) {
	root := fixturePath(t, "schema_invalid")
	_, err := DiscoverAndLoad(context.Background(), Options{WorkspaceRoot: root})
	if err == nil {
		t.Fatal("expected ErrManifestInvalid, got nil")
	}
	var bad *ErrManifestInvalid
	if !errors.As(err, &bad) {
		t.Fatalf("error type = %T, want *ErrManifestInvalid: %v", err, err)
	}
	if bad.File != "apps/bad/component.yaml" {
		t.Errorf("File = %q, want \"apps/bad/component.yaml\"", bad.File)
	}
	if bad.Pointer == "" {
		t.Errorf("Pointer empty; want non-empty JSON pointer (got reason=%q)", bad.Reason)
	}
}

func TestDiscoverAndLoad_MissingIntent_StillWorks(t *testing.T) {
	root := fixturePath(t, "no_intent")
	res, err := DiscoverAndLoad(context.Background(), Options{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("DiscoverAndLoad: %v", err)
	}
	if res.IntentPath != "" {
		t.Errorf("IntentPath = %q, want empty", res.IntentPath)
	}
	if len(res.Manifests) != 1 {
		t.Fatalf("manifests = %d, want 1", len(res.Manifests))
	}
	// No intent → no inheritance. Authored unset stays unset.
	if got := res.Manifests[0].Component.Spec.Lifecycle; got != "" {
		t.Errorf("Spec.Lifecycle = %q, want empty (no intent defaults)", got)
	}
}

func TestDiscoverAndLoad_EmptyWorkspaceRootRejected(t *testing.T) {
	_, err := DiscoverAndLoad(context.Background(), Options{WorkspaceRoot: ""})
	var werr *ErrWorkspaceInvalid
	if !errors.As(err, &werr) {
		t.Fatalf("error type = %T, want *ErrWorkspaceInvalid: %v", err, err)
	}
}

func TestDiscoverAndLoad_NonexistentWorkspaceRejected(t *testing.T) {
	_, err := DiscoverAndLoad(context.Background(), Options{WorkspaceRoot: filepath.Join(t.TempDir(), "does-not-exist")})
	var werr *ErrWorkspaceInvalid
	if !errors.As(err, &werr) {
		t.Fatalf("error type = %T, want *ErrWorkspaceInvalid: %v", err, err)
	}
}

func TestDiscoverAndLoad_MalformedIntentRejected(t *testing.T) {
	root := fixturePath(t, "bad_intent")
	_, err := DiscoverAndLoad(context.Background(), Options{WorkspaceRoot: root})
	var bad *ErrIntentInvalid
	if !errors.As(err, &bad) {
		t.Fatalf("error type = %T, want *ErrIntentInvalid: %v", err, err)
	}
}

// Mini-T-RES-1: two consecutive DiscoverAndLoad calls on the same
// fixture must produce byte-identical structured output.
func TestDiscoverAndLoad_Determinism(t *testing.T) {
	root := fixturePath(t, "canonical")
	a, err := DiscoverAndLoad(context.Background(), Options{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	b, err := DiscoverAndLoad(context.Background(), Options{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	enc := func(r DiscoveryResult) string {
		// Use the catalogmodel canonical encoder so map-key ordering is
		// stable across runs — the same encoder the C2 second PR will
		// hand to manifestHash.
		out, encErr := catalogmodel.CanonicalEncodeString(r)
		if encErr != nil {
			t.Fatalf("CanonicalEncodeString: %v", encErr)
		}
		return out
	}
	if got, want := enc(a), enc(b); got != want {
		t.Fatalf("non-deterministic DiscoveryResult:\n a=%s\n b=%s", got, want)
	}
}

func TestDiscoverAndLoad_EmptyListIsExplicitSet(t *testing.T) {
	root := fixturePath(t, "explicit_empty_list")
	res, err := DiscoverAndLoad(context.Background(), Options{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("DiscoverAndLoad: %v", err)
	}
	if len(res.Manifests) != 1 {
		t.Fatalf("manifests = %d, want 1", len(res.Manifests))
	}
	m := res.Manifests[0]
	// Authored providesApis: [] — explicit empty list. Even though the
	// intent has no providesApis defaults yet, the type-level
	// distinction must be preserved: the field is non-nil.
	if m.Component.Spec.ProvidesAPIs == nil {
		t.Errorf("ProvidesAPIs is nil; explicit `[]` must round-trip as a non-nil zero-length slice")
	}
	if got := len(m.Component.Spec.ProvidesAPIs); got != 0 {
		t.Errorf("ProvidesAPIs len = %d, want 0", got)
	}
	// Provenance must include the field, even though it's empty.
	if _, ok := m.Provenance["spec.providesApis"]; !ok {
		t.Errorf("Provenance[\"spec.providesApis\"] missing; explicit empty list must record provenance")
	}
}

func TestDiscoverAndLoad_YAMLMalformedReportsTyped(t *testing.T) {
	root := fixturePath(t, "yaml_malformed")
	_, err := DiscoverAndLoad(context.Background(), Options{WorkspaceRoot: root})
	var bad *ErrManifestInvalid
	if !errors.As(err, &bad) {
		t.Fatalf("error type = %T, want *ErrManifestInvalid: %v", err, err)
	}
	if !strings.Contains(bad.Reason, "yaml decode") {
		t.Errorf("Reason = %q, want substring \"yaml decode\"", bad.Reason)
	}
}

// helper

func manifestPaths(ms []AuthoredManifest) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.SourceFile
	}
	return out
}
