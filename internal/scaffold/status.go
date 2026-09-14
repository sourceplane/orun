package scaffold

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Derived phase state (orun-bootstrap-engine BE-O2).
//
// # The rule, and why it is a rule
//
// **Phase state is DERIVED. A stored file is a cache and must be safe to
// delete.** Not a preference — a property the paced bootstrap already depends
// on. `.orun/*` is gitignored in the baselines and in every product they
// scaffold, so the per-phase records those flows archive have never survived a
// container. And the bootstrap works anyway, because every phase re-derives:
// re-apply, and an empty diff means it was already placed.
//
// That is what lets someone run one phase today and the next in a fresh
// container months later. A ledger would answer faster and would quietly
// become the source of truth, and the first time it disagreed with the tree
// nobody would know which to believe.
//
// So Derive renders the blueprint exactly as Run would — same inputs, same
// order, same bytes — and compares the result against what is on disk. It
// writes nothing.

// PhaseState is one phase's derived condition.
type PhaseState string

const (
	// PhaseDone: every file this phase places is present and identical.
	PhaseDone PhaseState = "done"
	// PhasePending: none of this phase's files are on disk.
	PhasePending PhaseState = "pending"
	// PhasePartial: some files are present and some are not — an interrupted
	// run. Re-running the phase completes it.
	PhasePartial PhaseState = "partial"
	// PhaseDrifted: every file is present but at least one differs. Someone
	// edited the product, or the blueprint moved. Re-running would overwrite,
	// so this is reported and never silently resolved.
	PhaseDrifted PhaseState = "drifted"
)

// PhaseStatus is what Derive reports per phase.
type PhaseStatus struct {
	Name  string     `json:"name" yaml:"name"`
	State PhaseState `json:"state" yaml:"state"`
	// Files is how many files the phase places.
	Files int `json:"files" yaml:"files"`
	// Missing and Drifted name the paths, sorted, for a human to act on.
	Missing []string `json:"missing,omitempty" yaml:"missing,omitempty"`
	Drifted []string `json:"drifted,omitempty" yaml:"drifted,omitempty"`
}

// Status is the whole derivation.
type Status struct {
	// Phases in declared order.
	Phases []PhaseStatus `json:"phases" yaml:"phases"`
	// InputsHash and Inputs are recovered from the product's own
	// provenance.lock when there is one — the inputs a resumed run must reuse
	// rather than ask for again.
	InputsHash string         `json:"inputsHash,omitempty" yaml:"inputsHash,omitempty"`
	Inputs     map[string]any `json:"inputs,omitempty" yaml:"inputs,omitempty"`
	// Next is the first phase that is not done, or empty when all are.
	Next string `json:"next,omitempty" yaml:"next,omitempty"`
}

// Done reports whether every phase is done.
func (s *Status) Done() bool { return s.Next == "" }

// Derive computes each phase's state without writing anything.
func Derive(ctx context.Context, opts Options) (*Status, error) {
	plan, err := buildPlan(ctx, opts)
	if err != nil {
		return nil, err
	}
	status := &Status{}
	if prov, err := ReadProvenance(opts.OutDir); err == nil {
		status.InputsHash = prov.InputsHash
		status.Inputs = prov.Inputs
	}
	for _, phase := range plan.phases {
		status.Phases = append(status.Phases, derivePhase(opts.OutDir, phase.Name, plan.byPhase[phase.Name]))
	}
	for _, p := range status.Phases {
		if p.State != PhaseDone {
			status.Next = p.Name
			break
		}
	}
	return status, nil
}

// derivePhase compares one phase's rendered files against the tree.
func derivePhase(outDir, name string, files map[string]PlacedFile) PhaseStatus {
	st := PhaseStatus{Name: name, Files: len(files)}
	// A phase that places nothing — every module consume-mode — is done by
	// definition; there is nothing that could be missing.
	if len(files) == 0 {
		st.State = PhaseDone
		return st
	}
	for rel, want := range files {
		got, err := os.ReadFile(filepath.Join(outDir, filepath.FromSlash(rel)))
		switch {
		case err != nil:
			st.Missing = append(st.Missing, rel)
		case !bytes.Equal(got, want.Bytes):
			st.Drifted = append(st.Drifted, rel)
		}
	}
	sort.Strings(st.Missing)
	sort.Strings(st.Drifted)
	switch {
	case len(st.Missing) == len(files):
		st.State = PhasePending
	case len(st.Missing) > 0:
		st.State = PhasePartial
	case len(st.Drifted) > 0:
		st.State = PhaseDrifted
	default:
		st.State = PhaseDone
	}
	return st
}

// String renders a status for a terminal, one line per phase.
func (s *Status) String() string {
	var b strings.Builder
	for _, p := range s.Phases {
		mark := map[PhaseState]string{
			PhaseDone: "✓", PhasePending: "·", PhasePartial: "◐", PhaseDrifted: "!",
		}[p.State]
		fmt.Fprintf(&b, "%s %-24s %-8s %d file(s)", mark, p.Name, p.State, p.Files)
		if n := len(p.Missing); n > 0 {
			fmt.Fprintf(&b, " — %d missing", n)
		}
		if n := len(p.Drifted); n > 0 {
			fmt.Fprintf(&b, " — %d drifted", n)
		}
		b.WriteString("\n")
	}
	if s.Next == "" {
		b.WriteString("\nall phases done\n")
	} else {
		fmt.Fprintf(&b, "\nnext: %s\n", s.Next)
	}
	return b.String()
}

// RecoverInputs reads the inputs a previous run recorded, so a resumed run
// reuses them instead of asking again.
//
// A caller-supplied value that DISAGREES with the record is refused, naming
// both. It is not an override: half a product rendered with one value and half
// with another is a silent, expensive failure, and changing an input after the
// fact is upgrade's operation, not resume's.
func RecoverInputs(outDir string, given map[string]string) (map[string]string, error) {
	prov, err := ReadProvenance(outDir)
	if err != nil {
		// No lock is not an error: this may be a first run.
		return given, nil
	}
	out := make(map[string]string, len(prov.Inputs)+len(given))
	for name, value := range prov.Inputs {
		out[name] = fmt.Sprint(value)
	}
	var conflicts []string
	for name, value := range given {
		if recorded, ok := out[name]; ok && recorded != value {
			conflicts = append(conflicts, fmt.Sprintf("%s: recorded %q, given %q", name, recorded, value))
			continue
		}
		out[name] = value
	}
	if len(conflicts) > 0 {
		sort.Strings(conflicts)
		return nil, gateErr(
			"input conflict with %s — a resumed run reuses what the product was built with:\n  %s\n"+
				"Changing an input after the fact re-renders half the tree and leaves the other half; that is `upgrade`, not `resume`.",
			ProvenanceRelPath, strings.Join(conflicts, "\n  "))
	}
	return out, nil
}
