package worklens

import (
	"encoding/json"
	"sort"
)

// The intent-ladder fold (orun-work-v4 design §1.4). Intent state is a fold
// over COORDINATION EVENTS ONLY — authored inputs, derived rendering, nothing
// stored. It is entirely separate from the delivery fold (fold.go), which
// reads none of the v4 kinds (V4-1: the v2/v3 conformance fixtures stay
// byte-identical).
//
// Drift is derived from the log, never trusted from a payload: the ladder
// and doc revision at approval time are re-folded from events at or before
// the approval's position, and compared to their current values. Task churn
// is invisible to this fold by construction (task events carry task
// subjects), which is exactly V4-5: tasks are regenerable implementation
// detail; the doc + milestone ladder are the approved scope.

// Approval is the record of who approved what, exactly.
type Approval struct {
	Revision   string `json:"revision,omitempty"` // doc revision named by the approved event
	Snapshot   string `json:"snapshot,omitempty"` // sealed EpicSnapshot content id
	By         Actor  `json:"by"`
	At         string `json:"at,omitempty"`
	LadderHash string `json:"ladderHash,omitempty"` // milestone-ladder digest at approval
}

// EpicIntent is the intent fold's per-epic output.
type EpicIntent struct {
	Key               string      `json:"key"`
	State             IntentState `json:"state"`
	Approval          *Approval   `json:"approval,omitempty"`          // last active approval (approved / approved_drifted)
	CurrentRevision   string      `json:"currentRevision,omitempty"`   // latest doc_edited revision
	CurrentLadderHash string      `json:"currentLadderHash,omitempty"` // digest of the current ladder
	DocDrifted        bool        `json:"docDrifted,omitempty"`
	LadderDrifted     bool        `json:"ladderDrifted,omitempty"`
	Milestones        []Milestone `json:"milestones,omitempty"` // current ladder, ordered
}

// DesignIntent is the intent fold's per-design output.
type DesignIntent struct {
	Key             string      `json:"key"`
	State           IntentState `json:"state"`
	AdoptedRevision string      `json:"adoptedRevision,omitempty"`
	Minted          []string    `json:"minted,omitempty"`
	AdoptedBy       *Actor      `json:"adoptedBy,omitempty"`
	SupersededBy    string      `json:"supersededBy,omitempty"`
}

// ladderState folds milestone_edited ops in log order.
type ladderState struct {
	byKey map[string]*Milestone
	dead  map[string]bool
	seq   []string // creation order, for stable tie-breaks
}

func newLadderState() *ladderState {
	return &ladderState{byKey: map[string]*Milestone{}, dead: map[string]bool{}}
}

func (l *ladderState) apply(p MilestonePayload) {
	switch p.Op {
	case "create":
		if l.byKey[p.Key] != nil && !l.dead[p.Key] {
			return // keys are immutable once created; duplicate create is a no-op
		}
		m := &Milestone{Key: p.Key, Ordinal: len(l.seq)}
		applyMilestoneFields(m, p)
		l.byKey[p.Key] = m
		delete(l.dead, p.Key)
		l.seq = append(l.seq, p.Key)
	case "edit", "reorder":
		m := l.byKey[p.Key]
		if m == nil || l.dead[p.Key] {
			return
		}
		applyMilestoneFields(m, p)
	case "remove":
		if l.byKey[p.Key] != nil {
			l.dead[p.Key] = true
		}
	}
}

func applyMilestoneFields(m *Milestone, p MilestonePayload) {
	if p.Title != nil {
		m.Title = *p.Title
	}
	if p.Goal != nil {
		m.Goal = *p.Goal
	}
	if p.DoneWhen != nil {
		m.DoneWhen = append([]string(nil), p.DoneWhen...)
	}
	if p.TargetDate != nil {
		m.TargetDate = *p.TargetDate
	}
	if p.Ordinal != nil {
		m.Ordinal = *p.Ordinal
	}
}

