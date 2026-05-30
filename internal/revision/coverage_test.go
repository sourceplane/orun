package revision

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/statestore"
	"github.com/sourceplane/orun/internal/triggerctx"
)

// TestValidateTrigger_AllMissingFieldBranches walks every required field so
// each early-return arm in validateTrigger is exercised. The function is
// not exported; we test it indirectly through WriteRevision so we don't
// have to expose it just for coverage.
func TestValidateTrigger_AllMissingFieldBranches(t *testing.T) {
	store := newTestStore(t)
	cfg := newWriterCfg(store, time.Now().UTC())
	full := newTestTrigger(t)

	cases := []struct {
		name string
		mut  func(*triggerctx.TriggerOccurrence)
	}{
		{"TriggerID", func(o *triggerctx.TriggerOccurrence) { o.TriggerID = "" }},
		{"TriggerKey", func(o *triggerctx.TriggerOccurrence) { o.TriggerKey = "" }},
		{"TriggerName", func(o *triggerctx.TriggerOccurrence) { o.TriggerName = "" }},
		{"CreatedAt", func(o *triggerctx.TriggerOccurrence) { o.CreatedAt = time.Time{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			occ := full
			tc.mut(&occ)
			_, err := WriteRevision(context.Background(), cfg, occ, []byte(`{}`), "deadbeefcafebabe")
			if !errors.Is(err, statestore.ErrInvalid) {
				t.Errorf("missing %s: err=%v want ErrInvalid", tc.name, err)
			}
		})
	}
}

// TestSummaryFromScope_EmptyMode confirms the writer fills in PlanScopeFull
// when the trigger arrives with an empty PlanScope.Mode (legacy callers /
// migrated triggers).
func TestSummaryFromScope_EmptyMode(t *testing.T) {
	store := newTestStore(t)
	cfg := newWriterCfg(store, time.Now().UTC())
	trig := newTestTrigger(t)
	trig.PlanScope.Mode = ""
	trig.PlanScope.ChangedComponents = []string{"svc-a", "svc-b"}
	trig.PlanScope.ActiveEnvironments = nil

	rev, err := WriteRevision(context.Background(), cfg, trig, []byte(`{}`), "deadbeefcafebabe")
	if err != nil {
		t.Fatalf("WriteRevision: %v", err)
	}
	if rev.Summary.Scope != triggerctx.PlanScopeFull {
		t.Errorf("Scope=%q want %q", rev.Summary.Scope, triggerctx.PlanScopeFull)
	}
	if got := rev.Summary.ActiveEnvironments; got == nil || len(got) != 0 {
		t.Errorf("ActiveEnvironments=%v want []", got)
	}
	if len(rev.Summary.ChangedComponents) != 2 {
		t.Errorf("ChangedComponents=%v", rev.Summary.ChangedComponents)
	}
}

func TestRevisionKey_BadPlanHash(t *testing.T) {
	trig := newTestTrigger(t)
	if _, err := RevisionKey(trig, "abc"); !errors.Is(err, statestore.ErrInvalid) {
		t.Errorf("err=%v want ErrInvalid", err)
	}
}

// failingStore wraps a real store and injects a sentinel error on writes to
// a configured path prefix. We use it to drive WriteRevision down each
// "write step N failed" branch without resorting to a full mock.
type failingStore struct {
	statestore.StateStore
	failPrefix string
	err        error
}

func (f failingStore) Write(ctx context.Context, p string, b []byte, opts statestore.WriteOptions) (statestore.ObjectMeta, error) {
	if f.failPrefix != "" && hasPrefix(p, f.failPrefix) {
		return statestore.ObjectMeta{}, f.err
	}
	return f.StateStore.Write(ctx, p, b, opts)
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }

func TestWriteRevision_StepErrorsPropagate(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
	}{
		{"trigger.json", "revisions/"},   // first matching write
		{"refs/", "refs/"},                // refs writes (latest + trigger)
		{"indexes/finalize", "indexes/"}, // finalize step
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inner := newTestStore(t)
			sentinel := errors.New("disk fail: " + tc.name)
			store := failingStore{StateStore: inner, failPrefix: tc.prefix, err: sentinel}
			cfg := newWriterCfg(store, time.Now().UTC())
			trig := newTestTrigger(t)
			_, err := WriteRevision(context.Background(), cfg, trig, []byte(`{}`), "deadbeefcafebabe")
			if !errors.Is(err, sentinel) {
				t.Errorf("err=%v want %v", err, sentinel)
			}
		})
	}
}

// readErrStore returns a non-NotFound error from Read, exercising the
// updateLatestRevisionRef "read latest-revision ref" failure branch.
type readErrStore struct {
	statestore.StateStore
	err error
}

