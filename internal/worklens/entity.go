package worklens

import (
	"fmt"
	"regexp"
	"strings"
)

// Actor is a membership subject acting on the coordination log (WP-8:
// principals are the platform's principals — usr_/sp_/team_ ids; there is
// no work-local identity).
type Actor struct {
	Type ActorType `json:"type"`
	ID   string    `json:"id"`
	Via  string    `json:"via,omitempty"` // console | mcp | cli | import
}

// Validate enforces the mandatory-actor invariant (invariant 3).
func (a Actor) Validate() error {
	if !IsActorType(a.Type) {
		return fmt.Errorf("worklens: unknown actor type %q", a.Type)
	}
	if a.ID == "" {
		return fmt.Errorf("worklens: actor id is required")
	}
	return nil
}

// Contract is the task contract (data-model.md §3): the spec-milestone
// convention as schema. All fields optional individually; Complete derives
// Ready — the same predicate as agent-ready.
type Contract struct {
	Goal       string   `json:"goal,omitempty"`
	Affects    []string `json:"affects,omitempty"`
	DoneWhen   []string `json:"doneWhen,omitempty"`
	Gates      []string `json:"gates,omitempty"`
	DesignRefs []string `json:"designRefs,omitempty"`
	Deps       []string `json:"deps,omitempty"`

	// GatesDefined distinguishes an explicit empty gate set (merge alone
	// may reach Done — P-7's `gates: []` posture) from gates simply never
	// having been declared (merge parks In Review, gates unknown).
	GatesDefined bool `json:"gatesDefined,omitempty"`
}

// Complete reports contract completeness: goal + ≥1 affects + ≥1 doneWhen +
// gates declared. Completeness derives the Ready rung and agent-readiness —
// one definition of "actionable" for humans and agents (design.md §5).
func (c *Contract) Complete() bool {
	if c == nil {
		return false
	}
	gatesDeclared := c.GatesDefined || len(c.Gates) > 0
	return c.Goal != "" && len(c.Affects) > 0 && len(c.DoneWhen) > 0 && gatesDeclared
}

// Spec is the grouping document of intent — surfaced as the EPIC since v4
// (V4-C: the wire kind stays Spec; the epic is the unit that gets reviewed,
// approved, and dispatched, and the spec document is what it knows). The
// envelope is intent-plane only: nothing observation-mutable lives in it.
type Spec struct {
	APIVersion string            `json:"apiVersion"`
	Kind       Kind              `json:"kind"`
	ID         string            `json:"id,omitempty"`
	Key        string            `json:"key"`
	Workspace  string            `json:"workspace"`
	Title      string            `json:"title"`
	DocRef     string            `json:"docRef,omitempty"`     // content-addressed doc body
	Initiative string            `json:"initiative,omitempty"` // partOf target (v4); empty = unfiled
	TargetDate string            `json:"targetDate,omitempty"` // YYYY-MM-DD intent (v4)
	Labels     map[string]string `json:"labels,omitempty"`
	CreatedBy  Actor             `json:"createdBy"`
	CreatedAt  string            `json:"createdAt,omitempty"`
}

// Task is the atom. Title is the only authoring requirement; a complete
// contract makes it Ready; everything after Ready is observed.
type Task struct {
	APIVersion string            `json:"apiVersion"`
	Kind       Kind              `json:"kind"`
	ID         string            `json:"id,omitempty"`
	Key        string            `json:"key"`
	Workspace  string            `json:"workspace"`
	Spec       string            `json:"spec,omitempty"`      // partOf target; empty = inbox
	Milestone  string            `json:"milestone,omitempty"` // milestone key within Spec (v4); requires Spec
	Title      string            `json:"title"`
	Labels     map[string]string `json:"labels,omitempty"`
	Contract   *Contract         `json:"contract,omitempty"`
	CreatedBy  Actor             `json:"createdBy"`
	CreatedAt  string            `json:"createdAt,omitempty"`
}