// milestones returns the live ladder ordered by (ordinal, key).
func (l *ladderState) milestones() []Milestone {
	out := make([]Milestone, 0, len(l.byKey))
	for k, m := range l.byKey {
		if l.dead[k] {
			continue
		}
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Ordinal != out[j].Ordinal {
			return out[i].Ordinal < out[j].Ordinal
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// LadderHash digests a milestone ladder canonically (keys, titles, goals,
// doneWhen, target dates, order). Identical ladders hash identically on
// every machine — the approval-coverage seam (V4-2, V4-6).
func LadderHash(ms []Milestone) (string, error) {
	if ms == nil {
		ms = []Milestone{}
	}
	id, _, err := ContentID(ms)
	return id, err
}

// FoldMilestones derives an epic's current milestone ladder from its
// milestone_edited events. Events MUST be in log order; non-epic subjects
// are ignored.
func FoldMilestones(epicKey string, events []CoordinationEvent) []Milestone {
	l := newLadderState()
	for _, e := range events {
		if e.Subject != epicKey {
			continue
		}
		if p, ok := e.MilestoneOf(); ok {
			l.apply(p)
		}
	}
	return l.milestones()
}

// FoldEpicIntent derives an epic's intent state from its coordination
// events (log order). The delivery fold is not consulted: intent is
// authored, delivery is observed, and neither reads the other's inputs.
func FoldEpicIntent(epicKey string, events []CoordinationEvent) EpicIntent {
	ladder := newLadderState()
	var (
		docRev       string
		approval     *Approval
		lastDecision int64 // seq of the last approved/approval_revoked
		lastReview   int64 // seq of the last review_requested
		canceled     bool
	)
	for _, e := range events {
		if e.Subject != epicKey {
			continue
		}
		switch e.Kind {
		case EventMilestoneEdited:
			if p, ok := e.MilestoneOf(); ok {
				ladder.apply(p)
			}
		case EventDocEdited:
			if rev, ok := e.DocRevisionOf(); ok {
				docRev = rev
			}
		case EventReviewRequested:
			lastReview = e.Seq
		case EventApproved:
			p, _ := e.ApprovalOf()
			hash, err := LadderHash(ladder.milestones())
			if err != nil {
				hash = ""
			}
			rev := p.Revision
			if rev == "" {
				rev = docRev // an approval without a named revision covers the current doc
			}
			approval = &Approval{Revision: rev, Snapshot: p.Snapshot, By: e.Actor, At: e.At, LadderHash: hash}
			lastDecision = e.Seq
		case EventApprovalRevoked:
			approval = nil
			lastDecision = e.Seq
		case EventCanceled:
			canceled = true
		}
	}

	out := EpicIntent{Key: epicKey, CurrentRevision: docRev, Milestones: ladder.milestones()}
	if hash, err := LadderHash(out.Milestones); err == nil {
		out.CurrentLadderHash = hash
	}

	switch {
	case canceled:
		out.State = IntentCanceled
	case approval != nil:
		out.Approval = approval
		out.DocDrifted = approval.Revision != docRev
		out.LadderDrifted = approval.LadderHash != "" && approval.LadderHash != out.CurrentLadderHash
		if out.DocDrifted || out.LadderDrifted {
			out.State = IntentApprovedDrifted
		} else {
			out.State = IntentApproved
		}
	case lastReview > lastDecision:
		out.State = IntentInReview
	default:
		out.State = IntentDraft
	}
	return out
}

// FoldDesignIntent derives a design's intent state from its coordination
// events (log order). Adoption freezes its record (V4-4): a later supersede
// changes the state but never erases what was adopted.
func FoldDesignIntent(designKey string, events []CoordinationEvent) DesignIntent {
	out := DesignIntent{Key: designKey, State: IntentDraft}
	var inReview, adopted, superseded, canceled bool
	for _, e := range events {
		if e.Subject != designKey {
			continue
		}
		switch e.Kind {
		case EventReviewRequested:
			inReview = true
		case EventDesignAdopted:
			p, _ := e.AdoptionOf()
			adopted = true
			out.AdoptedRevision = p.Revision
			out.Minted = p.Minted
			by := e.Actor
			out.AdoptedBy = &by
		case EventSuperseded:
			superseded = true
			var p SupersededPayload
			if sp, ok := supersededOf(e); ok {
				p = sp
			}
			out.SupersededBy = p.By
		case EventCanceled:
			canceled = true
		}
	}
	switch {
	case canceled:
		out.State = IntentCanceled
	case superseded:
		out.State = IntentSuperseded
	case adopted:
		out.State = IntentAdopted
	case inReview:
		out.State = IntentInReview
	}
	return out
}

func supersededOf(e CoordinationEvent) (SupersededPayload, bool) {
	if e.Kind != EventSuperseded || len(e.Payload) == 0 {
		return SupersededPayload{}, false
	}
	var p SupersededPayload
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return SupersededPayload{}, false
	}
	return p, true
}