func (r readErrStore) Read(_ context.Context, _ string) ([]byte, statestore.ObjectMeta, error) {
	return nil, statestore.ObjectMeta{}, r.err
}

func TestUpdateLatestRevisionRef_ReadErrorPropagates(t *testing.T) {
	inner := newTestStore(t)
	sentinel := errors.New("permission denied")
	store := readErrStore{StateStore: inner, err: sentinel}
	cfg := newWriterCfg(store, time.Now().UTC())
	trig := newTestTrigger(t)
	_, err := WriteRevision(context.Background(), cfg, trig, []byte(`{}`), "deadbeefcafebabe")
	if !errors.Is(err, sentinel) {
		t.Errorf("err=%v want %v", err, sentinel)
	}
}

// createIfAbsentErr returns an injected error from CreateIfAbsent on the
// latest-revision ref bootstrap path; combined with NotFound from Read it
// drives updateLatestRevisionRef into the "create latest-revision ref"
// failure branch.
type bootstrapFailStore struct {
	statestore.StateStore
	createErr error
}

func (b bootstrapFailStore) Read(_ context.Context, p string) ([]byte, statestore.ObjectMeta, error) {
	if p == statestore.LatestRevisionRefPath() {
		return nil, statestore.ObjectMeta{}, statestore.ErrNotFound
	}
	return b.StateStore.Read(context.Background(), p)
}

func (b bootstrapFailStore) CreateIfAbsent(ctx context.Context, p string, body []byte) (statestore.ObjectMeta, error) {
	if p == statestore.LatestRevisionRefPath() {
		return statestore.ObjectMeta{}, b.createErr
	}
	return b.StateStore.CreateIfAbsent(ctx, p, body)
}

func TestUpdateLatestRevisionRef_CreateBootstrapErrorPropagates(t *testing.T) {
	inner := newTestStore(t)
	sentinel := errors.New("disk full")
	store := bootstrapFailStore{StateStore: inner, createErr: sentinel}
	cfg := newWriterCfg(store, time.Now().UTC())
	trig := newTestTrigger(t)
	_, err := WriteRevision(context.Background(), cfg, trig, []byte(`{}`), "deadbeefcafebabe")
	if !errors.Is(err, sentinel) {
		t.Errorf("err=%v want %v", err, sentinel)
	}
}

// casFailStore always fails CompareAndSwap with ErrConflict, so we exhaust
// the casRetryBudget and surface the budget-exhaustion error.
type casFailStore struct{ statestore.StateStore }

func (c casFailStore) CompareAndSwap(_ context.Context, _ string, _ string, _ []byte) (statestore.ObjectMeta, error) {
	return statestore.ObjectMeta{}, statestore.ErrConflict
}

func TestUpdateLatestRevisionRef_CASBudgetExhausted(t *testing.T) {
	inner := newTestStore(t)
	cfg := newWriterCfg(inner, time.Now().UTC())
	trig := newTestTrigger(t)
	if _, err := WriteRevision(context.Background(), cfg, trig, []byte(`{}`), "deadbeefcafebabe"); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	// Now wrap with the CAS-failing store and try again with a different
	// planHash so the writer reaches CAS instead of bootstrap.
	store := casFailStore{StateStore: inner}
	cfg2 := newWriterCfg(store, time.Now().UTC())
	_, err := WriteRevision(context.Background(), cfg2, trig, []byte(`{}`), "feedfacebadc0ffe")
	if !errors.Is(err, statestore.ErrConflict) {
		t.Errorf("err=%v want ErrConflict (budget exhausted)", err)
	}
}

// casOtherErrStore returns a non-conflict, non-nil error from CAS so we
// exercise the "cas latest-revision ref" generic-error branch.
type casOtherErrStore struct {
	statestore.StateStore
	err error
}

func (c casOtherErrStore) CompareAndSwap(_ context.Context, _ string, _ string, _ []byte) (statestore.ObjectMeta, error) {
	return statestore.ObjectMeta{}, c.err
}

func TestUpdateLatestRevisionRef_CASOtherError(t *testing.T) {
	inner := newTestStore(t)
	cfg := newWriterCfg(inner, time.Now().UTC())
	trig := newTestTrigger(t)
	if _, err := WriteRevision(context.Background(), cfg, trig, []byte(`{}`), "deadbeefcafebabe"); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	sentinel := errors.New("network down")
	store := casOtherErrStore{StateStore: inner, err: sentinel}
	cfg2 := newWriterCfg(store, time.Now().UTC())
	_, err := WriteRevision(context.Background(), cfg2, trig, []byte(`{}`), "feedfacebadc0ffe")
	if !errors.Is(err, sentinel) {
		t.Errorf("err=%v want %v", err, sentinel)
	}
}

