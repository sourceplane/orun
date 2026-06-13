package work

import (
	"fmt"
	"sort"
)

// StatusRow is the work_status projection row (data-model.md §5): the mutable
// runtime state of one entity, derived from the event log. It is rebuildable —
// dropping every row and replaying work_events reproduces it byte-for-byte
// (invariant 2). Reduce is that replay.
type StatusRow struct {
	Project    string   `json:"project"`
	Key        string   `json:"key"`
	Status     Status   `json:"status"`
	Assignees  []string `json:"assignees"`
	BoardOrder float64  `json:"boardOrder"`
	UpdatedSeq int64    `json:"updatedSeq"`
}

// State is the in-memory projection of one project's work plane: the entity
// envelopes, the status rows, and the relation edges, plus the Durable Object's
// sequence counters. It is the Go reference the backend Worker's D1 projection
// must match.
type State struct {
	Project string
	// Prefix is the project's task-key prefix (e.g. "ORN").
	Prefix string

	Items  map[string]*Item
	Status map[string]*StatusRow
	Links  []Link

	nextSeq int64 // next event seq the DO will assign
	nextKey int64 // next task human-key sequence the DO will allocate
}

// NewState returns an empty projection for a project. prefix is validated;
// an invalid prefix returns an error.
func NewState(project, prefix string) (*State, error) {
	if project == "" {
		return nil, fmt.Errorf("%w: project is empty", ErrInvalidKey)
	}
	if err := ValidatePrefix(prefix); err != nil {
		return nil, err
	}
	return &State{
		Project: project,
		Prefix:  prefix,
		Items:   map[string]*Item{},
		Status:  map[string]*StatusRow{},
	}, nil
}

// Reduce folds an event log into a fresh projection (invariant 2). The events
// must be in seq order; each carries its own seq, which Reduce preserves rather
// than reassigning. The result must canonical-equal the live projection the
// mutators produced from the same log.
func Reduce(project, prefix string, events []WorkEvent) (*State, error) {
	s, err := NewState(project, prefix)
	if err != nil {
		return nil, err
	}
	for i, ev := range events {
		if err := ev.Validate(); err != nil {
			return nil, fmt.Errorf("event %d (%s): %w", i, ev.Kind, err)
		}
		if err := s.applyEvent(ev); err != nil {
			return nil, fmt.Errorf("event %d (%s): %w", i, ev.Kind, err)
		}
		if ev.Seq > s.nextSeq {
			s.nextSeq = ev.Seq
		}
	}
	return s, nil
}

// NextSeq reports the seq the next committed event will receive.
func (s *State) NextSeq() int64 { return s.nextSeq + 1 }

// commit assigns the event its seq, validates it, folds it into the projection,
// and advances the sequence counter — the single transaction every mutator runs
// through (invariant 3). It returns the sealed event.
func (s *State) commit(ev WorkEvent) (WorkEvent, error) {
	if err := ev.Validate(); err != nil {
		return WorkEvent{}, err
	}
	ev.Seq = s.nextSeq + 1
	if err := s.applyEvent(ev); err != nil {
		return WorkEvent{}, err
	}
	s.nextSeq = ev.Seq
	return ev, nil
}

