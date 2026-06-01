package executionstate

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/catalogstore"
	"github.com/sourceplane/orun/internal/revision"
	"github.com/sourceplane/orun/internal/statestore"
	"github.com/sourceplane/orun/internal/testfx/statefs"
	"github.com/sourceplane/orun/internal/triggerctx"
)

// newWriterFixture builds a workspace with a written revision + manifest so
// CreateExecution / MarkTerminal have a manifest to update. Returns
// (cfg, revKey, trig).
func newWriterFixture(t *testing.T) (Config, string, triggerctx.TriggerOccurrence) {
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
	revCfg.JobCount = 3

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
	return cfg, rev.RevisionKey, occ
}

func TestSanitizeExecID_Alphabet(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"run-001", "run-001"},
		{"Run_001", "run-001"},
		{"my.exec.id", "my-exec-id"},
		{"  spaced  ", "spaced"},
		{"weird/chars*here!", "weird-chars-here"},
		{"a___b", "a-b"},
		{"---trim---", "trim"},
		{"UPPER", "upper"},
		{strings.Repeat("a", 100), strings.Repeat("a", sanitizeExecIDMaxLen)},
	}
	for _, c := range cases {
		got, err := SanitizeExecID(c.in)
		if err != nil {
			t.Errorf("SanitizeExecID(%q) err: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("SanitizeExecID(%q) = %q want %q", c.in, got, c.want)
		}
	}
}

func TestSanitizeExecID_Empty(t *testing.T) {
	for _, in := range []string{"", "***", "----"} {
		_, err := SanitizeExecID(in)
		if !errors.Is(err, statestore.ErrInvalid) {
			t.Errorf("SanitizeExecID(%q) err=%v want ErrInvalid", in, err)
		}
	}
}

func TestNextExecutionKey_FreshAndIncrement(t *testing.T) {
	cfg, revKey, _ := newWriterFixture(t)
	got, err := NextExecutionKey(context.Background(), cfg.Store, revKey)
	if err != nil {
		t.Fatalf("NextExecutionKey: %v", err)
	}
	if got != "run-001" {
		t.Fatalf("NextExecutionKey fresh=%q want run-001", got)
	}
	// Place run-005 manually; next should be run-006.
	if _, err := cfg.Store.Write(context.Background(),
		statestore.ExecutionDocPath(revKey, "run-005"),
		[]byte("{}\n"), statestore.WriteOptions{}); err != nil {
		t.Fatalf("seed run-005: %v", err)
	}
	got, err = NextExecutionKey(context.Background(), cfg.Store, revKey)
	if err != nil {
		t.Fatalf("NextExecutionKey: %v", err)
	}
	if got != "run-006" {
		t.Fatalf("NextExecutionKey post-seed=%q want run-006", got)
	}
}

func TestNextExecutionKey_StoreNil(t *testing.T) {
	if _, err := NextExecutionKey(context.Background(), nil, "rev-x"); !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("err=%v want ErrInvalid", err)
	}
}

func validInput(revKey, trigKey, trigID string) CreateExecutionInput {
	return CreateExecutionInput{
		RevisionKey: revKey,
		RevisionID:  "rev_01HZTESTDETERMINISTIC0001",
		TriggerID:   trigID,
		TriggerKey:  trigKey,
		Reason:      ReasonDirectRun,
		Status:      StatusPending,
		Runner:      RunnerProfile{Mode: "local", Backend: "local", Platform: "darwin"},
		Summary:     ExecSummary{Total: 1, Pending: 1},
	}
}

