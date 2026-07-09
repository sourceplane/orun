package worklens

import (
	"fmt"
	"sort"
)

// WorkSet is the fold's input: current intent envelopes plus both logs.
// Events and observations MUST be ordered by seq ascending (log order is
// fold order — P-5).
type WorkSet struct {
	Tasks        []Task
	Events       []CoordinationEvent
	Observations []Observation
}

// Pin is an active, attributed lifecycle override. It renders beside the
// observed rung, never instead of it (invariant 6), and expires the moment
// observed truth reaches the pinned rung.
type Pin struct {
	Rung Rung   `json:"rung"`
	By   Actor  `json:"by"`
	Note string `json:"note,omitempty"`
	At   string `json:"at,omitempty"`
}

// Lifecycle is the fold's per-task output: the observed rung with its
// evidence, the readiness/blocked flags, and any active pin. Nothing in it
// is ever stored (invariant 1) — caches of it must be droppable.
type Lifecycle struct {
	Key      string   `json:"key"`
	Rung     Rung     `json:"rung"`
	Pinned   *Pin     `json:"pinned,omitempty"`
	Ready    bool     `json:"ready"`   // contract complete (= agent-ready when not blocked)
	Blocked  bool     `json:"blocked"` // open blockedBy dep — a flag, never a rung
	Evidence []string `json:"evidence,omitempty"`
}

// DriftItem is a merged PR whose affected components no open task claims —
// the planning-integrity standing query (design.md §6).
type DriftItem struct {
	PR       string   `json:"pr"`
	Affected []string `json:"affected"`
}

// Suggestion is an ambiguous auto-claim: a PR whose affected set overlaps
// more than one open task. Ambiguity suggests, never links (P-6).
type Suggestion struct {
	PR       string   `json:"pr"`
	TaskKeys []string `json:"taskKeys"`
}

// FoldResult is everything the read surfaces derive from the two logs.
type FoldResult struct {
	Lifecycles  map[string]Lifecycle `json:"lifecycles"`
	Drift       []DriftItem          `json:"drift,omitempty"`
	Suggestions []Suggestion         `json:"suggestions,omitempty"`
}

// prState folds a PR's observations into its latest world state.
type prState struct {
	id        string
	firstSeq  int64
	branch    string
	draft     bool
	opened    bool
	merged    bool
	closed    bool
	revision  string
	taskKeys  []string
	affected  []string
	branchOnly bool
}

