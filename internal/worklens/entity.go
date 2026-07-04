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

// Spec is the grouping document of intent (the epic). The envelope is
// intent-plane only: nothing observation-mutable lives in it.
type Spec struct {
	APIVersion string            `json:"apiVersion"`
	Kind       Kind              `json:"kind"`
	ID         string            `json:"id,omitempty"`
	Key        string            `json:"key"`
	Workspace  string            `json:"workspace"`
	Title      string            `json:"title"`
	DocRef     string            `json:"docRef,omitempty"` // content-addressed doc body
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
	Spec       string            `json:"spec,omitempty"` // partOf target; empty = inbox
	Title      string            `json:"title"`
	Labels     map[string]string `json:"labels,omitempty"`
	Contract   *Contract         `json:"contract,omitempty"`
	CreatedBy  Actor             `json:"createdBy"`
	CreatedAt  string            `json:"createdAt,omitempty"`
}

var (
	prefixRe = regexp.MustCompile(`^[A-Z]{2,5}$`)
	slugRe   = regexp.MustCompile(`^[a-z0-9-]+$`)
	taskKeyRe = regexp.MustCompile(`^([A-Z]{2,5})-([1-9][0-9]*)$`)
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
	return t.CreatedBy.Validate()
}