// Initiative is the top of the v4 hierarchy: a human-defined business
// objective — the why. Envelope-only; its health and progress are folds over
// member epics (rollup.go), never fields.
type Initiative struct {
	APIVersion      string            `json:"apiVersion"`
	Kind            Kind              `json:"kind"`
	ID              string            `json:"id,omitempty"`
	Key             string            `json:"key"`
	Workspace       string            `json:"workspace"`
	Title           string            `json:"title"`
	Owner           string            `json:"owner,omitempty"`      // membership subject id
	TargetDate      string            `json:"targetDate,omitempty"` // YYYY-MM-DD intent
	SuccessCriteria []string          `json:"successCriteria,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	CreatedBy       Actor             `json:"createdBy"`
	CreatedAt       string            `json:"createdAt,omitempty"`
}

// DesignContext seals what a design assumed: the catalog snapshot and the
// two log cursors at creation time. A design is honest about its world.
type DesignContext struct {
	Catalog  string `json:"catalog,omitempty"` // CatalogSnapshot content id
	CoordSeq int64  `json:"coordSeq"`
	ObsSeq   int64  `json:"obsSeq"`
}

// Design is the v4 noun for "the what": a doc revision chain plus a
// structured Proposal of the epics/milestones/task-skeletons it would mint.
// Produced by a design run (AG8), a human session, or import — many designs
// per initiative; adoption mints epics and freezes the record (V4-4).
type Design struct {
	APIVersion string            `json:"apiVersion"`
	Kind       Kind              `json:"kind"`
	ID         string            `json:"id,omitempty"`
	Key        string            `json:"key"` // DSG-n, workspace-scoped
	Workspace  string            `json:"workspace"`
	Initiative string            `json:"initiative"` // hasDesign edge; exactly one
	Title      string            `json:"title"`
	DocRef     string            `json:"docRef,omitempty"`
	Context    DesignContext     `json:"context"`
	Proposal   *Proposal         `json:"proposal,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
	CreatedBy  Actor             `json:"createdBy"`
	CreatedAt  string            `json:"createdAt,omitempty"`
}

// Milestone is an epic-scoped checkpoint (V4-D): the repo's own
// implementation-plan ladder convention (WP0 → WH6) as schema. Addressed as
// "<epic-key>#<key>"; approval covers the ladder via LadderHash.
type Milestone struct {
	Key        string   `json:"key"`
	Title      string   `json:"title"`
	Goal       string   `json:"goal,omitempty"`
	DoneWhen   []string `json:"doneWhen,omitempty"`
	TargetDate string   `json:"targetDate,omitempty"`
	Ordinal    int      `json:"ordinal"`
}

// ProposalTaskSkeleton is a task a design proposes under a milestone; it
// lands as an ordinary Draft task at adoption.
type ProposalTaskSkeleton struct {
	Milestone string    `json:"milestone,omitempty"`
	Title     string    `json:"title"`
	Contract  *Contract `json:"contract,omitempty"`
}

// ProposalEpic is one epic a design proposes, with its milestone ladder and
// optional task skeletons.
type ProposalEpic struct {
	Slug          string                 `json:"slug"`
	Title         string                 `json:"title"`
	DocSeed       string                 `json:"docSeed,omitempty"` // initial epic doc revision
	Milestones    []Milestone            `json:"milestones,omitempty"`
	TaskSkeletons []ProposalTaskSkeleton `json:"taskSkeletons,omitempty"`
}

// Proposal is the structured half of a design — canonical, digest-covered by
// the design's revision, and the exact input adoption mints from.
type Proposal struct {
	Epics []ProposalEpic `json:"epics"`
}

var (
	prefixRe = regexp.MustCompile(`^[A-Z]{2,5}$`)
	slugRe   = regexp.MustCompile(`^[a-z0-9-]+$`)
	taskKeyRe = regexp.MustCompile(`^([A-Z]{2,5})-([1-9][0-9]*)$`)
	// Milestone keys mirror the ladder convention: WH2, PM0, M1.
	milestoneKeyRe = regexp.MustCompile(`^[A-Z]{1,6}[0-9]{1,3}[a-z]?$`)
)

// ValidPrefix reports whether p is a legal task-key prefix (2–5 uppercase).
func ValidPrefix(p string) bool { return prefixRe.MatchString(p) }

// ValidSlug reports whether s is a legal spec slug.
func ValidSlug(s string) bool { return slugRe.MatchString(s) }

