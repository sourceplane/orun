package work

import (
	"errors"
	"fmt"
	"time"
)

// Mutator errors. Each mutator is the one write path (WD-3); these are the
// verdicts it returns. Callers match with errors.Is.
var (
	// ErrNotFound is returned when a mutation targets an absent entity.
	ErrNotFound = errors.New("work: entity not found")
	// ErrConflict is returned when a create would collide with an existing key.
	ErrConflict = errors.New("work: key conflict")
	// ErrInvalidArgument is returned when a mutation's arguments are malformed.
	ErrInvalidArgument = errors.New("work: invalid argument")
)

// ItemOptions carries the progressive (non-required) fields of a new entity.
type ItemOptions struct {
	Doc      string
	Parent   string
	Cycle    string
	Labels   map[string]string
	Contract *Contract
}

// timestamp renders an event/envelope time in the data model's RFC 3339 / Z
// form. The caller supplies the clock, keeping mutators pure.
func timestamp(at time.Time) string { return at.UTC().Format(time.RFC3339) }

// createdRef is the {type,id} principal reference an envelope's createdBy
// carries (data-model.md §2) — the event actor without its via surface.
func createdRef(a Actor) Actor { return Actor{Type: a.Type, ID: a.ID} }

func (s *State) newEvent(kind EventKind, subject string, by Actor, at time.Time, payload any) WorkEvent {
	ev := WorkEvent{
		EventID: NewEventID(),
		Project: s.Project,
		Subject: subject,
		Kind:    kind,
		Actor:   by,
		At:      timestamp(at),
	}
	if payload != nil {
		ev.Payload = mustMarshal(payload)
	}
	return ev
}

// CreateTask creates a Task and allocates its PREFIX-seq human key from the
// project's sequence (the Durable Object's job, WD-9). title is required; every
// other field is progressive.
func (s *State) CreateTask(title string, opts ItemOptions, by Actor, at time.Time) (WorkEvent, error) {
	key := FormatTaskKey(s.Prefix, s.nextKey+1)
	return s.createItem(KindTask, key, title, opts, by, at)
}

// CreateEpic creates an Epic keyed by slug. The slug is validated.
func (s *State) CreateEpic(slug, title string, opts ItemOptions, by Actor, at time.Time) (WorkEvent, error) {
	if err := ValidateSlug(slug); err != nil {
		return WorkEvent{}, err
	}
	return s.createItem(KindEpic, slug, title, opts, by, at)
}

// CreateInitiative creates an Initiative keyed by slug. The slug is validated.
func (s *State) CreateInitiative(slug, title string, opts ItemOptions, by Actor, at time.Time) (WorkEvent, error) {
	if err := ValidateSlug(slug); err != nil {
		return WorkEvent{}, err
	}
	return s.createItem(KindInitiative, slug, title, opts, by, at)
}

func (s *State) createItem(kind Kind, key, title string, opts ItemOptions, by Actor, at time.Time) (WorkEvent, error) {
	if err := by.Validate(); err != nil {
		return WorkEvent{}, err
	}
	if title == "" {
		return WorkEvent{}, fmt.Errorf("%w: title is required", ErrInvalidArgument)
	}
	if _, exists := s.Items[key]; exists {
		return WorkEvent{}, fmt.Errorf("%w: %q already exists in %s", ErrConflict, key, s.Project)
	}
	if opts.Contract != nil && kind != KindTask {
		return WorkEvent{}, fmt.Errorf("%w: only Tasks carry a contract", ErrInvalidArgument)
	}
	item := Item{
		APIVersion: APIVersion,
		Kind:       kind,
		ID:         NewItemID(kind),
		Key:        key,
		Project:    s.Project,
		Title:      title,
		Doc:        opts.Doc,
		Parent:     opts.Parent,
		Cycle:      opts.Cycle,
		Labels:     opts.Labels,
		Contract:   opts.Contract,
		CreatedBy:  createdRef(by),
		CreatedAt:  timestamp(at),
	}
	return s.commit(s.newEvent(EventItemCreated, key, by, at, itemCreatedPayload{Item: item}))
}

// EditItem edits an entity's title and/or doc (item_edited). At least one of
// title/doc must be non-nil.
func (s *State) EditItem(key string, title, doc *string, by Actor, at time.Time) (WorkEvent, error) {
	if err := by.Validate(); err != nil {
		return WorkEvent{}, err
	}
	if title == nil && doc == nil {
		return WorkEvent{}, fmt.Errorf("%w: nothing to edit", ErrInvalidArgument)
	}
	if _, err := s.requireItem(key); err != nil {
		return WorkEvent{}, err
	}
	return s.commit(s.newEvent(EventItemEdited, key, by, at, itemEditedPayload{Title: title, Doc: doc}))
}