// bootstrapRaceStore simulates a lost bootstrap race: Read returns
// ErrNotFound the first time, CreateIfAbsent returns ErrExists, then on the
// next iteration Read succeeds with a real ref so the CAS branch wins. This
// drives the "continue" path inside updateLatestRevisionRef's bootstrap
// arm.
type bootstrapRaceStore struct {
	statestore.StateStore
	calls int
}

func (b *bootstrapRaceStore) Read(ctx context.Context, p string) ([]byte, statestore.ObjectMeta, error) {
	if p == statestore.LatestRevisionRefPath() && b.calls == 0 {
		b.calls++
		return nil, statestore.ObjectMeta{}, statestore.ErrNotFound
	}
	return b.StateStore.Read(ctx, p)
}

func (b *bootstrapRaceStore) CreateIfAbsent(ctx context.Context, p string, body []byte) (statestore.ObjectMeta, error) {
	if p == statestore.LatestRevisionRefPath() {
		// Pretend a concurrent writer just won the bootstrap, but actually
		// land the ref so the second read can succeed.
		_, _ = b.StateStore.Write(ctx, p, []byte(`{"revisionKey":"rev-other-abcdef0-pdeadbeef","revisionId":"rev_x","planHash":"d","createdAt":"2026-05-30T00:00:00Z"}`), statestore.WriteOptions{})
		return statestore.ObjectMeta{}, statestore.ErrExists
	}
	return b.StateStore.CreateIfAbsent(ctx, p, body)
}

func TestUpdateLatestRevisionRef_BootstrapRaceRetries(t *testing.T) {
	inner := newTestStore(t)
	store := &bootstrapRaceStore{StateStore: inner}
	cfg := newWriterCfg(store, time.Now().UTC())
	trig := newTestTrigger(t)
	rev, err := WriteRevision(context.Background(), cfg, trig, []byte(`{}`), "deadbeefcafebabe")
	if err != nil {
		t.Fatalf("WriteRevision: %v", err)
	}
	latest, _, err := statestore.ReadLatestRevisionRef(context.Background(), inner)
	if err != nil {
		t.Fatalf("ReadLatestRevisionRef: %v", err)
	}
	if latest.RevisionKey != rev.RevisionKey {
		t.Errorf("after race latest=%q want %q", latest.RevisionKey, rev.RevisionKey)
	}
}

func TestEnsureStateStoreVersion_DriverError(t *testing.T) {
	inner := newTestStore(t)
	sentinel := errors.New("io fail")
	store := bootstrapFailStore{StateStore: inner, createErr: sentinel}
	// Bootstrap-fail store currently only intercepts the latest-revision
	// path; redirect by composing a tiny adapter that fails on
	// version.json instead.
	store2 := versionFailStore{StateStore: inner, err: sentinel}
	if err := EnsureStateStoreVersion(context.Background(), store2, nil); !errors.Is(err, sentinel) {
		t.Errorf("err=%v want %v", err, sentinel)
	}
	// The unused store reference keeps the bootstrap-fail wrap from being
	// flagged by static analysis without affecting test logic.
	_ = store
}

type versionFailStore struct {
	statestore.StateStore
	err error
}

func (v versionFailStore) CreateIfAbsent(ctx context.Context, p string, body []byte) (statestore.ObjectMeta, error) {
	if p == "version.json" {
		return statestore.ObjectMeta{}, v.err
	}
	return v.StateStore.CreateIfAbsent(ctx, p, body)
}

func TestConfig_ResolveDefaults_FillsZeroValues(t *testing.T) {
	c := Config{Store: newTestStore(t)}
	out := c.resolveDefaults()
	if out.Now == nil || out.NewID == nil {
		t.Fatal("resolveDefaults left Now/NewID nil")
	}
	if !out.CompatibilityWrites {
		t.Error("zero-value Config should default CompatibilityWrites=true")
	}
	if got := out.Now(); got.IsZero() {
		t.Error("default Now produced zero time")
	}
	if id := out.NewID(); id == "" || filepath.Ext(id) == ".json" {
		t.Errorf("default NewID produced suspicious id %q", id)
	}
}

func TestConfig_WithCompatibilityWrites_PreservesIntent(t *testing.T) {
	c := Config{Store: newTestStore(t)}.WithCompatibilityWrites(false)
	out := c.resolveDefaults()
	if out.CompatibilityWrites {
		t.Error("explicit-false flag was overwritten by resolveDefaults")
	}
}
