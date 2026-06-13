package work

import (
	"encoding/json"
	"errors"
	"fmt"
)

// EventKind is a WorkEvent kind from the closed v1 vocabulary (data-model.md
// §4.1). The set is closed per schema version; an unknown kind is a write-time
// error, never a forward-compat dumping ground (extending it is a schema rev).
type EventKind string

// The closed set of event kinds (data-model.md §4.1).
const (
	EventItemCreated    EventKind = "item_created"
	EventItemEdited     EventKind = "item_edited"
	EventStatusChanged  EventKind = "status_changed"
	EventAssigned       EventKind = "assigned"
	EventUnassigned     EventKind = "unassigned"
	EventCommentAdded   EventKind = "comment_added"
	EventLinkAdded      EventKind = "link_added"
	EventLinkRemoved    EventKind = "link_removed"
	EventContractEdited EventKind = "contract_edited"
	EventMoved          EventKind = "moved"
	EventCycleChanged   EventKind = "cycle_changed"
	EventLabeled        EventKind = "labeled"
	EventUnlabeled      EventKind = "unlabeled"
	EventSealed         EventKind = "sealed"
	EventImported       EventKind = "imported"
	EventCanceled       EventKind = "canceled"
)

// EventKinds is the closed v1 set. Membership is checked on every append.
var EventKinds = map[EventKind]bool{
	EventItemCreated:    true,
	EventItemEdited:     true,
	EventStatusChanged:  true,
	EventAssigned:       true,
	EventUnassigned:     true,
	EventCommentAdded:   true,
	EventLinkAdded:      true,
	EventLinkRemoved:    true,
	EventContractEdited: true,
	EventMoved:          true,
	EventCycleChanged:   true,
	EventLabeled:        true,
	EventUnlabeled:      true,
	EventSealed:         true,
	EventImported:       true,
	EventCanceled:       true,
}

// ErrUnknownEventKind is returned when a WorkEvent carries a kind outside the
// closed set.
var ErrUnknownEventKind = errors.New("work: unknown event kind")

// ErrInvalidEvent is returned when a WorkEvent fails structural validation.
var ErrInvalidEvent = errors.New("work: invalid event")

// WorkEvent is one entry in the append-only log (data-model.md §4). Projections
// derive from the log (invariant 2); the log is the truth.
type WorkEvent struct {
	EventID string          `json:"eventId"`
	Project string          `json:"project"`
	Subject string          `json:"subject"`
	Kind    EventKind       `json:"kind"`
	Actor   Actor           `json:"actor"`
	At      string          `json:"at"`
	Payload json.RawMessage `json:"payload,omitempty"`
	// Seq is the per-project total order assigned by the project Durable
	// Object. It is the client sync cursor (design §7); zero until committed.
	Seq int64 `json:"seq"`
}

// Validate checks the structural invariants every event must satisfy regardless
// of kind: a known kind, a present actor, and a subject.
func (e WorkEvent) Validate() error {
	if !EventKinds[e.Kind] {
		return fmt.Errorf("%w: %q", ErrUnknownEventKind, e.Kind)
	}
	if err := e.Actor.Validate(); err != nil {
		return err
	}
	if e.Subject == "" {
		return fmt.Errorf("%w: subject is empty", ErrInvalidEvent)
	}
	return nil
}

// Status is a task/epic runtime status in the projection (data-model.md §5).
type Status string

// The closed set of statuses. Released is the status no external tracker can
// have — it derives only from the Deployment overlay (W3, invariant 5).
const (
	StatusBacklog    Status = "backlog"
	StatusTodo       Status = "todo"
	StatusInProgress Status = "in_progress"
	StatusInReview   Status = "in_review"
	StatusDone       Status = "done"
	StatusReleased   Status = "released"
	StatusCanceled   Status = "canceled"
)

// Statuses is the closed set of valid statuses.
var Statuses = map[Status]bool{
	StatusBacklog:    true,
	StatusTodo:       true,
	StatusInProgress: true,
	StatusInReview:   true,
	StatusDone:       true,
	StatusReleased:   true,
	StatusCanceled:   true,
}

// Cause records the delivery fact behind an automated transition (data-model.md
// §4): the PR, run, or deployment that moved the task.
type Cause struct {
	PR         string `json:"pr,omitempty"`
	Run        string `json:"run,omitempty"`
	Deployment string `json:"deployment,omitempty"`
}

// --- Typed event payloads (data-model.md §4). The WorkEvent carries them as raw
// JSON so the log stays one shape; the reducer decodes per kind. ---

type itemCreatedPayload struct {
	Item Item `json:"item"`
}

type itemEditedPayload struct {
	Title *string `json:"title,omitempty"`
	Doc   *string `json:"doc,omitempty"`
}

type statusChangedPayload struct {
	From  Status `json:"from"`
	To    Status `json:"to"`
	Cause *Cause `json:"cause,omitempty"`
}

type assignPayload struct {
	Principal string `json:"principal"`
}

type commentAddedPayload struct {
	CommentID string `json:"commentId"`
	Body      string `json:"body"`
}

type linkPayload struct {
	Link Link `json:"link"`
}

type contractEditedPayload struct {
	Contract *Contract `json:"contract"`
}

type movedPayload struct {
	Parent     *string  `json:"parent,omitempty"`
	BoardOrder *float64 `json:"boardOrder,omitempty"`
}

type cycleChangedPayload struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type labeledPayload struct {
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}

type sealedPayload struct {
	Object    string `json:"object"`
	Ref       string `json:"ref"`
	LedgerSeq int64  `json:"ledgerSeq"`
}

type importedPayload struct {
	Item   Item   `json:"item"`
	Source string `json:"source,omitempty"`
}

type canceledPayload struct {
	From   Status `json:"from"`
	Reason string `json:"reason,omitempty"`
}

func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		// The payload structs are all JSON-safe; a failure here is a
		// programming error, not a runtime condition.
		panic(fmt.Sprintf("work: marshal payload: %v", err))
	}
	return b
}

func decodePayload(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		return fmt.Errorf("%w: missing payload", ErrInvalidEvent)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("%w: decode payload: %v", ErrInvalidEvent, err)
	}
	return nil
}