// SetStatus moves an entity to a new status (status_changed). The target status
// must be in the closed set. cause records the delivery fact behind an automated
// move (optional for manual moves).
func (s *State) SetStatus(key string, to Status, cause *Cause, by Actor, at time.Time) (WorkEvent, error) {
	if err := by.Validate(); err != nil {
		return WorkEvent{}, err
	}
	if !Statuses[to] {
		return WorkEvent{}, fmt.Errorf("%w: status %q is not in the closed set", ErrInvalidArgument, to)
	}
	row, err := s.requireStatus(key)
	if err != nil {
		return WorkEvent{}, err
	}
	return s.commit(s.newEvent(EventStatusChanged, key, by, at, statusChangedPayload{From: row.Status, To: to, Cause: cause}))
}

// Assign adds a principal to an entity's assignees (assigned).
func (s *State) Assign(key, principalID string, by Actor, at time.Time) (WorkEvent, error) {
	if err := by.Validate(); err != nil {
		return WorkEvent{}, err
	}
	if principalID == "" {
		return WorkEvent{}, fmt.Errorf("%w: principal is required", ErrInvalidArgument)
	}
	if _, err := s.requireStatus(key); err != nil {
		return WorkEvent{}, err
	}
	return s.commit(s.newEvent(EventAssigned, key, by, at, assignPayload{Principal: principalID}))
}

// Unassign removes a principal from an entity's assignees (unassigned).
func (s *State) Unassign(key, principalID string, by Actor, at time.Time) (WorkEvent, error) {
	if err := by.Validate(); err != nil {
		return WorkEvent{}, err
	}
	if principalID == "" {
		return WorkEvent{}, fmt.Errorf("%w: principal is required", ErrInvalidArgument)
	}
	if _, err := s.requireStatus(key); err != nil {
		return WorkEvent{}, err
	}
	return s.commit(s.newEvent(EventUnassigned, key, by, at, assignPayload{Principal: principalID}))
}

// AddComment appends a comment to an entity (comment_added). The comment lives
// in the event log; the status projection is unchanged.
func (s *State) AddComment(key, body string, by Actor, at time.Time) (WorkEvent, error) {
	if err := by.Validate(); err != nil {
		return WorkEvent{}, err
	}
	if body == "" {
		return WorkEvent{}, fmt.Errorf("%w: comment body is required", ErrInvalidArgument)
	}
	if _, err := s.requireItem(key); err != nil {
		return WorkEvent{}, err
	}
	payload := commentAddedPayload{CommentID: "cmt_" + nextULID(), Body: body}
	return s.commit(s.newEvent(EventCommentAdded, key, by, at, payload))
}

// EditContract replaces a Task's contract (contract_edited). Only Tasks carry a
// contract.
func (s *State) EditContract(key string, c *Contract, by Actor, at time.Time) (WorkEvent, error) {
	if err := by.Validate(); err != nil {
		return WorkEvent{}, err
	}
	it, err := s.requireItem(key)
	if err != nil {
		return WorkEvent{}, err
	}
	if it.Kind != KindTask {
		return WorkEvent{}, fmt.Errorf("%w: only Tasks carry a contract", ErrInvalidArgument)
	}
	return s.commit(s.newEvent(EventContractEdited, key, by, at, contractEditedPayload{Contract: c}))
}

// AddLink records a relation edge (link_added). The edge's createdBy/createdAt
// are stamped from the actor and clock; the edge type must be in the work
// vocabulary.
func (s *State) AddLink(l Link, by Actor, at time.Time) (WorkEvent, error) {
	if err := by.Validate(); err != nil {
		return WorkEvent{}, err
	}
	l.Project = s.Project
	l.CreatedBy = createdRef(by)
	l.CreatedAt = timestamp(at)
	if err := l.Validate(); err != nil {
		return WorkEvent{}, err
	}
	return s.commit(s.newEvent(EventLinkAdded, l.From, by, at, linkPayload{Link: l}))
}

// RemoveLink removes a relation edge (link_removed). Only the edge identity
// (project, from, type, to) matters.
func (s *State) RemoveLink(l Link, by Actor, at time.Time) (WorkEvent, error) {
	if err := by.Validate(); err != nil {
		return WorkEvent{}, err
	}
	l.Project = s.Project
	if err := l.Validate(); err != nil {
		return WorkEvent{}, err
	}
	return s.commit(s.newEvent(EventLinkRemoved, l.From, by, at, linkPayload{Link: l}))
}