func TestCreateExecution_HappyPath(t *testing.T) {
	cfg, revKey, occ := newWriterFixture(t)
	rec, err := CreateExecution(context.Background(), cfg, validInput(revKey, occ.TriggerKey, occ.TriggerID))
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	if rec.ExecutionKey != "run-001" {
		t.Fatalf("ExecutionKey=%q want run-001", rec.ExecutionKey)
	}
	// execution.json present
	if _, _, err := cfg.Store.Read(context.Background(),
		statestore.ExecutionDocPath(revKey, "run-001")); err != nil {
		t.Fatalf("read execution.json: %v", err)
	}
	// index entry
	if _, _, err := statestore.ReadExecutionIndex(context.Background(), cfg.Store, "run-001"); err != nil {
		t.Fatalf("ReadExecutionIndex: %v", err)
	}
	// latest-execution ref
	if _, _, err := statestore.ReadLatestExecutionRef(context.Background(), cfg.Store); err != nil {
		t.Fatalf("ReadLatestExecutionRef: %v", err)
	}
	// execution-created event
	evtPath := statestore.EventPath(revKey, "run-001", 1, "execution-created")
	if _, _, err := cfg.Store.Read(context.Background(), evtPath); err != nil {
		t.Fatalf("read event: %v", err)
	}
	// manifest summary updated
	raw, _, err := cfg.Store.Read(context.Background(), statestore.ManifestPath(revKey))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if !strings.Contains(string(raw), `"latestExecutionKey": "run-001"`) {
		t.Fatalf("manifest missing latestExecutionKey: %s", raw)
	}
	if !strings.Contains(string(raw), `"latestExecutionStatus": "pending"`) {
		t.Fatalf("manifest missing latestExecutionStatus: %s", raw)
	}
}

func TestCreateExecution_CatalogParentWritesCanonicalExecution(t *testing.T) {
	cfg, revKey, occ := newWriterFixture(t)
	cfg.CatalogParent = revision.CatalogParentRef{
		SourceKey:  "src-branch-main-abcdef0",
		CatalogKey: "cat-abcdef",
	}
	rec, err := CreateExecution(context.Background(), cfg, validInput(revKey, occ.TriggerKey, occ.TriggerID))
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}

	catalogPath, err := catalogstore.CatalogExecutionDocPath(cfg.CatalogParent.SourceKey, cfg.CatalogParent.CatalogKey, revKey, rec.ExecutionKey)
	if err != nil {
		t.Fatalf("CatalogExecutionDocPath: %v", err)
	}
	if _, _, err := cfg.Store.Read(context.Background(), catalogPath); err != nil {
		t.Fatalf("read catalog execution.json: %v", err)
	}
	if _, _, err := cfg.Store.Read(context.Background(), statestore.ExecutionDocPath(revKey, rec.ExecutionKey)); err != nil {
		t.Fatalf("read global execution mirror: %v", err)
	}
	idx, _, err := statestore.ReadExecutionIndex(context.Background(), cfg.Store, rec.ExecutionKey)
	if err != nil {
		t.Fatalf("ReadExecutionIndex: %v", err)
	}
	wantDir, _ := catalogstore.CatalogExecutionDir(cfg.CatalogParent.SourceKey, cfg.CatalogParent.CatalogKey, revKey, rec.ExecutionKey)
	if idx.Path != wantDir {
		t.Fatalf("index Path = %q; want catalog dir %q", idx.Path, wantDir)
	}

	if _, err := MarkTerminal(context.Background(), cfg, revKey, rec.ExecutionKey, StatusCompleted, ExecSummary{Total: 1, Completed: 1}); err != nil {
		t.Fatalf("MarkTerminal: %v", err)
	}
	raw, _, err := cfg.Store.Read(context.Background(), catalogPath)
	if err != nil {
		t.Fatalf("read terminal catalog execution.json: %v", err)
	}
	if !strings.Contains(string(raw), `"status": "completed"`) {
		t.Fatalf("catalog execution not terminal:\n%s", raw)
	}
}

func TestWriteCatalogParentExecution_Errors(t *testing.T) {
	cfg, revKey, _ := newWriterFixture(t)
	rec := ExecutionRun{RevisionKey: revKey, ExecutionKey: "run-001"}

	err := writeCatalogParentExecution(context.Background(), cfg.Store,
		revision.CatalogParentRef{SourceKey: "bad/source", CatalogKey: "cat-abcdef"}, rec)
	if err == nil {
		t.Fatal("invalid parent unexpectedly succeeded")
	}

	sentinel := errors.New("catalog write failed")
	err = writeCatalogParentExecution(context.Background(),
		writePrefixErrStore{StateStore: cfg.Store, prefix: "sources/", err: sentinel},
		revision.CatalogParentRef{SourceKey: "src-branch-main-abcdef0", CatalogKey: "cat-abcdef"}, rec)
	if !errors.Is(err, sentinel) {
		t.Fatalf("write err=%v want %v", err, sentinel)
	}
}

