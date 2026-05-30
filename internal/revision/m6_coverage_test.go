package revision_test

// M6 coverage gates — targeted unit tests for previously-uncovered
// functions in package revision. Lifts the package above its 90% floor
// without lowering the gate (task-0021 §guardrails).
//
// Scope is deliberately narrow: each test exercises one branch that was
// missed by the existing functional + property suites. No new behaviour
// is being introduced; these tests pin the documented error paths so a
// future refactor cannot silently swallow them.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/revision"
	"github.com/sourceplane/orun/internal/statestore"
	"github.com/sourceplane/orun/internal/triggerctx"
)

// newStore returns a LocalStore rooted at t.TempDir(). All tests in this
// file allocate isolated stores so t.Parallel is safe.
func newStore(t *testing.T) statestore.StateStore {
	t.Helper()
	s, err := statestore.NewLocalStore(statestore.LocalConfig{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	return s
}

// ---- ScanLegacyPlanHashes ------------------------------------------------

func TestScanLegacyPlanHashes_EmptyWorkspace(t *testing.T) {
	t.Parallel()
	got, err := revision.ScanLegacyPlanHashes(context.Background(), newStore(t))
	if err != nil {
		t.Fatalf("ScanLegacyPlanHashes(empty): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("empty workspace returned %d entries; want 0", len(got))
	}
}

func TestScanLegacyPlanHashes_NilStore(t *testing.T) {
	t.Parallel()
	_, err := revision.ScanLegacyPlanHashes(context.Background(), nil)
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("nil store err = %v; want statestore.ErrInvalid", err)
	}
}

func TestScanLegacyPlanHashes_FiltersAndSorts(t *testing.T) {
	t.Parallel()
	store := newStore(t)
	ctx := context.Background()

	// Seed three valid checksums plus latest.json plus a user-named alias.
	// ScanLegacyPlanHashes must filter latest.json, skip non-hex aliases
	// silently, and return the three remaining entries sorted by checksum.
	sha := func(hex string) string {
		// Pad to planShortHashLen (8) — anything shorter is rejected
		// by normalizeLegacyChecksum.
		return hex + "0000000000000000"
	}
	seed := map[string]string{
		"plans/" + sha("aaaa") + ".json":     "{}",
		"plans/" + sha("ffff") + ".json":     "{}",
		"plans/" + sha("0001") + ".json":     "{}",
		"plans/latest.json":                  "{}",
		"plans/my-custom-named-alias.json":   "{}",
	}
	for path, body := range seed {
		if _, err := store.Write(ctx, path, []byte(body), statestore.WriteOptions{}); err != nil {
			t.Fatalf("seed %s: %v", path, err)
		}
	}

	got, err := revision.ScanLegacyPlanHashes(ctx, store)
	if err != nil {
		t.Fatalf("ScanLegacyPlanHashes: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d entries; want 3 (latest + named alias filtered): %+v", len(got), got)
	}
	// Sorted ascending by Checksum.
	for i := 1; i < len(got); i++ {
		if got[i-1].Checksum > got[i].Checksum {
			t.Fatalf("entries not sorted: %+v", got)
		}
	}
	// Every returned checksum is bare lowercase hex (no "sha256:" prefix).
	for _, e := range got {
		if strings.Contains(e.Checksum, ":") {
			t.Fatalf("checksum %q still carries prefix; normalizeLegacyChecksum did not run", e.Checksum)
		}
	}
}

// ---- WriteLegacyNamedPlan -----------------------------------------------

func TestWriteLegacyNamedPlan_NilStore(t *testing.T) {
	t.Parallel()
	err := revision.WriteLegacyNamedPlan(context.Background(), nil, "release", []byte("{}"))
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("nil store err = %v; want statestore.ErrInvalid", err)
	}
}

func TestWriteLegacyNamedPlan_RejectsReservedLatest(t *testing.T) {
	t.Parallel()
	err := revision.WriteLegacyNamedPlan(context.Background(), newStore(t), "latest", []byte("{}"))
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("latest err = %v; want statestore.ErrInvalid", err)
	}
}

func TestWriteLegacyNamedPlan_RejectsInvalidComponent(t *testing.T) {
	t.Parallel()
	err := revision.WriteLegacyNamedPlan(context.Background(), newStore(t), "../escape", []byte("{}"))
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("../escape err = %v; want statestore.ErrInvalid", err)
	}
}

func TestWriteLegacyNamedPlan_HappyPath(t *testing.T) {
	t.Parallel()
	store := newStore(t)
	ctx := context.Background()
	body := []byte(`{"hello":"world"}`)
	if err := revision.WriteLegacyNamedPlan(ctx, store, "release-1", body); err != nil {
		t.Fatalf("WriteLegacyNamedPlan: %v", err)
	}
	got, _, err := store.Read(ctx, "plans/release-1.json")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("body mismatch: got %s want %s", got, body)
	}
}

// ---- RevisionKey input validation ---------------------------------------

func TestRevisionKey_RejectsEmptyTriggerKey(t *testing.T) {
	t.Parallel()
	// Zero-value TriggerOccurrence has empty TriggerKey; scopePart's
	// validator must reject it (the "scope=…" prefix is empty).
	_, err := revision.RevisionKey(triggerctx.TriggerOccurrence{}, "deadbeef00")
	if err == nil {
		t.Fatal("RevisionKey(zero trigger) returned nil err; want validation failure")
	}
}

func TestRevisionKey_RejectsShortPlanHash(t *testing.T) {
	t.Parallel()
	trig := triggerctx.TriggerOccurrence{
		Source: triggerctx.TriggerSource{
			SourceScope:  "demo",
			HeadRevision: strings.Repeat("a", 40),
			WorkingTree:  triggerctx.WorkingTreeClean,
		},
	}
	trig.TriggerKey = triggerctx.TriggerKey(trig)
	// 4 hex chars is below planShortHashLen (8) — PlanShortHash rejects.
	_, err := revision.RevisionKey(trig, "abcd")
	if err == nil {
		t.Fatal("RevisionKey(short planHash) returned nil err")
	}
}
