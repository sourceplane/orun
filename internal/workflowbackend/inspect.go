package workflowbackend

import (
	"fmt"
	"sort"

	yaml "gopkg.in/yaml.v3"
)

// Inspection is what orun reads out of a pinned workflow file at compile time:
// the declared NAMES that become plan content (orun-workflows-v2 §4/§5, "only
// names are intent"). Connections are the connection names the workflow's steps
// reference; Outputs are the declared spec.outputs keys.
type Inspection struct {
	Connections []string
	Outputs     []string
}

// inspectDoc is the tolerant read shape: only the fields orun needs, everything
// else ignored. A file this reader cannot parse fails compilation for workflow
// steps only — fail-closed and scoped (S-2).
type inspectDoc struct {
	Spec struct {
		Steps []struct {
			Connection string `yaml:"connection"`
		} `yaml:"steps"`
		Outputs map[string]string `yaml:"outputs"`
	} `yaml:"spec"`
}

// InspectWorkflow extracts the declared connection names and output names from
// a workflow document. Deterministic: results are sorted and de-duplicated.
func InspectWorkflow(data []byte) (Inspection, error) {
	var doc inspectDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return Inspection{}, fmt.Errorf("parse workflow for inspection: %w", err)
	}
	connSet := map[string]struct{}{}
	for _, s := range doc.Spec.Steps {
		if s.Connection != "" {
			connSet[s.Connection] = struct{}{}
		}
	}
	insp := Inspection{}
	for name := range connSet {
		insp.Connections = append(insp.Connections, name)
	}
	sort.Strings(insp.Connections)
	for name := range doc.Spec.Outputs {
		insp.Outputs = append(insp.Outputs, name)
	}
	sort.Strings(insp.Outputs)
	return insp, nil
}

// ValidateGrant enforces the connections grant (design §4) against a workflow's
// inspection: every connection the workflow declares MUST be mapped, and every
// mapping MUST name a declared connection. The returned error prints the exact
// block to paste (S-8).
func ValidateGrant(where string, declared []string, granted map[string]map[string]string) error {
	grantedSet := map[string]struct{}{}
	for name := range granted {
		grantedSet[name] = struct{}{}
	}
	var missing []string
	declaredSet := map[string]struct{}{}
	for _, name := range declared {
		declaredSet[name] = struct{}{}
		if _, ok := grantedSet[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		msg := where + ": workflow declares connections that are not granted.\nAdd a connections: block mapping each to a secret reference, e.g.:\n\n  connections:"
		for _, name := range missing {
			msg += "\n    " + name + ":\n      token: secret://<workspace>/<project>/<env>/<KEY>"
		}
		return fmt.Errorf("%s", msg)
	}
	for name := range grantedSet {
		if _, ok := declaredSet[name]; !ok {
			return fmt.Errorf("%s: connections grant names %q, but the workflow declares no such connection (stale or misspelled grant)", where, name)
		}
	}
	return nil
}