// Move re-parents an entity and/or sets its board ordering (moved). At least one
// of parent/boardOrder must be non-nil.
func (s *State) Move(key string, parent *string, boardOrder *float64, by Actor, at time.Time) (WorkEvent, error) {
	if err := by.Validate(); err != nil {
		return WorkEvent{}, err
	}
	if parent == nil && boardOrder == nil {
		return WorkEvent{}, fmt.Errorf("%w: nothing to move", ErrInvalidArgument)
	}
	if _, err := s.requireItem(key); err != nil {
		return WorkEvent{}, err
	}
	return s.commit(s.newEvent(EventMoved, key, by, at, movedPayload{Parent: parent, BoardOrder: boardOrder}))
}

// SetCycle assigns an entity to a project cycle (cycle_changed).
func (s *State) SetCycle(key, cycle string, by Actor, at time.Time) (WorkEvent, error) {
	if err := by.Validate(); err != nil {
		return WorkEvent{}, err
	}
	it, err := s.requireItem(key)
	if err != nil {
		return WorkEvent{}, err
	}
	return s.commit(s.newEvent(EventCycleChanged, key, by, at, cycleChangedPayload{From: it.Cycle, To: cycle}))
}

// Label sets a label on an entity (labeled).
func (s *State) Label(key, labelKey, value string, by Actor, at time.Time) (WorkEvent, error) {
	if err := by.Validate(); err != nil {
		return WorkEvent{}, err
	}
	if labelKey == "" {
		return WorkEvent{}, fmt.Errorf("%w: label key is required", ErrInvalidArgument)
	}
	if _, err := s.requireItem(key); err != nil {
		return WorkEvent{}, err
	}
	return s.commit(s.newEvent(EventLabeled, key, by, at, labeledPayload{Key: labelKey, Value: value}))
}

// Unlabel removes a label from an entity (unlabeled).
func (s *State) Unlabel(key, labelKey string, by Actor, at time.Time) (WorkEvent, error) {
	if err := by.Validate(); err != nil {
		return WorkEvent{}, err
	}
	if labelKey == "" {
		return WorkEvent{}, fmt.Errorf("%w: label key is required", ErrInvalidArgument)
	}
	if _, err := s.requireItem(key); err != nil {
		return WorkEvent{}, err
	}
	return s.commit(s.newEvent(EventUnlabeled, key, by, at, labeledPayload{Key: labelKey}))
}

// Cancel terminally cancels an entity (canceled). The human key is never freed
// (data-model.md §1).
func (s *State) Cancel(key, reason string, by Actor, at time.Time) (WorkEvent, error) {
	if err := by.Validate(); err != nil {
		return WorkEvent{}, err
	}
	row, err := s.requireStatus(key)
	if err != nil {
		return WorkEvent{}, err
	}
	return s.commit(s.newEvent(EventCanceled, key, by, at, canceledPayload{From: row.Status, Reason: reason}))
}

// Seal records that an entity's epic boundary or ledger segment was sealed into
// the object store (sealed). It touches no hot state (CR-1); the payload is the
// object id, ref, and the ledger seq the snapshot reflects.
func (s *State) Seal(key, object, ref string, ledgerSeq int64, by Actor, at time.Time) (WorkEvent, error) {
	if err := by.Validate(); err != nil {
		return WorkEvent{}, err
	}
	if object == "" {
		return WorkEvent{}, fmt.Errorf("%w: object id is required", ErrInvalidArgument)
	}
	if _, err := s.requireItem(key); err != nil {
		return WorkEvent{}, err
	}
	return s.commit(s.newEvent(EventSealed, key, by, at, sealedPayload{Object: object, Ref: ref, LedgerSeq: ledgerSeq}))
}

// Import creates an entity from an imported envelope (imported), used by
// `orun work import` (W6). The envelope is taken verbatim so imported docs
// round-trip losslessly (Q-4); the caller supplies a fully-formed Item.
func (s *State) Import(item Item, source string, by Actor, at time.Time) (WorkEvent, error) {
	if err := by.Validate(); err != nil {
		return WorkEvent{}, err
	}
	if item.Key == "" || item.Title == "" {
		return WorkEvent{}, fmt.Errorf("%w: imported item needs a key and title", ErrInvalidArgument)
	}
	if !Kinds[item.Kind] {
		return WorkEvent{}, fmt.Errorf("%w: imported item kind %q is invalid", ErrInvalidArgument, item.Kind)
	}
	if _, exists := s.Items[item.Key]; exists {
		return WorkEvent{}, fmt.Errorf("%w: %q already exists in %s", ErrConflict, item.Key, s.Project)
	}
	if item.APIVersion == "" {
		item.APIVersion = APIVersion
	}
	if item.Project == "" {
		item.Project = s.Project
	}
	return s.commit(s.newEvent(EventImported, item.Key, by, at, importedPayload{Item: item, Source: source}))
}
