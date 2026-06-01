package executionstate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/revision"
	"github.com/sourceplane/orun/internal/statestore"
	"github.com/sourceplane/orun/internal/testfx/statefs"
	"github.com/sourceplane/orun/internal/triggerctx"
)

// resolverFixture builds a workspace with one revision + one execution.
func resolverFixture(t *testing.T) (Config, string, ExecutionRun, string) {
	t.Helper()
	root := statefs.NewWorkspace(t)
	store, err := statestore.NewLocalStore(statestore.LocalConfig{Root: filepath.Join(root, ".orun")})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	now := time.Date(2026, 5, 30, 18, 0, 0, 0, time.UTC)
	occ := triggerctx.NewSystemManual(triggerctx.SystemOptions{
		Source: triggerctx.TriggerSource{
			Repo:         "git@example.com:o/r.git",
			Ref:          "refs/heads/main",
			SourceScope:  "main",
			HeadRevision: "abcdef0",
			WorkingTree:  triggerctx.WorkingTreeClean,
		},
		PlanScope: triggerctx.PlanScope{
			Mode:               triggerctx.PlanScopeFull,
			ActiveEnvironments: []string{"prod"},
		},
		Now: now,
	})
	revCfg := revision.Config{
		Store: store,
		Now:   func() time.Time { return now },
		NewID: func() string { return "rev_01HZTESTDETERMINISTIC0001" },
	}.WithCompatibilityWrites(false)
	plan := []byte(`{"apiVersion":"orun.io/v1alpha1","kind":"Plan","jobs":[]}`)
	planHash := "feedface00112233445566778899aabbccddeeff00112233"
	rev, err := revision.WriteRevision(context.Background(), revCfg, occ, plan, planHash)
	if err != nil {
		t.Fatalf("WriteRevision: %v", err)
	}
	if err := revision.WriteManifest(context.Background(), revCfg, rev, occ); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	cfg := Config{
		Store:          store,
		RevisionConfig: revCfg,
		Now:            func() time.Time { return now },
		NewID:          func() string { return "exec_01HZTESTDETERMINISTIC0001" },
	}
	rec, err := CreateExecution(context.Background(), cfg, CreateExecutionInput{
		RevisionKey: rev.RevisionKey,
		RevisionID:  rev.RevisionID,
		TriggerID:   occ.TriggerID,
		TriggerKey:  occ.TriggerKey,
		Reason:      ReasonDirectRun,
		Status:      StatusPending,
		Runner:      RunnerProfile{Mode: "local", Backend: "local", Platform: "darwin"},
		Summary:     ExecSummary{Total: 1, Pending: 1},
	})
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	return cfg, rev.RevisionKey, rec, root
}

func TestResolveExecution_NilStore(t *testing.T) {
	if _, err := ResolveExecution(context.Background(), nil, "x", "", ResolveOptions{}); !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("err=%v want ErrInvalid", err)
	}
}

func TestResolveExecution_Branch3_Latest(t *testing.T) {
	cfg, _, rec, _ := resolverFixture(t)
	for _, arg := range []string{"", "latest"} {
		ref, err := ResolveExecution(context.Background(), cfg.Store, arg, "", ResolveOptions{})
		if err != nil {
			t.Fatalf("arg=%q err=%v", arg, err)
		}
		if ref.Source != ResolveSourceLatestRef {
			t.Fatalf("arg=%q Source=%q want latest-ref", arg, ref.Source)
		}
		if ref.Execution.ExecutionKey != rec.ExecutionKey {
			t.Fatalf("ExecutionKey=%q want %q", ref.Execution.ExecutionKey, rec.ExecutionKey)
		}
	}
}

func TestResolveExecution_Branch1_ExactKey(t *testing.T) {
	cfg, _, rec, _ := resolverFixture(t)
	ref, err := ResolveExecution(context.Background(), cfg.Store, rec.ExecutionKey, "", ResolveOptions{})
	if err != nil {
		t.Fatalf("ResolveExecution: %v", err)
	}
	if ref.Source != ResolveSourceExactKey {
		t.Fatalf("Source=%q want exact-key", ref.Source)
	}
}