// Fold derives lifecycle, drift, and claim suggestions from the two logs.
// It is pure and deterministic: identical inputs produce identical outputs
// on every machine — the orun-cloud TypeScript fold replays this package's
// conformance fixtures byte-for-byte.
func Fold(ws WorkSet) FoldResult {
	tasks := map[string]Task{}
	for _, t := range ws.Tasks {
		tasks[t.Key] = t
	}

	// Pass 1 — coordination: cancellation, active pins, and blocks relations
	// (v3 PM2: `related {rel: blocks, target}` makes target blocked by the
	// subject exactly as a contract dep would), in log order.
	canceled := map[string]Actor{}
	pins := map[string]*Pin{}
	blockers := map[string]map[string]bool{} // target key -> open set of blocker keys
	for _, e := range ws.Events {
		switch e.Kind {
		case EventCanceled:
			canceled[e.Subject] = e.Actor
		case EventPinned:
			p, ok := e.PinOf()
			if !ok {
				continue
			}
			if p.Rung == "" {
				delete(pins, e.Subject) // explicit unpin
				continue
			}
			pins[e.Subject] = &Pin{Rung: p.Rung, By: e.Actor, Note: p.Note, At: e.At}
		case EventRelated, EventUnrelated:
			r, ok := e.RelationOf()
			if !ok || r.Rel != "blocks" {
				continue
			}
			if e.Kind == EventRelated {
				if blockers[r.Target] == nil {
					blockers[r.Target] = map[string]bool{}
				}
				blockers[r.Target][e.Subject] = true
			} else {
				delete(blockers[r.Target], e.Subject)
			}
		}
	}

	// Pass 2 — observation: fold PR trajectories and gate/live facts.
	prs := map[string]*prState{}
	prOf := func(id string, seq int64) *prState {
		if p, ok := prs[id]; ok {
			return p
		}
		p := &prState{id: id, firstSeq: seq}
		prs[id] = p
		return p
	}
	// gate results: latest verdict per (gate, revision), by log order
	gates := map[string]GateStatus{}
	gateKey := func(gate, rev string) string { return gate + "@" + rev }
	// live revisions: revision -> environment first observed live
	live := map[string]string{}

	for _, o := range ws.Observations {
		switch o.Kind {
		case ObsBranchSeen:
			p, ok := o.prPayload()
			if !ok {
				continue
			}
			id := p.PR
			if id == "" {
				id = "branch:" + p.Branch
			}
			st := prOf(id, o.Seq)
			st.branch = p.Branch
			if !st.opened {
				st.branchOnly = true
			}
			st.taskKeys = mergeKeys(st.taskKeys, p.TaskKeys)
		case ObsPROpened:
			st := prOf(o.mustPR(), o.Seq)
			p, _ := o.prPayload()
			st.opened, st.branchOnly, st.closed = true, false, false
			st.draft = p.Draft
			st.taskKeys = mergeKeys(st.taskKeys, p.TaskKeys)
			st.affected = mergeKeys(st.affected, p.Affected)
		case ObsPRMerged:
			st := prOf(o.mustPR(), o.Seq)
			p, _ := o.prPayload()
			st.merged, st.branchOnly = true, false
			if p.Revision != "" {
				st.revision = p.Revision
			}
			st.taskKeys = mergeKeys(st.taskKeys, p.TaskKeys)
			st.affected = mergeKeys(st.affected, p.Affected)
		case ObsPRClosed:
			st := prOf(o.mustPR(), o.Seq)
			if !st.merged {
				st.closed = true
			}
		case ObsGateResult:
			g, ok := o.gatePayload()
			if !ok {
				continue
			}
			gates[gateKey(g.Gate, g.Revision)] = g.Status
		case ObsRevisionLive:
			l, ok := o.livePayload()
			if !ok {
				continue
			}
			if _, seen := live[l.Revision]; !seen {
				live[l.Revision] = l.Environment
			}
		}
	}

	orderedPRs := make([]*prState, 0, len(prs))
	for _, p := range prs {
		orderedPRs = append(orderedPRs, p)
	}
	sort.Slice(orderedPRs, func(i, j int) bool { return orderedPRs[i].firstSeq < orderedPRs[j].firstSeq })

	// Pass 3 — the claim join: key parse wins and is always unambiguous;
	// component overlap claims only when exactly one open task matches.
	claims := map[string][]*prState{} // task key -> claiming PRs
	var suggestions []Suggestion
	openForClaim := func(key string) bool {
		_, isTask := tasks[key]
		_, isCanceled := canceled[key]
		return isTask && !isCanceled
	}
	for _, pr := range orderedPRs {
		claimed := false
		for _, k := range pr.taskKeys {
			if openForClaim(k) {
				claims[k] = append(claims[k], pr)
				claimed = true
			}
		}
		if claimed || len(pr.affected) == 0 {
			continue
		}
		var matches []string
		for _, t := range sortedTasks(ws.Tasks) {
			if !openForClaim(t.Key) || t.Contract == nil {
				continue
			}
			if overlaps(t.Contract.Affects, pr.affected) {
				matches = append(matches, t.Key)
			}
		}
		switch len(matches) {
		case 0:
		case 1:
			claims[matches[0]] = append(claims[matches[0]], pr)
		default:
			suggestions = append(suggestions, Suggestion{PR: pr.id, TaskKeys: matches})
		}
	}

	// Pass 4 — per-task observed rung + evidence.
	lifecycles := map[string]Lifecycle{}
	for _, t := range sortedTasks(ws.Tasks) {
		lc := Lifecycle{Key: t.Key, Ready: t.Contract.Complete()}
		if by, isCanceled := canceled[t.Key]; isCanceled {
			lc.Rung = RungCanceled
			lc.Evidence = []string{fmt.Sprintf("canceled by %s", by.ID)}
			lifecycles[t.Key] = lc
			continue
		}
		lc.Rung, lc.Evidence = observedRung(t, claims[t.Key], gates, gateKey, live)
		lc.Blocked = isBlocked(t, tasks, canceled, blockers, claims, gates, gateKey, live)
		if pin := pins[t.Key]; pin != nil {
			oi, okO := RungIndex(lc.Rung)
			pi, okP := RungIndex(pin.Rung)
			if okO && okP && oi < pi { // not yet expired (invariant 6)
				lc.Pinned = pin
			}
		}
		lifecycles[t.Key] = lc
	}

	// Pass 5 — drift: merged PRs with affected data, no claims, and no
	// open task claiming any of their components.
	var drift []DriftItem
	openAffects := map[string]bool{}
	for _, t := range ws.Tasks {
		lc := lifecycles[t.Key]
		if lc.Rung == RungCanceled || lc.Rung == RungDone || lc.Rung == RungReleased {
			continue
		}
		if t.Contract != nil {
			for _, a := range t.Contract.Affects {
				openAffects[a] = true
			}
		}
	}
	for _, pr := range orderedPRs {
		if !pr.merged || len(pr.affected) == 0 {
			continue
		}
		if prClaimsAnything(pr, claims) {
			continue
		}
		any := false
		for _, a := range pr.affected {
			if openAffects[a] {
				any = true
				break
			}
		}
		if !any {
			drift = append(drift, DriftItem{PR: pr.id, Affected: pr.affected})
		}
	}

	return FoldResult{Lifecycles: lifecycles, Drift: drift, Suggestions: suggestions}
}

