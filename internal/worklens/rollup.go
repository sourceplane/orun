package worklens

import (
	"fmt"
	"sort"
)

// Rollup folds (orun-work-v4 design §1.5): milestone progress, epic
// execution, and initiative health — derived, never stored, and never
// editable (V4-4). Every rollup is a fold OVER the delivery fold's output;
// the delivery fold itself is untouched (V4-1). Pin-beside-truth generalizes
// upward: a human may pin initiative health with the ordinary pinned event;
// the pin renders beside the derived value and auto-expires when the derived
// value reaches the pinned one (v2 invariant 6, verbatim).

// MilestoneProgress is the per-milestone delivery rollup. Key "" is the
// epic's unscheduled bucket (tasks with a spec but no milestone).
type MilestoneProgress struct {
	Key      string       `json:"key,omitempty"`
	Counts   map[Rung]int `json:"counts"`
	Total    int          `json:"total"`
	Complete int          `json:"complete"` // done + released
	Blocked  int          `json:"blocked"`
}

// EpicExecution is the per-epic delivery rollup: the milestone ladder with
// per-milestone progress, plus totals.
type EpicExecution struct {
	Key         string              `json:"key"`
	Milestones  []MilestoneProgress `json:"milestones,omitempty"` // ladder order
	Unscheduled *MilestoneProgress  `json:"unscheduled,omitempty"`
	Totals      map[Rung]int        `json:"totals"`
	Total       int                 `json:"total"`
	Complete    int                 `json:"complete"`
	Blocked     int                 `json:"blocked"`
}

// HealthPin is an active, attributed health override on an initiative —
// rendered beside the derived health, never instead of it.
type HealthPin struct {
	Health Health `json:"health"`
	By     Actor  `json:"by"`
	Note   string `json:"note,omitempty"`
	At     string `json:"at,omitempty"`
}

// InitiativeStatus is the top-of-pyramid rollup: derived health with named
// evidence, plus progress totals over member epics.
type InitiativeStatus struct {
	Key      string       `json:"key"`
	Health   Health       `json:"health"`
	Evidence []string     `json:"evidence,omitempty"`
	Pinned   *HealthPin   `json:"pinned,omitempty"`
	Progress map[Rung]int `json:"progress"`
	Total    int          `json:"total"`
	Complete int          `json:"complete"`
	Epics    int          `json:"epics"`
}

// EpicRollup pairs an epic's envelope with its two folds — the input one
// level of the pyramid hands the next.
type EpicRollup struct {
	Epic      Spec          `json:"epic"`
	Intent    EpicIntent    `json:"intent"`
	Execution EpicExecution `json:"execution"`
}

func newProgress(key string) *MilestoneProgress {
	return &MilestoneProgress{Key: key, Counts: map[Rung]int{}}
}

func (p *MilestoneProgress) add(lc Lifecycle) {
	p.Counts[lc.Rung]++
	p.Total++
	if lc.Rung == RungDone || lc.Rung == RungReleased {
		p.Complete++
	}
	if lc.Blocked {
		p.Blocked++
	}
}

// FoldEpicExecution rolls an epic's tasks up its milestone ladder. Tasks
// naming a milestone absent from the ladder count into the unscheduled
// bucket rather than vanishing (unresolved references degrade visibly —
// v2 invariant 8).
func FoldEpicExecution(ws WorkSet, epicKey string, ladder []Milestone, fr FoldResult) EpicExecution {
	out := EpicExecution{Key: epicKey, Totals: map[Rung]int{}}
	byKey := map[string]*MilestoneProgress{}
	for _, m := range ladder {
		byKey[m.Key] = newProgress(m.Key)
	}
	unscheduled := newProgress("")

	for _, t := range sortedTasks(ws.Tasks) {
		if t.Spec != epicKey {
			continue
		}
		lc := fr.Lifecycles[t.Key]
		bucket := unscheduled
		if t.Milestone != "" {
			if mp, ok := byKey[t.Milestone]; ok {
				bucket = mp
			}
		}
		bucket.add(lc)
		out.Totals[lc.Rung]++
		out.Total++
		if lc.Rung == RungDone || lc.Rung == RungReleased {
			out.Complete++
		}
		if lc.Blocked {
			out.Blocked++
		}
	}

	for _, m := range ladder {
		out.Milestones = append(out.Milestones, *byKey[m.Key])
	}
	if unscheduled.Total > 0 {
		out.Unscheduled = unscheduled
	}
	return out
}

