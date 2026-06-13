package work

//go:generate go run ./schema/gen schema/work.schema.json

// APIVersion is the work-plane apiVersion stamped on every entity envelope and
// sealed object (data-model.md §2).
const APIVersion = "orun.io/v1"

// Kind enumerates the work entity kinds. They join the same typed relation
// graph as catalog entities (WD-4).
type Kind string

// The closed set of entity kinds (data-model.md §2).
const (
	KindInitiative Kind = "Initiative"
	KindEpic       Kind = "Epic"
	KindTask       Kind = "Task"
)

// Kinds is the closed set of valid entity kinds.
var Kinds = map[Kind]bool{
	KindInitiative: true,
	KindEpic:       true,
	KindTask:       true,
}

// Item is the work entity envelope (data-model.md §2). One envelope serves all
// three kinds; kind-specific behavior lives in the mutators and projection, not
// in extra shapes.
//
// The mutable runtime fields (status, assignees, ordering, counters) live in the
// StatusRow projection, never in the envelope — the envelope is what seals, and
// hot state must never enter the content-addressed graph (CR-1, invariant 1).
type Item struct {
	APIVersion string            `json:"apiVersion"`
	Kind       Kind              `json:"kind"`
	ID         string            `json:"id"`
	Key        string            `json:"key"`
	Project    string            `json:"project"`
	Title      string            `json:"title"`
	Doc        string            `json:"doc,omitempty"`
	Parent     string            `json:"parent,omitempty"`
	Cycle      string            `json:"cycle,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
	Contract   *Contract         `json:"contract,omitempty"`
	CreatedBy  Actor             `json:"createdBy"`
	CreatedAt  string            `json:"createdAt"`
}

// Contract is the task contract (data-model.md §3): the spec-milestone
// convention (goal / affects / doneWhen / gates / designRefs / deps) promoted to
// schema. All fields are individually optional; structural Complete-ness plus
// resolution of every affects key derives agent-readiness.
type Contract struct {
	Goal       string   `json:"goal,omitempty"`
	Affects    []string `json:"affects,omitempty"`
	DoneWhen   []string `json:"doneWhen,omitempty"`
	Gates      []string `json:"gates,omitempty"`
	DesignRefs []string `json:"designRefs,omitempty"`
	Deps       []string `json:"deps,omitempty"`
}

// Complete reports whether the contract is structurally complete: a goal, at
// least one affects entry, at least one doneWhen entry, and at least one gate.
// Resolution of the affects keys against the catalog (the second half of
// agent-readiness) is the delivery bridge's job (W2); this is the structural
// half that the model can decide on its own.
func (c *Contract) Complete() bool {
	if c == nil {
		return false
	}
	return c.Goal != "" && len(c.Affects) > 0 && len(c.DoneWhen) > 0 && len(c.Gates) > 0
}

// AgentReady reports whether the contract is complete and every affects key
// resolves. resolved is the catalog lookup; a nil resolver treats every key as
// resolved (used before the delivery bridge lands).
func (c *Contract) AgentReady(resolved func(componentKey string) bool) bool {
	if !c.Complete() {
		return false
	}
	if resolved == nil {
		return true
	}
	for _, k := range c.Affects {
		if !resolved(k) {
			return false
		}
	}
	return true
}