func TestCreateExecution_CatalogParentInvalidIndexPath(t *testing.T) {
	cfg, revKey, occ := newWriterFixture(t)
	cfg.CatalogParent = revision.CatalogParentRef{SourceKey: "bad/source", CatalogKey: "cat-abcdef"}
	_, err := CreateExecution(context.Background(), cfg, validInput(revKey, occ.TriggerKey, occ.TriggerID))
	if err == nil {
		t.Fatal("CreateExecution unexpectedly succeeded with invalid catalog parent")
	}
}

func TestMarkTerminal_CatalogParentWriteError(t *testing.T) {
	cfg, revKey, occ := newWriterFixture(t)
	cfg.CatalogParent = revision.CatalogParentRef{SourceKey: "src-branch-main-abcdef0", CatalogKey: "cat-abcdef"}
	rec, err := CreateExecution(context.Background(), cfg, validInput(revKey, occ.TriggerKey, occ.TriggerID))
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}

	sentinel := errors.New("catalog terminal write failed")
	cfg.Store = writePrefixErrStore{StateStore: cfg.Store, prefix: "sources/", err: sentinel}
	_, err = MarkTerminal(context.Background(), cfg, revKey, rec.ExecutionKey, StatusCompleted, ExecSummary{Total: 1, Completed: 1})
	if !errors.Is(err, sentinel) {
		t.Fatalf("MarkTerminal err=%v want %v", err, sentinel)
	}
}

func TestCreateExecution_OriginalKeyIsSanitized(t *testing.T) {
	cfg, revKey, occ := newWriterFixture(t)
	in := validInput(revKey, occ.TriggerKey, occ.TriggerID)
	in.OriginalKey = "My Run!"
	rec, err := CreateExecution(context.Background(), cfg, in)
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	if rec.ExecutionKey != "my-run" {
		t.Fatalf("ExecutionKey=%q want my-run", rec.ExecutionKey)
	}
	if rec.OriginalKey != "My Run!" {
		t.Fatalf("OriginalKey=%q want raw input preserved", rec.OriginalKey)
	}
}

func TestCreateExecution_OriginalKeyDuplicateErrExists(t *testing.T) {
	cfg, revKey, occ := newWriterFixture(t)
	in := validInput(revKey, occ.TriggerKey, occ.TriggerID)
	in.OriginalKey = "fixed-key"
	if _, err := CreateExecution(context.Background(), cfg, in); err != nil {
		t.Fatalf("first: %v", err)
	}
	_, err := CreateExecution(context.Background(), cfg, in)
	if !errors.Is(err, statestore.ErrExists) {
		t.Fatalf("second err=%v want ErrExists", err)
	}
}

func TestCreateExecution_DerivedClaimRetriesOnRace(t *testing.T) {
	cfg, revKey, occ := newWriterFixture(t)
	// Pre-claim run-001 so the next CreateExecution must retry.
	if _, err := cfg.Store.CreateIfAbsent(context.Background(),
		statestore.ExecutionDocPath(revKey, "run-001"),
		[]byte("{}\n")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rec, err := CreateExecution(context.Background(), cfg, validInput(revKey, occ.TriggerKey, occ.TriggerID))
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	if rec.ExecutionKey != "run-002" {
		t.Fatalf("ExecutionKey=%q want run-002", rec.ExecutionKey)
	}
}

func TestCreateExecution_NilStore(t *testing.T) {
	_, err := CreateExecution(context.Background(), Config{}, CreateExecutionInput{RevisionKey: "rev-x"})
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("err=%v want ErrInvalid", err)
	}
}