// FoldInitiativeStatus derives an initiative's health from its member
// epics' folds. Deterministic by construction: time enters only through
// asOf (RFC 3339 date or timestamp), never through a clock — drop the
// cache, replay the logs at the same asOf, get the same health.
//
// The v1 formula (locked against the dogfood corpus in WH6, Q-7):
//   off_track — a member epic is past its target date and incomplete
//   at_risk   — blocked tasks, a drifted approval, or an epic past target
//               with nothing started yet count against it
//   on_track  — otherwise
// Every contributing fact is named in Evidence.
func FoldInitiativeStatus(key string, epics []EpicRollup, events []CoordinationEvent, asOf string) InitiativeStatus {
	out := InitiativeStatus{Key: key, Health: HealthOnTrack, Progress: map[Rung]int{}}

	ordered := append([]EpicRollup(nil), epics...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Epic.Key < ordered[j].Epic.Key })

	var atRisk, offTrack []string
	for _, er := range ordered {
		out.Epics++
		for r, n := range er.Execution.Totals {
			out.Progress[r] += n
		}
		out.Total += er.Execution.Total
		out.Complete += er.Execution.Complete

		if er.Intent.State == IntentApprovedDrifted {
			atRisk = append(atRisk, fmt.Sprintf("approval drifted on %s", er.Epic.Key))
		}
		if er.Execution.Blocked > 0 {
			atRisk = append(atRisk, fmt.Sprintf("%d blocked task(s) in %s", er.Execution.Blocked, er.Epic.Key))
		}
		if er.Epic.TargetDate != "" && asOf != "" && er.Epic.TargetDate < asOf[:min(len(asOf), 10)] {
			if er.Execution.Total > 0 && er.Execution.Complete < er.Execution.Total {
				offTrack = append(offTrack, fmt.Sprintf("%s past target %s (%d/%d complete)", er.Epic.Key, er.Epic.TargetDate, er.Execution.Complete, er.Execution.Total))
			} else if er.Execution.Total == 0 {
				atRisk = append(atRisk, fmt.Sprintf("%s past target %s with no tasks", er.Epic.Key, er.Epic.TargetDate))
			}
		}
	}

	switch {
	case len(offTrack) > 0:
		out.Health = HealthOffTrack
		out.Evidence = append(offTrack, atRisk...)
	case len(atRisk) > 0:
		out.Health = HealthAtRisk
		out.Evidence = atRisk
	default:
		if out.Total > 0 {
			out.Evidence = []string{fmt.Sprintf("%d/%d tasks complete across %d epic(s)", out.Complete, out.Total, out.Epics)}
		}
	}

	// Pin-beside-health: the last pinned event on the initiative subject
	// whose rung parses as a health value; expires when derived health
	// reaches the pinned level.
	var pin *HealthPin
	for _, e := range events {
		if e.Subject != key || e.Kind != EventPinned {
			continue
		}
		p, ok := e.PinOf()
		if !ok {
			continue
		}
		if p.Rung == "" {
			pin = nil
			continue
		}
		h := Health(p.Rung)
		if _, isHealth := HealthIndex(h); !isHealth {
			continue
		}
		pin = &HealthPin{Health: h, By: e.Actor, Note: p.Note, At: e.At}
	}
	if pin != nil {
		di, _ := HealthIndex(out.Health)
		pi, _ := HealthIndex(pin.Health)
		if di < pi { // derived is worse than pinned — pin still says something
			out.Pinned = pin
		}
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