func TestResolveByIndexEntry_EmptyPathUsesGlobalLayout(t *testing.T) {
	cfg, revKey, rec, _ := resolverFixture(t)
	ref, err := resolveByIndexEntry(context.Background(), cfg.Store, statestore.ExecutionIndexEntry{
		ExecutionKey: rec.ExecutionKey,
		RevisionKey:  revKey,
	})
	if err != nil {
		t.Fatalf("resolveByIndexEntry: %v", err)
	}
	if ref.Execution.ExecutionKey != rec.ExecutionKey || ref.RevisionKey != revKey {
		t.Fatalf("resolved ref mismatch: %+v", ref)
	}
}

func TestResolveExecution_Branch2_RevHint(t *testing.T) {
	cfg, revKey, rec, _ := resolverFixture(t)
	ref, err := ResolveExecution(context.Background(), cfg.Store, rec.ExecutionKey, revKey, ResolveOptions{})
	if err != nil {
		t.Fatalf("ResolveExecution: %v", err)
	}
	if ref.Source != ResolveSourceRevisionScoped {
		t.Fatalf("Source=%q want revision-scoped", ref.Source)
	}
	if ref.RevisionKey != revKey {
		t.Fatalf("RevisionKey=%q want %q", ref.RevisionKey, revKey)
	}
}

func TestResolveExecution_RevHintUsesExecutionIndexPath(t *testing.T) {
	cfg, revKey, rec, _ := resolverFixture(t)
	ctx := context.Background()
	altDir := "sources/src-branch-main-abcdef0/catalogs/cat-abcdef/revisions/" + revKey + "/executions/" + rec.ExecutionKey
	raw, _, err := cfg.Store.Read(ctx, statestore.ExecutionDocPath(revKey, rec.ExecutionKey))
	if err != nil {
		t.Fatalf("read global execution: %v", err)
	}
	if _, err := cfg.Store.Write(ctx, altDir+"/execution.json", raw, statestore.WriteOptions{}); err != nil {
		t.Fatalf("write alternate execution: %v", err)
	}
	if _, err := cfg.Store.Write(ctx, statestore.ExecutionIndexPath(rec.ExecutionKey), marshalCanonicalJSON(statestore.ExecutionIndexEntry{
		ExecutionKey: rec.ExecutionKey,
		ExecutionID:  rec.ExecutionID,
		RevisionKey:  revKey,
		Status:       rec.Status,
		CreatedAt:    rec.CreatedAt,
		Path:         altDir,
	}), statestore.WriteOptions{}); err != nil {
		t.Fatalf("overwrite execution index: %v", err)
	}
	if err := cfg.Store.Delete(ctx, statestore.ExecutionDocPath(revKey, rec.ExecutionKey)); err != nil {
		t.Fatalf("delete global execution: %v", err)
	}

	ref, err := ResolveExecution(ctx, cfg.Store, rec.ExecutionKey, revKey, ResolveOptions{})
	if err != nil {
		t.Fatalf("ResolveExecution: %v", err)
	}
	if ref.Source != ResolveSourceRevisionScoped {
		t.Fatalf("Source=%q want revision-scoped", ref.Source)
	}
	if ref.Execution.ExecutionKey != rec.ExecutionKey {
		t.Fatalf("ExecutionKey=%q want %q", ref.Execution.ExecutionKey, rec.ExecutionKey)
	}
}

func TestResolveExecution_Branch2_RevHintMiss_FallsThrough(t *testing.T) {
	cfg, _, rec, _ := resolverFixture(t)
	// Use a revHint that doesn't exist; resolver should fall through and find via exact-key.
	ref, err := ResolveExecution(context.Background(), cfg.Store, rec.ExecutionKey, "rev-missing", ResolveOptions{})
	if err != nil {
		t.Fatalf("ResolveExecution: %v", err)
	}
	if ref.Source != ResolveSourceExactKey {
		t.Fatalf("Source=%q want exact-key", ref.Source)
	}
}

