package worklens

import (
	"encoding/json"
	"fmt"
)

// CoordinationEvent is one entry in the authored log (data-model.md §4.1).
// Append-only; every entry carries an actor; seq is the per-workspace total
// order and the client sync cursor.
type CoordinationEvent struct {
	EventID   string          `json:"eventId,omitempty"`
	Workspace string          `json:"workspace"`
	Subject   string          `json:"subject"` // task or spec key
	Kind      EventKind       `json:"kind"`
	Actor     Actor           `json:"actor"`
	At        string          `json:"at"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Seq       int64           `json:"seq"`
}

// PinPayload is the payload of a `pinned` event. Rung "" (null) unpins.
type PinPayload struct {
	Rung Rung   `json:"rung,omitempty"`
	Note string `json:"note,omitempty"`
}

// Validate enforces the write-time rules for coordination events:
// closed kind, mandatory actor, non-empty subject, and the agent guardrail
// (agents never author pins — WP-10, invariant 3).
func (e CoordinationEvent) Validate() error {
	if !IsEventKind(e.Kind) {
		return fmt.Errorf("worklens: unknown event kind %q", e.Kind)
	}
	if e.Subject == "" {
		return fmt.Errorf("worklens: event %s has no subject", e.Kind)
	}
	if err := e.Actor.Validate(); err != nil {
		return fmt.Errorf("worklens: event %s on %s: %w", e.Kind, e.Subject, err)
	}
	if e.Kind == EventPinned && e.Actor.Type == ActorAgent {
		return fmt.Errorf("worklens: agents may not pin (%s on %s)", e.Kind, e.Subject)
	}
	if IsHumanOnlyEventKind(e.Kind) && e.Actor.Type != ActorUser {
		return fmt.Errorf("worklens: %s is a human-only decision — actor type %q may not author it (V4-2)", e.Kind, e.Actor.Type)
	}
	return nil
}

// RelationPayload is the payload of related/unrelated events (v3 PM2).
// rel is one of blocks|parent|relates; `blocks` is the only kind the fold
// reads — the target derives Blocked from it exactly as from contract Deps.
type RelationPayload struct {
	Rel    string `json:"rel"`
	Target string `json:"target"`
}

// RelationOf decodes a related/unrelated event's payload; ok is false for
// other kinds or malformed payloads.
func (e CoordinationEvent) RelationOf() (RelationPayload, bool) {
	if e.Kind != EventRelated && e.Kind != EventUnrelated {
		return RelationPayload{}, false
	}
	if len(e.Payload) == 0 {
		return RelationPayload{}, false
	}
	var p RelationPayload
	if err := json.Unmarshal(e.Payload, &p); err != nil || p.Rel == "" || p.Target == "" {
		return RelationPayload{}, false
	}
	return p, true
}

// PinOf decodes a pinned event's payload; ok is false for non-pin events.
func (e CoordinationEvent) PinOf() (PinPayload, bool) {
	if e.Kind != EventPinned {
		return PinPayload{}, false
	}
	var p PinPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return PinPayload{}, false
		}
	}
	return p, true
}

// --- v4 intent-ladder payloads (orun-work-v4 design §1.3) ---

// MilestonePayload is the payload of a milestone_edited event on an epic
// subject — the only way milestones change. Pointer fields distinguish
// "not touched" from "set to zero" on edit.
type MilestonePayload struct {
	Op         string   `json:"op"` // create | edit | reorder | remove
	Key        string   `json:"key"`
	Title      *string  `json:"title,omitempty"`
	Goal       *string  `json:"goal,omitempty"`
	DoneWhen   []string `json:"doneWhen,omitempty"`
	TargetDate *string  `json:"targetDate,omitempty"`
	Ordinal    *int     `json:"ordinal,omitempty"`
}

// MilestoneSetPayload is the payload of a milestone_set event on a task
// subject. Milestone "" clears (back to the epic's unscheduled bucket).
type MilestoneSetPayload struct {
	Milestone string `json:"milestone,omitempty"`
}

// ReviewRequestedPayload enters an epic or design into In Review at a named
// doc revision. Reviewers are suggestions, never gates.
type ReviewRequestedPayload struct {
	Revision  string   `json:"revision,omitempty"`
	Reviewers []string `json:"reviewers,omitempty"`
	Note      string   `json:"note,omitempty"`
}

// ReviewSubmittedPayload carries a reviewer's verdict at a revision. Agent
// verdicts are advice (V4-2) — they count toward nothing by themselves.
type ReviewSubmittedPayload struct {
	Revision string        `json:"revision,omitempty"`
	Verdict  ReviewVerdict `json:"verdict"`
	Note     string        `json:"note,omitempty"`
}

// ApprovedPayload names exactly what was approved: the doc revision and the
// sealed EpicSnapshot minted in the same transaction. "Approved" never
// renders without "of what" (V4-2).
type ApprovedPayload struct {
	Revision string `json:"revision,omitempty"`
	Snapshot string `json:"snapshot,omitempty"`
}

// DesignAdoptedPayload freezes the adoption record: the design revision the
// mint ran from and the epic keys it created (V4-4).
type DesignAdoptedPayload struct {
	Revision string   `json:"revision,omitempty"`
	Minted   []string `json:"minted,omitempty"`
}

// SupersededPayload marks a design terminal, optionally naming its rival.
type SupersededPayload struct {
	By   string `json:"by,omitempty"`
	Note string `json:"note,omitempty"`
}

// MilestoneOf decodes a milestone_edited payload; ok is false for other
// kinds or payloads without op+key.
func (e CoordinationEvent) MilestoneOf() (MilestonePayload, bool) {
	if e.Kind != EventMilestoneEdited || len(e.Payload) == 0 {
		return MilestonePayload{}, false
	}
	var p MilestonePayload
	if err := json.Unmarshal(e.Payload, &p); err != nil || p.Op == "" || p.Key == "" {
		return MilestonePayload{}, false
	}
	return p, true
}

// DocRevisionOf decodes a doc_edited payload's revision; ok is false for
// other kinds or revision-less payloads.
func (e CoordinationEvent) DocRevisionOf() (string, bool) {
	if e.Kind != EventDocEdited || len(e.Payload) == 0 {
		return "", false
	}
	var p struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(e.Payload, &p); err != nil || p.Revision == "" {
		return "", false
	}
	return p.Revision, true
}

// ApprovalOf decodes an approved payload; ok is false for other kinds.
func (e CoordinationEvent) ApprovalOf() (ApprovedPayload, bool) {
	if e.Kind != EventApproved {
		return ApprovedPayload{}, false
	}
	var p ApprovedPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return ApprovedPayload{}, false
		}
	}
	return p, true
}

// AdoptionOf decodes a design_adopted payload; ok is false for other kinds.
func (e CoordinationEvent) AdoptionOf() (DesignAdoptedPayload, bool) {
	if e.Kind != EventDesignAdopted {
		return DesignAdoptedPayload{}, false
	}
	var p DesignAdoptedPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return DesignAdoptedPayload{}, false
		}
	}
	return p, true
}

// Observation is one entry in the world-authored log (data-model.md §4.2).
// Idempotent by DedupeKey (invariant 4): the same world fact ingested twice
// folds identically.
type Observation struct {
	ObsID         string          `json:"obsId,omitempty"`
	Workspace     string          `json:"workspace"`
	Source        string          `json:"source"` // github-webhook | run-stream | deploy-overlay | ci | import-backfill
	SourceVersion int             `json:"sourceVersion"`
	Kind          ObservationKind `json:"kind"`
	At            string          `json:"at"`
	DedupeKey     string          `json:"dedupeKey"`
	Payload       json.RawMessage `json:"payload,omitempty"`
	Seq           int64           `json:"seq"`
}

// PRPayload is the payload shape of branch_seen / pr_opened / pr_merged /
// pr_closed observations.
type PRPayload struct {
	PR       string   `json:"pr,omitempty"` // "owner/repo#412"
	Branch   string   `json:"branch,omitempty"`
	Title    string   `json:"title,omitempty"`
	Draft    bool     `json:"draft,omitempty"`
	Revision string   `json:"revision,omitempty"`
	TaskKeys []string `json:"taskKeys,omitempty"` // parsed from branch/title by the ingester
	Affected []string `json:"affected,omitempty"` // Result.Affected, produced by orun/CI
}

// GatePayload is the payload shape of gate_result observations.
type GatePayload struct {
	Gate     string     `json:"gate"`
	Revision string     `json:"revision"`
	Status   GateStatus `json:"status"`
	RunRef   string     `json:"runRef,omitempty"`
}

// LivePayload is the payload shape of revision_live observations
// (the resources-runtime liveObservation).
type LivePayload struct {
	Revision      string `json:"revision"`
	Environment   string `json:"environment"`
	DeploymentRef string `json:"deploymentRef,omitempty"`
}

// Validate enforces the write-time rules for observations: closed kind,
// named versioned source, and a dedupe key.
func (o Observation) Validate() error {
	if !IsObservationKind(o.Kind) {
		return fmt.Errorf("worklens: unknown observation kind %q", o.Kind)
	}
	if o.Source == "" {
		return fmt.Errorf("worklens: observation %s has no source", o.Kind)
	}
	if o.SourceVersion < 1 {
		return fmt.Errorf("worklens: observation %s from %s has no sourceVersion", o.Kind, o.Source)
	}
	if o.DedupeKey == "" {
		return fmt.Errorf("worklens: observation %s from %s has no dedupeKey", o.Kind, o.Source)
	}
	return nil
}

func (o Observation) prPayload() (PRPayload, bool) {
	switch o.Kind {
	case ObsBranchSeen, ObsPROpened, ObsPRMerged, ObsPRClosed:
	default:
		return PRPayload{}, false
	}
	var p PRPayload
	if len(o.Payload) > 0 {
		if err := json.Unmarshal(o.Payload, &p); err != nil {
			return PRPayload{}, false
		}
	}
	return p, true
}

func (o Observation) gatePayload() (GatePayload, bool) {
	if o.Kind != ObsGateResult {
		return GatePayload{}, false
	}
	var p GatePayload
	if len(o.Payload) > 0 {
		if err := json.Unmarshal(o.Payload, &p); err != nil {
			return GatePayload{}, false
		}
	}
	return p, true
}

func (o Observation) livePayload() (LivePayload, bool) {
	if o.Kind != ObsRevisionLive {
		return LivePayload{}, false
	}
	var p LivePayload
	if len(o.Payload) > 0 {
		if err := json.Unmarshal(o.Payload, &p); err != nil {
			return LivePayload{}, false
		}
	}
	return p, true
}