// applyEvent folds a single (already-validated) event into the projection using
// the event's own seq. It is the heart of both commit and Reduce.
func (s *State) applyEvent(ev WorkEvent) error {
	switch ev.Kind {
	case EventItemCreated:
		var p itemCreatedPayload
		if err := decodePayload(ev.Payload, &p); err != nil {
			return err
		}
		return s.createFromEnvelope(p.Item, StatusBacklog, ev.Seq)

	case EventImported:
		var p importedPayload
		if err := decodePayload(ev.Payload, &p); err != nil {
			return err
		}
		return s.createFromEnvelope(p.Item, StatusBacklog, ev.Seq)

	case EventItemEdited:
		var p itemEditedPayload
		if err := decodePayload(ev.Payload, &p); err != nil {
			return err
		}
		it, err := s.requireItem(ev.Subject)
		if err != nil {
			return err
		}
		if p.Title != nil {
			it.Title = *p.Title
		}
		if p.Doc != nil {
			it.Doc = *p.Doc
		}
		return nil

	case EventStatusChanged:
		var p statusChangedPayload
		if err := decodePayload(ev.Payload, &p); err != nil {
			return err
		}
		row, err := s.requireStatus(ev.Subject)
		if err != nil {
			return err
		}
		row.Status = p.To
		row.UpdatedSeq = ev.Seq
		return nil

	case EventCanceled:
		row, err := s.requireStatus(ev.Subject)
		if err != nil {
			return err
		}
		row.Status = StatusCanceled
		row.UpdatedSeq = ev.Seq
		return nil

	case EventAssigned:
		var p assignPayload
		if err := decodePayload(ev.Payload, &p); err != nil {
			return err
		}
		row, err := s.requireStatus(ev.Subject)
		if err != nil {
			return err
		}
		row.Assignees = addSorted(row.Assignees, p.Principal)
		row.UpdatedSeq = ev.Seq
		return nil

	case EventUnassigned:
		var p assignPayload
		if err := decodePayload(ev.Payload, &p); err != nil {
			return err
		}
		row, err := s.requireStatus(ev.Subject)
		if err != nil {
			return err
		}
		row.Assignees = removeString(row.Assignees, p.Principal)
		row.UpdatedSeq = ev.Seq
		return nil

	case EventMoved:
		var p movedPayload
		if err := decodePayload(ev.Payload, &p); err != nil {
			return err
		}
		it, err := s.requireItem(ev.Subject)
		if err != nil {
			return err
		}
		if p.Parent != nil {
			it.Parent = *p.Parent
		}
		if p.BoardOrder != nil {
			row := s.Status[ev.Subject]
			if row != nil {
				row.BoardOrder = *p.BoardOrder
				row.UpdatedSeq = ev.Seq
			}
		}
		return nil

	case EventCycleChanged:
		var p cycleChangedPayload
		if err := decodePayload(ev.Payload, &p); err != nil {
			return err
		}
		it, err := s.requireItem(ev.Subject)
		if err != nil {
			return err
		}
		it.Cycle = p.To
		return nil

	case EventLabeled:
		var p labeledPayload
		if err := decodePayload(ev.Payload, &p); err != nil {
			return err
		}
		it, err := s.requireItem(ev.Subject)
		if err != nil {
			return err
		}
		if it.Labels == nil {
			it.Labels = map[string]string{}
		}
		it.Labels[p.Key] = p.Value
		return nil

	case EventUnlabeled:
		var p labeledPayload
		if err := decodePayload(ev.Payload, &p); err != nil {
			return err
		}
		it, err := s.requireItem(ev.Subject)
		if err != nil {
			return err
		}
		delete(it.Labels, p.Key)
		if len(it.Labels) == 0 {
			it.Labels = nil
		}
		return nil

	case EventContractEdited:
		var p contractEditedPayload
		if err := decodePayload(ev.Payload, &p); err != nil {
			return err
		}
		it, err := s.requireItem(ev.Subject)
		if err != nil {
			return err
		}
		it.Contract = p.Contract
		return nil

	case EventLinkAdded:
		var p linkPayload
		if err := decodePayload(ev.Payload, &p); err != nil {
			return err
		}
		if err := p.Link.Validate(); err != nil {
			return err
		}
		s.addLink(p.Link)
		return nil

	case EventLinkRemoved:
		var p linkPayload
		if err := decodePayload(ev.Payload, &p); err != nil {
			return err
		}
		s.removeLink(p.Link)
		return nil

	case EventCommentAdded:
		// Comments live in the event log; work_status carries no comment
		// counter (data-model.md §5), so the projection is unchanged. The
		// event still validates and is recorded.
		var p commentAddedPayload
		return decodePayload(ev.Payload, &p)

	case EventSealed:
		// Sealing records a SpecSnapshot/ledger reference; it touches no hot
		// state (CR-1). The projection is unchanged.
		var p sealedPayload
		return decodePayload(ev.Payload, &p)

	default:
		return fmt.Errorf("%w: %q", ErrUnknownEventKind, ev.Kind)
	}
}

func (s *State) createFromEnvelope(it Item, initial Status, seq int64) error {
	if _, exists := s.Items[it.Key]; exists {
		return fmt.Errorf("%w: %q already exists in %s", ErrConflict, it.Key, s.Project)
	}
	clone := it
	s.Items[it.Key] = &clone
	s.Status[it.Key] = &StatusRow{
		Project:    it.Project,
		Key:        it.Key,
		Status:     initial,
		BoardOrder: float64(seq),
		UpdatedSeq: seq,
	}
	// Keep the task-key counter consistent whether the row arrived via a live
	// mutator or a replay: parse the PREFIX-seq suffix and advance past it so a
	// post-replay CreateTask does not collide.
	if it.Kind == KindTask {
		if n := taskKeySeq(it.Key, s.Prefix); n > s.nextKey {
			s.nextKey = n
		}
	}
	return nil
}

// taskKeySeq returns the numeric sequence of a PREFIX-seq task key, or 0 if it
// does not match the project's prefix.
func taskKeySeq(key, prefix string) int64 {
	want := prefix + "-"
	if len(key) <= len(want) || key[:len(want)] != want {
		return 0
	}
	var n int64
	for _, r := range key[len(want):] {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int64(r-'0')
	}
	return n
}

func (s *State) requireItem(key string) (*Item, error) {
	it, ok := s.Items[key]
	if !ok {
		return nil, fmt.Errorf("%w: %q in %s", ErrNotFound, key, s.Project)
	}
	return it, nil
}

func (s *State) requireStatus(key string) (*StatusRow, error) {
	row, ok := s.Status[key]
	if !ok {
		return nil, fmt.Errorf("%w: %q in %s", ErrNotFound, key, s.Project)
	}
	return row, nil
}

func (s *State) addLink(l Link) {
	id := l.identity()
	for i := range s.Links {
		if s.Links[i].identity() == id {
			s.Links[i] = l // idempotent upsert
			return
		}
	}
	s.Links = append(s.Links, l)
}

func (s *State) removeLink(l Link) {
	id := l.identity()
	out := s.Links[:0]
	for _, e := range s.Links {
		if e.identity() == id {
			continue
		}
		out = append(out, e)
	}
	s.Links = out
}

func addSorted(xs []string, v string) []string {
	for _, x := range xs {
		if x == v {
			return xs
		}
	}
	xs = append(xs, v)
	sort.Strings(xs)
	return xs
}

func removeString(xs []string, v string) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		if x != v {
			out = append(out, x)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