func TestResolveExecution_Branch4_PrefixScan(t *testing.T) {
	cfg, _, rec, _ := resolverFixture(t)
	// rec.ExecutionKey = "run-001"; "run-0" prefix should match exactly one.
	ref, err := ResolveExecution(context.Background(), cfg.Store, "run-0", "", ResolveOptions{})
	if err != nil {
		t.Fatalf("ResolveExecution: %v", err)
	}
	if ref.Source != ResolveSourcePrefixScan {
		t.Fatalf("Source=%q want prefix-scan", ref.Source)
	}
	if ref.Execution.ExecutionKey != rec.ExecutionKey {
		t.Fatalf("ExecutionKey=%q want %q", ref.Execution.ExecutionKey, rec.ExecutionKey)
	}
}

func TestResolveExecution_Branch6_AmbiguousPrefix(t *testing.T) {
	cfg, revKey, _, _ := resolverFixture(t)
	// Add a second execution so prefix "run-0" matches both run-001 and run-002.
	_, err := CreateExecution(context.Background(), cfg, CreateExecutionInput{
		RevisionKey: revKey,
		RevisionID:  "rev_01HZTESTDETERMINISTIC0001",
		TriggerID:   "trg_xxx",
		TriggerKey:  "system.manual:main:clean",
		Reason:      ReasonDirectRun,
		Status:      StatusPending,
		Runner:      RunnerProfile{Mode: "local", Backend: "local", Platform: "darwin"},
		Summary:     ExecSummary{Total: 1, Pending: 1},
	})
	if err != nil {
		t.Fatalf("CreateExecution second: %v", err)
	}
	_, err = ResolveExecution(context.Background(), cfg.Store, "run-0", "", ResolveOptions{})
	if !errors.Is(err, statestore.ErrConflict) {
		t.Fatalf("err=%v want ErrConflict", err)
	}
}

func TestResolveExecution_Branch7_NotFound(t *testing.T) {
	cfg, _, _, _ := resolverFixture(t)
	_, err := ResolveExecution(context.Background(), cfg.Store, "run-zzz", "", ResolveOptions{})
	if !errors.Is(err, statestore.ErrNotFound) {
		t.Fatalf("err=%v want ErrNotFound", err)
	}
}

func TestResolveExecution_Branch5_LegacyFallback(t *testing.T) {
	root := statefs.NewWorkspace(t)
	store, err := statestore.NewLocalStore(statestore.LocalConfig{Root: filepath.Join(root, ".orun")})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	// Synthesize legacy `.orun/executions/<execID>/execution.json`.
	legacyDir := filepath.Join(root, ".orun", "executions", "legacy-run")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("mkdir legacy: %v", err)
	}
	body := []byte(`{"status":"completed","other":"ignored"}` + "\n")
	if err := os.WriteFile(filepath.Join(legacyDir, "execution.json"), body, 0o644); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	ref, err := ResolveExecution(context.Background(), store, "legacy-run", "", ResolveOptions{
		LegacyRoot: LegacyRoot(filepath.Join(root, ".orun")),
	})
	if err != nil {
		t.Fatalf("ResolveExecution legacy: %v", err)
	}
	if ref.Source != ResolveSourceLegacyFallback {
		t.Fatalf("Source=%q want legacy-fallback", ref.Source)
	}
	if !ref.Synthesized {
		t.Fatalf("Synthesized should be true")
	}
	if ref.Execution.Status != StatusCompleted {
		t.Fatalf("Status=%q want completed", ref.Execution.Status)
	}
	if ref.Execution.TriggerKey != string(triggerctx.SystemMigrated) {
		t.Fatalf("TriggerKey=%q", ref.Execution.TriggerKey)
	}
}

