package worklens

// APIVersion stamps every envelope and sealed object.
const APIVersion = "orun.io/v1"

// Kind is the closed set of work entity kinds (WP-4: two nouns).
type Kind string

const (
	KindSpec Kind = "Spec"
	KindTask Kind = "Task"
)

// Rung is a position on the derived lifecycle ladder (design.md §5).
// Rungs are never stored; they are the fold's output.
type Rung string

const (
	RungDraft      Rung = "draft"
	RungReady      Rung = "ready"
	RungInProgress Rung = "in_progress"
	RungInReview   Rung = "in_review"
	RungDone       Rung = "done"
	RungReleased   Rung = "released"
	RungCanceled   Rung = "canceled"
)

// rungOrder positions rungs on the ladder for pin-expiry comparison
// (invariant 6: a pin auto-expires the moment observed truth reaches the
// pinned rung). Canceled is terminal and outside the ladder.
var rungOrder = map[Rung]int{
	RungDraft:      0,
	RungReady:      1,
	RungInProgress: 2,
	RungInReview:   3,
	RungDone:       4,
	RungReleased:   5,
}

// RungIndex returns the ladder position of r and whether r is on the ladder
// (Canceled and unknown rungs are not).
func RungIndex(r Rung) (int, bool) {
	i, ok := rungOrder[r]
	return i, ok
}

// EventKind is the closed coordination-event vocabulary (data-model.md §4.1).
// An unknown kind is a write-time error; extending the set is a schema rev.
type EventKind string

const (
	EventItemCreated    EventKind = "item_created"
	EventItemEdited     EventKind = "item_edited"
	EventContractEdited EventKind = "contract_edited"
	EventAssigned       EventKind = "assigned"
	EventUnassigned     EventKind = "unassigned"
	EventCommentAdded   EventKind = "comment_added"
	EventOrdered        EventKind = "ordered"
	EventPinned         EventKind = "pinned"
	EventCanceled       EventKind = "canceled"
)

// EventKinds enumerates the closed set in a stable order.
var EventKinds = []EventKind{
	EventItemCreated, EventItemEdited, EventContractEdited,
	EventAssigned, EventUnassigned, EventCommentAdded,
	EventOrdered, EventPinned, EventCanceled,
}

// IsEventKind reports whether k is in the closed coordination vocabulary.
func IsEventKind(k EventKind) bool {
	for _, v := range EventKinds {
		if v == k {
			return true
		}
	}
	return false
}

// ObservationKind is the closed world-authored fact vocabulary
// (data-model.md §4.2). Observations are never authored by mutators (WP-6).
type ObservationKind string

const (
	ObsBranchSeen   ObservationKind = "branch_seen"
	ObsPROpened     ObservationKind = "pr_opened"
	ObsPRMerged     ObservationKind = "pr_merged"
	ObsPRClosed     ObservationKind = "pr_closed"
	ObsGateResult   ObservationKind = "gate_result"
	ObsRevisionLive ObservationKind = "revision_live"
)

// ObservationKinds enumerates the closed set in a stable order.
var ObservationKinds = []ObservationKind{
	ObsBranchSeen, ObsPROpened, ObsPRMerged, ObsPRClosed,
	ObsGateResult, ObsRevisionLive,
}

// IsObservationKind reports whether k is in the closed observation vocabulary.
func IsObservationKind(k ObservationKind) bool {
	for _, v := range ObservationKinds {
		if v == k {
			return true
		}
	}
	return false
}

// ActorType is the provenance vocabulary. Automation never wears a user's
// or agent's identity (invariant 3).
type ActorType string

const (
	ActorUser       ActorType = "user"
	ActorAgent      ActorType = "agent"
	ActorAutomation ActorType = "automation"
)

// IsActorType reports whether t is a known actor type.
func IsActorType(t ActorType) bool {
	return t == ActorUser || t == ActorAgent || t == ActorAutomation
}

// GateStatus is the verdict vocabulary carried by gate_result observations.
type GateStatus string

const (
	GateGreen   GateStatus = "green"
	GateRed     GateStatus = "red"
	GatePending GateStatus = "pending"
)
