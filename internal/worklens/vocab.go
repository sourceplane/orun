package worklens

// APIVersion stamps every envelope and sealed object.
const APIVersion = "orun.io/v1"

// Kind is the closed set of work entity kinds (WP-4: two nouns).
type Kind string

const (
	KindSpec Kind = "Spec"
	KindTask Kind = "Task"

	// v4 (orun-work-v4 WH0). The Spec kind gains the surface name "Epic"
	// (V4-C: alias, not migration — the wire kind stays Spec). Initiative and
	// Design are envelope-level nouns; milestones are epic-scoped sub-items
	// addressed as "<epic-key>#<milestone-key>", not a kind.
	KindInitiative Kind = "Initiative"
	KindDesign     Kind = "Design"
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

	// v3 (orun-work-v3 PM0; orun-cloud specs/epics/orun-work-v3 design §1.2).
	// Write-time acceptance only: every addition is intent or conversation
	// (V3-1) and THE FOLD READS NONE OF THEM when deriving a rung — the
	// shared conformance fixtures pin that byte-identically. There is still
	// no lifecycle-write kind.
	EventDocEdited       EventKind = "doc_edited"
	EventReactionAdded   EventKind = "reaction_added"
	EventReactionRemoved EventKind = "reaction_removed"
	EventLabeled         EventKind = "labeled"
	EventUnlabeled       EventKind = "unlabeled"
	EventPrioritized     EventKind = "prioritized"
	EventEstimated       EventKind = "estimated"
	EventCycleSet        EventKind = "cycle_set"
	EventRelated         EventKind = "related"
	EventUnrelated       EventKind = "unrelated"

	// v4 (orun-work-v4 WH0; orun-cloud specs/epics/orun-work-v4 design §1.3).
	// Every addition is a decision or intent — the INTENT ladder is authored
	// (review/approve/adopt are things the world cannot observe, exactly like
	// Canceled), while the DELIVERY fold reads none of these (V4-1: the v2/v3
	// conformance fixtures stay byte-identical). There is still no
	// delivery-lifecycle-write kind. approved / approval_revoked /
	// design_adopted / superseded are HUMAN-ONLY (V4-2: the agent-pin guard,
	// extended — enforced in CoordinationEvent.Validate and again server-side).
	EventMilestoneEdited EventKind = "milestone_edited"
	EventMilestoneSet    EventKind = "milestone_set"
	EventReviewRequested EventKind = "review_requested"
	EventReviewSubmitted EventKind = "review_submitted"
	EventApproved        EventKind = "approved"
	EventApprovalRevoked EventKind = "approval_revoked"
	EventDesignAdopted   EventKind = "design_adopted"
	EventSuperseded      EventKind = "superseded"
)

// EventKinds enumerates the closed set in a stable order.
var EventKinds = []EventKind{
	EventItemCreated, EventItemEdited, EventContractEdited,
	EventAssigned, EventUnassigned, EventCommentAdded,
	EventOrdered, EventPinned, EventCanceled,
	EventDocEdited, EventReactionAdded, EventReactionRemoved,
	EventLabeled, EventUnlabeled, EventPrioritized,
	EventEstimated, EventCycleSet, EventRelated, EventUnrelated,
	EventMilestoneEdited, EventMilestoneSet, EventReviewRequested,
	EventReviewSubmitted, EventApproved, EventApprovalRevoked,
	EventDesignAdopted, EventSuperseded,
}

// HumanOnlyEventKinds are the decisions only a person may author (V4-2):
// approval, its revocation, adopting a design, and superseding one. Neither
// agents nor automation may wear these — the world cannot know a human
// decided, so only a human actor may say so.
var HumanOnlyEventKinds = []EventKind{
	EventApproved, EventApprovalRevoked, EventDesignAdopted, EventSuperseded,
}

// IsHumanOnlyEventKind reports whether k requires an actor of type user.
func IsHumanOnlyEventKind(k EventKind) bool {
	for _, v := range HumanOnlyEventKinds {
		if v == k {
			return true
		}
	}
	return false
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

// IntentState is the AUTHORED intent ladder (v4, design §1.4) — a fold over
// coordination events only, entirely separate from the derived delivery Rung.
// Approved never renders without its revision (V4-2); post-approval edits to
// the doc or milestone ladder fold to ApprovedDrifted (V4-3), which only a
// fresh approval clears.
type IntentState string

const (
	IntentDraft           IntentState = "draft"
	IntentInReview        IntentState = "in_review"
	IntentApproved        IntentState = "approved"
	IntentApprovedDrifted IntentState = "approved_drifted"
	IntentAdopted         IntentState = "adopted"    // designs only
	IntentSuperseded      IntentState = "superseded" // designs only
	IntentCanceled        IntentState = "canceled"
)

// ReviewVerdict is the vocabulary of review_submitted payloads. Agents may
// submit verdicts (advice, rendered with an agent chip); only humans approve.
type ReviewVerdict string

const (
	VerdictApprove        ReviewVerdict = "approve"
	VerdictRequestChanges ReviewVerdict = "request_changes"
)

// Health is the derived initiative health vocabulary (v4, design §1.5) —
// computed with named evidence, never a dropdown. A human may pin health
// (the ordinary pinned event on the initiative subject); the pin renders
// beside the derived value and auto-expires when truth catches up.
type Health string

const (
	HealthOnTrack  Health = "on_track"
	HealthAtRisk   Health = "at_risk"
	HealthOffTrack Health = "off_track"
)

// healthOrder positions health values for pin-expiry comparison (worse → 0).
var healthOrder = map[Health]int{
	HealthOffTrack: 0,
	HealthAtRisk:   1,
	HealthOnTrack:  2,
}

// HealthIndex returns the order of h and whether h is a health value.
func HealthIndex(h Health) (int, bool) {
	i, ok := healthOrder[h]
	return i, ok
}