// ParseTaskKey splits a human task key ("ORN-142") into prefix and sequence
// text; ok is false when key is not a task key.
func ParseTaskKey(key string) (prefix, seq string, ok bool) {
	m := taskKeyRe.FindStringSubmatch(key)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// TaskKeysIn extracts every distinct task key mentioned in free text
// (branch names, PR titles) in order of first appearance — the auto-claim
// short-circuit (design.md §6).
func TaskKeysIn(text string) []string {
	re := regexp.MustCompile(`[A-Z]{2,5}-[1-9][0-9]*`)
	seen := map[string]bool{}
	var keys []string
	for _, k := range re.FindAllString(text, -1) {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	return keys
}

// ValidMilestoneKey reports whether k is a legal milestone key (WH2, M1).
func ValidMilestoneKey(k string) bool { return milestoneKeyRe.MatchString(k) }

// MilestoneSubject renders the addressable sub-item subject for a milestone
// within an epic: "<epic-key>#<milestone-key>" (design §1.1). Coordination
// events (comments, edits) target this string; the logs treat subjects as
// opaque keys, so milestones get timelines with zero log changes.
func MilestoneSubject(epicKey, milestoneKey string) string {
	return epicKey + "#" + milestoneKey
}

// ParseMilestoneSubject splits "<epic-key>#<milestone-key>"; ok is false
// when subject is not a milestone subject.
func ParseMilestoneSubject(subject string) (epicKey, milestoneKey string, ok bool) {
	i := strings.LastIndex(subject, "#")
	if i <= 0 || i == len(subject)-1 {
		return "", "", false
	}
	epicKey, milestoneKey = subject[:i], subject[i+1:]
	if !ValidMilestoneKey(milestoneKey) {
		return "", "", false
	}
	return epicKey, milestoneKey, true
}

// ValidateSpec checks a Spec envelope's identity core.
func ValidateSpec(s Spec) error {
	if s.Kind != KindSpec {
		return fmt.Errorf("worklens: spec envelope has kind %q", s.Kind)
	}
	if s.Key == "" || s.Workspace == "" || s.Title == "" {
		return fmt.Errorf("worklens: spec %q: key, workspace, and title are required", s.Key)
	}
	slug := s.Key
	if i := strings.LastIndex(s.Key, "/"); i >= 0 {
		slug = s.Key[i+1:]
	}
	if !ValidSlug(slug) {
		return fmt.Errorf("worklens: spec slug %q must match %s", slug, slugRe)
	}
	return s.CreatedBy.Validate()
}

// ValidateTask checks a Task envelope's identity core.
func ValidateTask(t Task) error {
	if t.Kind != KindTask {
		return fmt.Errorf("worklens: task envelope has kind %q", t.Kind)
	}
	if t.Key == "" || t.Workspace == "" || t.Title == "" {
		return fmt.Errorf("worklens: task %q: key, workspace, and title are required", t.Key)
	}
	if _, _, ok := ParseTaskKey(t.Key); !ok {
		return fmt.Errorf("worklens: task key %q must be <PREFIX>-<seq>", t.Key)
	}
	if t.Milestone != "" {
		if t.Spec == "" {
			return fmt.Errorf("worklens: task %q has a milestone but no spec — a milestone lives inside exactly one epic (design §1.2)", t.Key)
		}
		if !ValidMilestoneKey(t.Milestone) {
			return fmt.Errorf("worklens: task %q milestone key %q must match %s", t.Key, t.Milestone, milestoneKeyRe)
		}
	}
	return t.CreatedBy.Validate()
}

// ValidateInitiative checks an Initiative envelope's identity core.
func ValidateInitiative(in Initiative) error {
	if in.Kind != KindInitiative {
		return fmt.Errorf("worklens: initiative envelope has kind %q", in.Kind)
	}
	if in.Key == "" || in.Workspace == "" || in.Title == "" {
		return fmt.Errorf("worklens: initiative %q: key, workspace, and title are required", in.Key)
	}
	return in.CreatedBy.Validate()
}

// ValidateDesign checks a Design envelope: identity core, the mandatory
// initiative (hasDesign is exactly-one), and milestone-key/slug hygiene in
// the proposal so a malformed proposal fails at write time, not at adoption.
func ValidateDesign(d Design) error {
	if d.Kind != KindDesign {
		return fmt.Errorf("worklens: design envelope has kind %q", d.Kind)
	}
	if d.Key == "" || d.Workspace == "" || d.Title == "" {
		return fmt.Errorf("worklens: design %q: key, workspace, and title are required", d.Key)
	}
	if d.Initiative == "" {
		return fmt.Errorf("worklens: design %q must belong to exactly one initiative", d.Key)
	}
	if d.Proposal != nil {
		for _, pe := range d.Proposal.Epics {
			if pe.Slug == "" || pe.Title == "" {
				return fmt.Errorf("worklens: design %q proposal epic needs slug and title", d.Key)
			}
			if !ValidSlug(pe.Slug) {
				return fmt.Errorf("worklens: design %q proposal epic slug %q must match %s", d.Key, pe.Slug, slugRe)
			}
			ladder := map[string]bool{}
			for _, m := range pe.Milestones {
				if !ValidMilestoneKey(m.Key) || m.Title == "" {
					return fmt.Errorf("worklens: design %q proposal epic %q milestone %q needs a valid key and title", d.Key, pe.Slug, m.Key)
				}
				ladder[m.Key] = true
			}
			for _, ts := range pe.TaskSkeletons {
				if ts.Title == "" {
					return fmt.Errorf("worklens: design %q proposal epic %q has a titleless task skeleton", d.Key, pe.Slug)
				}
				if ts.Milestone != "" && !ladder[ts.Milestone] {
					return fmt.Errorf("worklens: design %q proposal epic %q task %q names milestone %q not in the ladder", d.Key, pe.Slug, ts.Title, ts.Milestone)
				}
			}
		}
	}
	return d.CreatedBy.Validate()
}
