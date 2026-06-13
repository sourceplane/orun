package work

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Sealed objects — the system of proof (data-model.md §8). Epics seal into
// content-addressed SpecSnapshots an agent pulls by hash; event ranges seal into
// a WorkLedgerSegment chain that makes the audit log tamper-evident. Sealing is
// one-way (WD-7): the DB is authoritative for mutation; these objects are
// projections, addressed by the hash of their canonical bytes (invariant 6).
//
// CR-1 / invariant 1: no hot work state ever enters a sealed object. A
// SpecSnapshot carries entity envelopes (Item) and links only — never the
// StatusRow projection (status/assignees/ordering). The Item envelope has no
// hot-state fields by construction, so the snapshot is hot-state-free by type.

// SealedKind is the kind tag on a sealed object.
const (
	KindSpecSnapshot      = "SpecSnapshot"
	KindWorkLedgerSegment = "WorkLedgerSegment"
)

// SpecSnapshot is the frozen epic an agent pulls (data-model.md §8.1): the epic
// doc + its task envelopes + contracts + links, pinned to the catalog the
// affects keys resolved against and the ledger seq it reflects. No hot state.
type SpecSnapshot struct {
	Kind       string `json:"kind"`
	APIVersion string `json:"apiVersion"`
	Epic       Item   `json:"epic"`
	Tasks      []Item `json:"tasks"`
	Links      []Link `json:"links"`
	// Catalog is the CatalogSnapshot ObjectID the affects keys resolved against.
	Catalog string `json:"catalog,omitempty"`
	// LedgerSeq is the per-project event seq this snapshot reflects.
	LedgerSeq int64 `json:"ledgerSeq"`
}

// WorkLedgerSegment is a sealed, batched event range for audit/offline replay
// (data-model.md §8.2). The Prev chain links segments into a verifiable, tamper-
// evident history.
type WorkLedgerSegment struct {
	Kind       string      `json:"kind"`
	APIVersion string      `json:"apiVersion"`
	Project    string      `json:"project"`
	FromSeq    int64       `json:"fromSeq"`
	ToSeq      int64       `json:"toSeq"`
	Events     []WorkEvent `json:"events"`
	// Prev is the ObjectID of the previous segment ("" for the first).
	Prev string `json:"prev,omitempty"`
}

// ContentID returns the object id of v: "sha256:" + hex(sha256(Canonical(v))).
// Canonical encoding (sorted keys, no whitespace) makes the id depend only on
// content, identically on every machine (the git-push / Nix-cache model).
func ContentID(v any) (string, error) {
	b, err := Canonical(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// SealSpecSnapshot builds a SpecSnapshot from an epic, its tasks, and the
// work-internal + affects links. It rejects a non-Epic head and any task that
// is not a Task (only Tasks belong under an epic snapshot). The returned
// snapshot is deterministic: the same inputs always seal to the same bytes and
// thus the same ContentID (invariant 6).
func SealSpecSnapshot(epic Item, tasks []Item, links []Link, catalog string, ledgerSeq int64) (SpecSnapshot, error) {
	if epic.Kind != KindEpic {
		return SpecSnapshot{}, fmt.Errorf("%w: spec snapshot head must be an Epic, got %q", ErrInvalidArgument, epic.Kind)
	}
	for _, t := range tasks {
		if t.Kind != KindTask {
			return SpecSnapshot{}, fmt.Errorf("%w: spec snapshot member %q must be a Task, got %q", ErrInvalidArgument, t.Key, t.Kind)
		}
	}
	return SpecSnapshot{
		Kind:       KindSpecSnapshot,
		APIVersion: APIVersion,
		Epic:       epic,
		Tasks:      tasks,
		Links:      links,
		Catalog:    catalog,
		LedgerSeq:  ledgerSeq,
	}, nil
}

// ObjectID returns the content-addressed id of the snapshot.
func (s SpecSnapshot) ObjectID() (string, error) { return ContentID(s) }

// SealLedgerSegment builds a WorkLedgerSegment over an event range. Events must
// be contiguous and in seq order within [fromSeq, toSeq]; prev is the previous
// segment's ObjectID ("" for the first segment).
func SealLedgerSegment(project string, fromSeq, toSeq int64, events []WorkEvent, prev string) (WorkLedgerSegment, error) {
	if project == "" {
		return WorkLedgerSegment{}, fmt.Errorf("%w: project is required", ErrInvalidArgument)
	}
	if toSeq < fromSeq {
		return WorkLedgerSegment{}, fmt.Errorf("%w: toSeq %d < fromSeq %d", ErrInvalidArgument, toSeq, fromSeq)
	}
	for i, e := range events {
		if e.Seq < fromSeq || e.Seq > toSeq {
			return WorkLedgerSegment{}, fmt.Errorf("%w: event %d seq %d outside [%d,%d]", ErrInvalidArgument, i, e.Seq, fromSeq, toSeq)
		}
		if i > 0 && e.Seq <= events[i-1].Seq {
			return WorkLedgerSegment{}, fmt.Errorf("%w: events not strictly increasing at index %d", ErrInvalidArgument, i)
		}
	}
	return WorkLedgerSegment{
		Kind:       KindWorkLedgerSegment,
		APIVersion: APIVersion,
		Project:    project,
		FromSeq:    fromSeq,
		ToSeq:      toSeq,
		Events:     events,
		Prev:       prev,
	}, nil
}

// ObjectID returns the content-addressed id of the segment.
func (s WorkLedgerSegment) ObjectID() (string, error) { return ContentID(s) }
