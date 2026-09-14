package scaffold

import (
	"fmt"
	"strings"
)

// Narration's honesty rules (orun-bootstrap-engine BE-O6).
//
// Narration is prose a BASELINE authored, rendered by the engine and shown to
// an operator as the record of what happened. Three things keep it from
// drifting into decoration, or worse, into a claim the engine cannot support.
//
//  1. It is a TEMPLATE OVER STATE, rendered through the same constrained
//     funcmap as every module — no filesystem, no exec, no network, no clock.
//  2. It MAY NOT ASSERT A STATE. `state` is the truth and narration is the
//     caption, so a line may not contain a bare state word outside an
//     expression. "The edge is live" is a description of the world; "05-edge:
//     done" is a claim the engine alone gets to make.
//  3. A missing line renders a generated one — never silence, which reads as a
//     stalled build.

// stateWords are the words narration may not assert. They are exactly the
// vocabulary the event stream owns.
var stateWords = []string{"done", "failed", "complete", "completed", "succeeded", "skipped"}

// validateNarration checks a phase's authored lines at parse time. A narration
// that lies is worse than none, and finding out at run time means finding out
// in front of the operator it lied to.
func validateNarration(where string, n *Narration) error {
	if n == nil {
		return nil
	}
	for label, line := range map[string]string{
		"start": n.Start, "await": n.Await, "done": n.Done, "failed": n.Failed,
	} {
		if err := checkNarrationLine(fmt.Sprintf("%s narrate.%s", where, label), line); err != nil {
			return err
		}
	}
	return nil
}

// checkNarrationLine rejects a line that asserts a state.
func checkNarrationLine(where, line string) error {
	if strings.TrimSpace(line) == "" {
		return nil
	}
	// Only the prose outside template expressions is checked: an expression
	// may legitimately reference a field whose name is a state word.
	prose := stripExpressions(line)
	lower := strings.ToLower(prose)
	for _, word := range stateWords {
		if containsWord(lower, word) {
			return fmt.Errorf(
				"%s: narration may not assert %q — the engine's `state` says what happened and narration is the caption. Describe the world instead (\"the edge answers /health\"), not the run",
				where, word)
		}
	}
	return nil
}

// stripExpressions removes {{ … }} spans.
func stripExpressions(s string) string {
	var b strings.Builder
	for {
		open := strings.Index(s, "{{")
		if open < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:open])
		rest := s[open+2:]
		close := strings.Index(rest, "}}")
		if close < 0 {
			return b.String()
		}
		s = rest[close+2:]
	}
}

// containsWord reports a whole-word match, so "completed" does not fire on
// "incompleteness" and "done" does not fire on "donetown".
func containsWord(haystack, word string) bool {
	for i := 0; i+len(word) <= len(haystack); i++ {
		if haystack[i:i+len(word)] != word {
			continue
		}
		if i > 0 && isWordByte(haystack[i-1]) {
			continue
		}
		if end := i + len(word); end < len(haystack) && isWordByte(haystack[end]) {
			continue
		}
		return true
	}
	return false
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// renderNarration renders an authored line against the phase's facts, falling
// back to a generated one. A template that fails to render falls back too: a
// broken caption must never fail a build that otherwise succeeded.
func renderNarration(authored, phase string, state EventState, scope map[string]any) string {
	if strings.TrimSpace(authored) == "" {
		return generatedNarration(phase, state)
	}
	out, err := Render("narrate."+phase, authored, scope)
	if err != nil {
		return generatedNarration(phase, state)
	}
	return string(out)
}
