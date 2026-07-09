package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/worklens"
)

// BriefInput is the resolved material a brief is assembled from. The runtime
// (or the CLI) gathers it — a pulled SpecSnapshot, the task contract, the
// affected set, the agent type's persona — and the assembler renders +
// seals a content-addressed nodes.AgentBrief plus the driver-facing prompt.
type BriefInput struct {
	RunKind   string // nodes.RunKind*
	Task      string // task key (implementation/fix); empty for design
	Persona   []byte // the agent type's persona body
	Literacy  []byte // base literacy (defaults to the binary's when nil)
	Contract  *worklens.Contract
	SpecID    string // sealed SpecSnapshot id, when known
	CatalogID string // CatalogSnapshot the affects resolved against
	// Affected is the frozen blast radius (component keys). Rendered into the
	// instructions and sealed as an AffectedSet blob.
	Affected []string
	SpecDoc  string // the spec doc body, when materialized
}

// AssembledBrief is the sealed brief plus the rendered instructions the driver
// runs from.
type AssembledBrief struct {
	ID           string // the AgentBrief object id
	Instructions string // rendered system prompt (also sealed as a blob)
	Node         nodes.AgentBrief
}

// AffectedSet is the sealed blast-radius blob (design.md §4 step 2): the
// affected engine's output frozen as content so a run is reproducible.
type AffectedSet struct {
	Kind       string   `json:"kind"`
	APIVersion string   `json:"apiVersion"`
	Components []string `json:"components"`
}

// AssembleBrief renders the instructions, seals the affected set and the
// instructions as content-addressed blobs, and seals the AgentBrief node that
// pins them. Same inputs → same brief id (design.md §4): a local run and a
// cloud run from one brief id are one run.
func AssembleBrief(ctx context.Context, store objectstore.ObjectStore, in BriefInput) (AssembledBrief, error) {
	if in.RunKind == "" {
		return AssembledBrief{}, fmt.Errorf("agent: brief runKind empty")
	}
	literacy := in.Literacy
	if literacy == nil {
		literacy = Literacy()
	}

	instructions := renderInstructions(literacy, in)
	instrID, err := store.PutBlob(ctx, []byte(instructions))
	if err != nil {
		return AssembledBrief{}, err
	}

	node := nodes.AgentBrief{
		Kind:         nodes.KindAgentBrief,
		APIVersion:   "orun.io/v1",
		RunKind:      in.RunKind,
		Task:         in.Task,
		Spec:         in.SpecID,
		Instructions: string(instrID),
	}
	if len(in.Affected) > 0 {
		set := AffectedSet{Kind: "AffectedSet", APIVersion: "orun.io/v1", Components: dedupSorted(in.Affected)}
		b, err := nodes.Encode(set)
		if err != nil {
			return AssembledBrief{}, err
		}
		affID, err := store.PutBlob(ctx, b)
		if err != nil {
			return AssembledBrief{}, err
		}
		node.Affected = string(affID)
	}
	if literacy != nil {
		litID, err := store.PutBlob(ctx, literacy)
		if err != nil {
			return AssembledBrief{}, err
		}
		node.Literacy = string(litID)
	}

	id, err := nodes.AssembleAgentBrief(ctx, store, node)
	if err != nil {
		return AssembledBrief{}, err
	}
	return AssembledBrief{ID: string(id), Instructions: instructions, Node: node}, nil
}

// renderInstructions layers the system prompt: base literacy, then the agent
// type's persona, then the concrete task contract and blast radius. Layered,
// not concatenated ad hoc (design.md §4 step 3).
func renderInstructions(literacy []byte, in BriefInput) string {
	var b strings.Builder
	b.Write(literacy)
	if len(in.Persona) > 0 {
		b.WriteString("\n\n---\n\n")
		b.Write(in.Persona)
	}
	b.WriteString("\n\n---\n\n# This run\n\n")
	fmt.Fprintf(&b, "Run kind: %s\n", in.RunKind)
	if in.Task != "" {
		fmt.Fprintf(&b, "Task: %s\n", in.Task)
	}
	if c := in.Contract; c != nil {
		if c.Goal != "" {
			fmt.Fprintf(&b, "\n## Goal\n\n%s\n", c.Goal)
		}
		if len(c.Affects) > 0 {
			fmt.Fprintf(&b, "\n## Affects (your blast-radius ceiling)\n\n%s\n", bullets(c.Affects))
		}
		if len(c.DoneWhen) > 0 {
			fmt.Fprintf(&b, "\n## Done when\n\n%s\n", bullets(c.DoneWhen))
		}
		if len(c.Gates) > 0 {
			fmt.Fprintf(&b, "\n## Gates (verified from orun execution truth)\n\n%s\n", bullets(c.Gates))
		}
	}
	if len(in.Affected) > 0 {
		fmt.Fprintf(&b, "\n## Affected components (frozen)\n\n%s\n", bullets(dedupSorted(in.Affected)))
	}
	if strings.TrimSpace(in.SpecDoc) != "" {
		fmt.Fprintf(&b, "\n## Spec\n\n%s\n", strings.TrimSpace(in.SpecDoc))
	}
	return b.String()
}

func bullets(items []string) string {
	var b strings.Builder
	for _, it := range items {
		fmt.Fprintf(&b, "- %s\n", it)
	}
	return strings.TrimRight(b.String(), "\n")
}

func dedupSorted(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sortStrings(out)
	return out
}
