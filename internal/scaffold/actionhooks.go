package scaffold

import (
	"fmt"

	yaml "gopkg.in/yaml.v3"

	"github.com/sourceplane/orun/internal/actions"
)

// Parse-time validation of action hooks (orun-bootstrap-engine BE-O1).
//
// A hook that says `uses:` is checked against the action's DECLARED parameters
// before anything is placed. The point is the failure mode it removes: without
// this, a misspelled parameter is discovered when the action runs — which, in a
// bootstrap, is partway through writing a customer's repository, after several
// phases have already landed.
//
// Errors carry a LINE NUMBER, which is why this file re-decodes the document
// into a yaml.Node. The struct decode that ParseBlueprint already performs
// throws positions away, and "action orun.http/probe@v1 has no parameter
// urls2" is markedly less useful than the same sentence with the line it is on.

// hookLocator indexes hook mappings by their document path so a validation
// error can name the line its offending key sits on.
type hookLocator struct {
	nodes map[string]*yaml.Node
}

// mapValue returns the value node for a key in a mapping node, or nil.
func mapValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// newHookLocator walks the raw document and records every hook mapping under
// the two places hooks may appear. A document that does not parse as YAML is
// not this function's problem — ParseBlueprint has already reported it — so a
// failure here yields an empty locator and validation simply omits lines.
func newHookLocator(data []byte) *hookLocator {
	loc := &hookLocator{nodes: map[string]*yaml.Node{}}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) == 0 {
		return loc
	}
	root := doc.Content[0]

	if phases := mapValue(root, "phases"); phases != nil && phases.Kind == yaml.SequenceNode {
		for pi, phase := range phases.Content {
			hooks := mapValue(phase, "hooks")
			if hooks == nil || hooks.Kind != yaml.SequenceNode {
				continue
			}
			for hi, hook := range hooks.Content {
				loc.nodes[fmt.Sprintf("phases[%d].hooks[%d]", pi, hi)] = hook
			}
		}
	}
	if hooks := mapValue(root, "hooks"); hooks != nil {
		if post := mapValue(hooks, "postInstantiate"); post != nil && post.Kind == yaml.SequenceNode {
			for hi, hook := range post.Content {
				loc.nodes[fmt.Sprintf("hooks.postInstantiate[%d]", hi)] = hook
			}
		}
	}
	return loc
}

// line reports the 1-based line of a hook's `uses` value, or of one key inside
// its `with` block when param is non-empty. Zero means "not located", which
// callers render as no line rather than as line 0.
func (l *hookLocator) line(path, param string) int {
	hook := l.nodes[path]
	if hook == nil {
		return 0
	}
	if param == "" {
		if uses := mapValue(hook, "uses"); uses != nil {
			return uses.Line
		}
		return hook.Line
	}
	with := mapValue(hook, "with")
	if with == nil || with.Kind != yaml.MappingNode {
		return hook.Line
	}
	for i := 0; i+1 < len(with.Content); i += 2 {
		if with.Content[i].Value == param {
			return with.Content[i].Line
		}
	}
	return with.Line
}

// atLine prefixes a message with its line when one was located.
func atLine(line int, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if line > 0 {
		return fmt.Errorf("line %d: %s", line, msg)
	}
	return fmt.Errorf("%s", msg)
}

// paramOf extracts the parameter name from an actions.Validate error, so the
// line reported is the parameter's own rather than the hook's. Empty when the
// error is not about a specific parameter.
func paramOf(err error, with map[string]any) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	for name := range with {
		if containsQuoted(msg, name) {
			return name
		}
	}
	return ""
}

// containsQuoted reports whether msg mentions `"name"`.
func containsQuoted(msg, name string) bool {
	q := `"` + name + `"`
	for i := 0; i+len(q) <= len(msg); i++ {
		if msg[i:i+len(q)] == q {
			return true
		}
	}
	return false
}

// validateActionHooks checks every `uses:` hook in the blueprint against the
// action registry. Called from ParseBlueprint, so an invalid action hook is a
// parse failure and never reaches placement.
func validateActionHooks(bp *Blueprint, loc *hookLocator) error {
	check := func(path, label string, h Hook) error {
		if !h.IsAction() {
			// A hook that carries `with:` without `uses:` is not an action, and
			// the With block would be silently ignored. Say so: a hook that
			// quietly drops half its declaration is the bug this epic exists
			// to stop shipping.
			if len(h.With) > 0 && !h.IsWorkflow() {
				return atLine(loc.line(path, ""), "%s: sets `with:` but no `uses:` — parameters belong to an action", label)
			}
			return nil
		}
		if _, known := actions.Lookup(h.Uses); !known {
			return atLine(loc.line(path, ""), "%s: %s", label, unknownActionMessage(h.Uses))
		}
		if err := actions.Validate(h.Uses, h.With); err != nil {
			return atLine(loc.line(path, paramOf(err, h.With)), "%s: %v", label, err)
		}
		return nil
	}

	for pi, ph := range bp.Phases {
		for hi, h := range ph.Hooks {
			path := fmt.Sprintf("phases[%d].hooks[%d]", pi, hi)
			label := fmt.Sprintf("phases[%d] (%s) hooks[%d] (%s)", pi, ph.Name, hi, h.ID)
			if err := check(path, label, h); err != nil {
				return err
			}
		}
	}
	for hi, h := range bp.Hooks.PostInstantiate {
		path := fmt.Sprintf("hooks.postInstantiate[%d]", hi)
		label := fmt.Sprintf("hooks.postInstantiate[%d] (%s)", hi, h.ID)
		if err := check(path, label, h); err != nil {
			return err
		}
	}
	return nil
}

// unknownActionMessage is actions.Validate's unknown-id message, reused so the
// two paths cannot describe the same mistake differently.
func unknownActionMessage(id string) string {
	err := actions.Validate(id, nil)
	if err == nil {
		return fmt.Sprintf("unknown action %q", id)
	}
	return err.Error()
}
