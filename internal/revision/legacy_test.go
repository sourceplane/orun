package revision

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/sourceplane/orun/internal/statestore"
)

// TestWriteLegacyNamedPlan_ByteIdentical verifies the named alias contains
// exactly the bytes the caller passed in (compatibility-and-migration.md §1
// requires byte-identical compat aliases for tooling that reads the legacy
// `.orun/plans/<name>.json` directly).
func TestWriteLegacyNamedPlan_ByteIdentical(t *testing.T) {
	store := newTestStore(t)
	want := []byte(`{"hello":"world"}` + "\n")
	if err := WriteLegacyNamedPlan(context.Background(), store, "my-plan", want); err != nil {
		t.Fatalf("WriteLegacyNamedPlan: %v", err)
	}
	got, _, err := store.Read(context.Background(), "plans/my-plan.json")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("byte mismatch:\nwant=%q\n got=%q", want, got)
	}
}

func TestWriteLegacyNamedPlan_RejectsLatestName(t *testing.T) {
	store := newTestStore(t)
	err := WriteLegacyNamedPlan(context.Background(), store, "latest", []byte("{}"))
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("got err=%v, want ErrInvalid", err)
	}
}

func TestWriteLegacyNamedPlan_RejectsBadName(t *testing.T) {
	store := newTestStore(t)
	err := WriteLegacyNamedPlan(context.Background(), store, "../escape", []byte("{}"))
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("got err=%v, want ErrInvalid", err)
	}
}

func TestWriteLegacyNamedPlan_NilStore(t *testing.T) {
	err := WriteLegacyNamedPlan(context.Background(), nil, "my-plan", []byte("{}"))
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("got err=%v, want ErrInvalid", err)
	}
}