func TestResolveExecution_LegacyLatestFallback(t *testing.T) {
	root := statefs.NewWorkspace(t)
	store, err := statestore.NewLocalStore(statestore.LocalConfig{Root: filepath.Join(root, ".orun")})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	legacyDir := filepath.Join(root, ".orun", "executions", "legacy-only")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("mkdir legacy: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "execution.json"),
		[]byte(`{"status":"failed"}`), 0o644); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	ref, err := ResolveExecution(context.Background(), store, "", "", ResolveOptions{
		LegacyRoot: LegacyRoot(filepath.Join(root, ".orun")),
	})
	if err != nil {
		t.Fatalf("ResolveExecution latest legacy: %v", err)
	}
	if ref.Source != ResolveSourceLegacyFallback {
		t.Fatalf("Source=%q want legacy-fallback", ref.Source)
	}
	if ref.Execution.Status != StatusFailed {
		t.Fatalf("Status=%q want failed", ref.Execution.Status)
	}
}

func TestResolveExecution_LegacyRoot_FromStoreRoot(t *testing.T) {
	root := statefs.NewWorkspace(t)
	store, err := statestore.NewLocalStore(statestore.LocalConfig{Root: filepath.Join(root, ".orun")})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	legacyDir := filepath.Join(root, ".orun", "executions", "auto-root")
	_ = os.MkdirAll(legacyDir, 0o755)
	_ = os.WriteFile(filepath.Join(legacyDir, "execution.json"),
		[]byte(`{"status":"completed"}`), 0o644)
	ref, err := ResolveExecution(context.Background(), store, "auto-root", "", ResolveOptions{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ref.Source != ResolveSourceLegacyFallback {
		t.Fatalf("Source=%q", ref.Source)
	}
}

func TestResolveExecution_LegacyInvalidComponent(t *testing.T) {
	root := statefs.NewWorkspace(t)
	store, err := statestore.NewLocalStore(statestore.LocalConfig{Root: filepath.Join(root, ".orun")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ResolveExecution(context.Background(), store, "bad/comp", "", ResolveOptions{
		LegacyRoot: LegacyRoot(filepath.Join(root, ".orun")),
	})
	if !errors.Is(err, statestore.ErrNotFound) {
		t.Fatalf("err=%v want ErrNotFound", err)
	}
}

func TestResolveExecution_LegacyMalformedJSON(t *testing.T) {
	root := statefs.NewWorkspace(t)
	store, _ := statestore.NewLocalStore(statestore.LocalConfig{Root: filepath.Join(root, ".orun")})
	legacyDir := filepath.Join(root, ".orun", "executions", "bad-json")
	_ = os.MkdirAll(legacyDir, 0o755)
	_ = os.WriteFile(filepath.Join(legacyDir, "execution.json"), []byte("not json"), 0o644)
	_, err := ResolveExecution(context.Background(), store, "bad-json", "", ResolveOptions{
		LegacyRoot: LegacyRoot(filepath.Join(root, ".orun")),
	})
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("err=%v want ErrInvalid", err)
	}
}

func TestResolveLegacyLatest_EmptyDir(t *testing.T) {
	root := statefs.NewWorkspace(t)
	_ = os.MkdirAll(filepath.Join(root, ".orun", "executions"), 0o755)
	_, err := resolveLegacyLatest(LegacyRoot(filepath.Join(root, ".orun")))
	if !errors.Is(err, statestore.ErrNotFound) {
		t.Fatalf("err=%v want ErrNotFound", err)
	}
}

func TestResolveLegacy_EmptyRoot(t *testing.T) {
	_, err := resolveLegacy("", "x")
	if !errors.Is(err, statestore.ErrNotFound) {
		t.Fatalf("err=%v want ErrNotFound", err)
	}
}

func TestLegacyRoot_String(t *testing.T) {
	if LegacyRoot("/x").String() != "/x" {
		t.Fatal("String broken")
	}
}
