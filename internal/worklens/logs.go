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
	return nil
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