func TestCreateExecution_ValidationFailures(t *testing.T) {
	cfg, revKey, occ := newWriterFixture(t)
	base := validInput(revKey, occ.TriggerKey, occ.TriggerID)
	muts := []func(*CreateExecutionInput){
		func(i *CreateExecutionInput) { i.RevisionKey = "" },
		func(i *CreateExecutionInput) { i.RevisionID = "" },
		func(i *CreateExecutionInput) { i.TriggerID = "" },
		func(i *CreateExecutionInput) { i.TriggerKey = "" },
		func(i *CreateExecutionInput) { i.Reason = "" },
		func(i *CreateExecutionInput) { i.Status = "" },
	}
	for i, m := range muts {
		in := base
		m(&in)
		if _, err := CreateExecution(context.Background(), cfg, in); !errors.Is(err, statestore.ErrInvalid) {
			t.Errorf("case %d err=%v want ErrInvalid", i, err)
		}
	}
}

func TestUpdateSnapshot(t *testing.T) {
	cfg, revKey, occ := newWriterFixture(t)
	rec, err := CreateExecution(context.Background(), cfg, validInput(revKey, occ.TriggerKey, occ.TriggerID))
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	rec.Status = StatusRunning
	rec.Summary = ExecSummary{Total: 1, Running: 1}
	if err := UpdateSnapshot(context.Background(), cfg, rec); err != nil {
		t.Fatalf("UpdateSnapshot: %v", err)
	}
	raw, _, err := cfg.Store.Read(context.Background(), statestore.SnapshotPath(revKey, rec.ExecutionKey))
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if !strings.Contains(string(raw), `"status": "running"`) {
		t.Fatalf("snapshot missing running status: %s", raw)
	}
	// Rewrite is allowed (last-write-wins).
	rec.Status = StatusCompleted
	if err := UpdateSnapshot(context.Background(), cfg, rec); err != nil {
		t.Fatalf("UpdateSnapshot 2: %v", err)
	}
}

func TestUpdateSnapshot_Validation(t *testing.T) {
	if err := UpdateSnapshot(context.Background(), Config{}, ExecutionRun{}); !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("nil store err=%v", err)
	}
	cfg, _, _ := newWriterFixture(t)
	if err := UpdateSnapshot(context.Background(), cfg, ExecutionRun{RevisionKey: "BAD/KEY"}); !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("bad rev err=%v", err)
	}
	if err := UpdateSnapshot(context.Background(), cfg, ExecutionRun{RevisionKey: "rev-ok", ExecutionKey: ""}); !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("empty exec err=%v", err)
	}
}

func TestMarkTerminal_HappyAndIdempotent(t *testing.T) {
	cfg, revKey, occ := newWriterFixture(t)
	rec, err := CreateExecution(context.Background(), cfg, validInput(revKey, occ.TriggerKey, occ.TriggerID))
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	final := ExecSummary{Total: 1, Completed: 1}
	updated, err := MarkTerminal(context.Background(), cfg, revKey, rec.ExecutionKey, StatusCompleted, final)
	if err != nil {
		t.Fatalf("MarkTerminal: %v", err)
	}
	if updated.Status != StatusCompleted {
		t.Fatalf("Status=%q", updated.Status)
	}
	if updated.FinishedAt == nil {
		t.Fatalf("FinishedAt nil")
	}
	// Re-call: byte-identical -> short-circuit.
	again, err := MarkTerminal(context.Background(), cfg, revKey, rec.ExecutionKey, StatusCompleted, final)
	if err != nil {
		t.Fatalf("MarkTerminal idempotent: %v", err)
	}
	if again.Status != StatusCompleted {
		t.Fatalf("idempotent Status=%q", again.Status)
	}
}

func TestMarkTerminal_RejectsNonTerminal(t *testing.T) {
	cfg, revKey, _ := newWriterFixture(t)
	if _, err := MarkTerminal(context.Background(), cfg, revKey, "run-001", StatusRunning, ExecSummary{}); !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("err=%v want ErrInvalid", err)
	}
}

func TestMarkTerminal_NilStore(t *testing.T) {
	if _, err := MarkTerminal(context.Background(), Config{}, "rev", "run-001", StatusCompleted, ExecSummary{}); !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("nil store err=%v", err)
	}
}