// observedRung walks the ladder top-down, most-delivered rung first
// (data-model.md §6). Conservative by construction: unknown-to-orun renders
// unknown and parks In Review (invariant 5).
func observedRung(t Task, claiming []*prState, gates map[string]GateStatus, gateKey func(string, string) string, live map[string]string) (Rung, []string) {
	// Released: a merged claiming PR's revision observed live.
	for _, pr := range claiming {
		if pr.merged && pr.revision != "" {
			if env, ok := live[pr.revision]; ok {
				return RungReleased, []string{fmt.Sprintf("revision %s live in %s (PR %s)", pr.revision, env, pr.id)}
			}
		}
	}
	// Done / merged-but-parked.
	for _, pr := range claiming {
		if !pr.merged {
			continue
		}
		if t.Contract == nil || !(t.Contract.GatesDefined || len(t.Contract.Gates) > 0) {
			return RungInReview, []string{fmt.Sprintf("PR %s merged; gates unknown to orun", pr.id)}
		}
		for _, g := range t.Contract.Gates {
			status, ok := gates[gateKey(g, pr.revision)]
			if !ok {
				return RungInReview, []string{fmt.Sprintf("PR %s merged; gate %s unknown", pr.id, g)}
			}
			if status != GateGreen {
				return RungInReview, []string{fmt.Sprintf("PR %s merged; gate %s %s", pr.id, g, status)}
			}
		}
		if len(t.Contract.Gates) == 0 {
			return RungDone, []string{fmt.Sprintf("PR %s merged", pr.id)}
		}
		return RungDone, []string{fmt.Sprintf("PR %s merged; gates green", pr.id)}
	}
	// In Review: an open, non-draft claiming PR.
	for _, pr := range claiming {
		if pr.opened && !pr.merged && !pr.closed && !pr.draft {
			return RungInReview, []string{fmt.Sprintf("PR %s open", pr.id)}
		}
	}
	// In Progress: a draft PR or a seen branch.
	for _, pr := range claiming {
		if pr.opened && !pr.merged && !pr.closed && pr.draft {
			return RungInProgress, []string{fmt.Sprintf("PR %s draft", pr.id)}
		}
		if pr.branchOnly {
			return RungInProgress, []string{fmt.Sprintf("branch %s seen", pr.branch)}
		}
	}
	if t.Contract.Complete() {
		return RungReady, []string{"contract complete"}
	}
	return RungDraft, nil
}

// isBlocked derives the Blocked flag from open blockedBy deps and open
// `blocks` relations (v3 PM2) — a flag, not a rung, so it can never go
// stale by forgetting to un-set it.
func isBlocked(t Task, tasks map[string]Task, canceled map[string]Actor, blockers map[string]map[string]bool, claims map[string][]*prState, gates map[string]GateStatus, gateKey func(string, string) string, live map[string]string) bool {
	open := func(key string) bool {
		blocker, ok := tasks[key]
		if !ok {
			return false // unresolved blocker renders elsewhere (invariant 8), does not block
		}
		if _, isCanceled := canceled[key]; isCanceled {
			return false
		}
		rung, _ := observedRung(blocker, claims[key], gates, gateKey, live)
		return rung != RungDone && rung != RungReleased
	}
	if t.Contract != nil {
		for _, dep := range t.Contract.Deps {
			if open(dep) {
				return true
			}
		}
	}
	for key := range blockers[t.Key] {
		if open(key) {
			return true
		}
	}
	return false
}

// Progress folds a spec's tasks into per-rung counts — the projection that
// replaces hand-edited status tables (design.md §6.4).
func Progress(ws WorkSet, specKey string, r FoldResult) map[Rung]int {
	counts := map[Rung]int{}
	for _, t := range ws.Tasks {
		if t.Spec != specKey {
			continue
		}
		counts[r.Lifecycles[t.Key].Rung]++
	}
	return counts
}

func (o Observation) mustPR() string {
	p, _ := o.prPayload()
	if p.PR != "" {
		return p.PR
	}
	return "branch:" + p.Branch
}

func mergeKeys(into, add []string) []string {
	seen := map[string]bool{}
	for _, k := range into {
		seen[k] = true
	}
	for _, k := range add {
		if !seen[k] {
			seen[k] = true
			into = append(into, k)
		}
	}
	return into
}

func overlaps(a, b []string) bool {
	set := map[string]bool{}
	for _, x := range a {
		set[x] = true
	}
	for _, y := range b {
		if set[y] {
			return true
		}
	}
	return false
}

func sortedTasks(ts []Task) []Task {
	out := append([]Task(nil), ts...)
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func prClaimsAnything(pr *prState, claims map[string][]*prState) bool {
	for _, list := range claims {
		for _, p := range list {
			if p == pr {
				return true
			}
		}
	}
	return false
}