func TestMarkTerminal_NotFound(t *testing.T) {
	cfg, revKey, _ := newWriterFixture(t)
	_, err := MarkTerminal(context.Background(), cfg, revKey, "run-999", StatusCompleted, ExecSummary{})
	if !errors.Is(err, statestore.ErrNotFound) {
		t.Fatalf("err=%v want ErrNotFound", err)
	}
}

func TestMarkTerminal_BadKeys(t *testing.T) {
	cfg, _, _ := newWriterFixture(t)
	if _, err := MarkTerminal(context.Background(), cfg, "BAD/KEY", "run-001", StatusCompleted, ExecSummary{}); !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("bad rev err=%v", err)
	}
	cfg2, revKey, _ := newWriterFixture(t)
	if _, err := MarkTerminal(context.Background(), cfg2, revKey, "", StatusCompleted, ExecSummary{}); !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("empty exec err=%v", err)
	}
}

func TestUpdateRevisionSummary_Direct(t *testing.T) {
	cfg, revKey, _ := newWriterFixture(t)
	rec := ExecutionRun{
		RevisionKey:  revKey,
		ExecutionKey: "run-direct",
		Status:       StatusCompleted,
	}
	if err := updateRevisionSummary(context.Background(), cfg, rec); err != nil {
		t.Fatalf("updateRevisionSummary: %v", err)
	}
	// Idempotent re-call short-circuits via byte-equality inside revision pkg.
	if err := updateRevisionSummary(context.Background(), cfg, rec); err != nil {
		t.Fatalf("updateRevisionSummary 2: %v", err)
	}
}

func TestExecutionKeyFromPath(t *testing.T) {
	prefix := "revisions/rev-x/executions"
	cases := map[string]string{
		"revisions/rev-x/executions/run-001/execution.json": "run-001",
		"revisions/rev-x/executions/run-002":                "run-002",
		"revisions/other/executions/run-003":                "",
		"revisions/rev-x/executions":                        "",
	}
	for in, want := range cases {
		if got := executionKeyFromPath(prefix, in); got != want {
			t.Errorf("executionKeyFromPath(%q)=%q want %q", in, got, want)
		}
	}
}

func TestPathBase(t *testing.T) {
	if got := pathBase("a/b/c.json"); got != "c.json" {
		t.Fatalf("pathBase=%q", got)
	}
}

func TestListExecutionKeys_Sorted(t *testing.T) {
	cfg, revKey, _ := newWriterFixture(t)
	for _, k := range []string{"run-003", "run-001", "run-002"} {
		if _, err := cfg.Store.Write(context.Background(),
			statestore.ExecutionDocPath(revKey, k),
			[]byte("{}\n"), statestore.WriteOptions{}); err != nil {
			t.Fatalf("seed %s: %v", k, err)
		}
	}
	out, err := listExecutionKeys(context.Background(), cfg.Store, revKey)
	if err != nil {
		t.Fatalf("listExecutionKeys: %v", err)
	}
	want := []string{"run-001", "run-002", "run-003"}
	if strings.Join(out, ",") != strings.Join(want, ",") {
		t.Fatalf("got=%v want=%v", out, want)
	}
}

func TestConfig_ResolveDefaults(t *testing.T) {
	c := Config{Store: nil}.resolveDefaults()
	if c.Now == nil || c.NewID == nil {
		t.Fatal("defaults not applied")
	}
	if !strings.HasPrefix(c.NewID(), idPrefixExecution) {
		t.Fatal("NewID prefix")
	}
	if c.Now().IsZero() {
		t.Fatal("Now zero")
	}
}

type writePrefixErrStore struct {
	statestore.StateStore
	prefix string
	err    error
}

func (s writePrefixErrStore) Write(ctx context.Context, p string, b []byte, opts statestore.WriteOptions) (statestore.ObjectMeta, error) {
	if strings.HasPrefix(p, s.prefix) {
		return statestore.ObjectMeta{}, s.err
	}
	return s.StateStore.Write(ctx, p, b, opts)
}
