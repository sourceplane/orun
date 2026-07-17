package workflowbackend

import (
	"fmt"
	"regexp"
	"strings"
)

// outputRefPattern matches a cross-step output reference:
//
//	${{ steps.<stepId>.outputs.<name> }}
//
// The ${{ }} spelling is deliberate (GHA-familiar) and distinct from orun's
// compile-time {{ }} templating: output values exist only at run time, so the
// compiler validates the reference grammar against the pinned workflow's
// declared output NAMES and leaves the span intact for the runner to substitute
// (orun-workflows-v2 §5 — names are intent, values are execution).
var outputRefPattern = regexp.MustCompile(`\$\{\{\s*steps\.([A-Za-z0-9_.@-]+)\.outputs\.([A-Za-z0-9_-]+)\s*\}\}`)

// OutputRef is one parsed ${{ steps.X.outputs.Y }} reference.
type OutputRef struct {
	StepID string
	Name   string
}

// FindOutputRefs returns every output reference in s, in order.
func FindOutputRefs(s string) []OutputRef {
	var refs []OutputRef
	for _, m := range outputRefPattern.FindAllStringSubmatch(s, -1) {
		refs = append(refs, OutputRef{StepID: m[1], Name: m[2]})
	}
	return refs
}

// SubstituteOutputRefs replaces every output reference in s using lookup. A
// reference lookup cannot resolve is an error — fail-closed, a step must not
// run with a dangling reference silently left in place.
func SubstituteOutputRefs(s string, lookup func(stepID, name string) (string, bool)) (string, error) {
	var firstErr error
	out := outputRefPattern.ReplaceAllStringFunc(s, func(match string) string {
		m := outputRefPattern.FindStringSubmatch(match)
		value, ok := lookup(m[1], m[2])
		if !ok {
			if firstErr == nil {
				firstErr = fmt.Errorf("output reference %s cannot be resolved: step %q has no recorded output %q", strings.TrimSpace(match), m[1], m[2])
			}
			return match
		}
		return value
	})
	if firstErr != nil {
		return "", firstErr
	}
	return out, nil
}

// MaskOutputRefs replaces every output reference with an opaque token so a
// compile-time text/template pass leaves it untouched; UnmaskOutputRefs
// restores them. The returned restore slice is positional.
func MaskOutputRefs(s string) (masked string, spans []string) {
	masked = outputRefPattern.ReplaceAllStringFunc(s, func(match string) string {
		spans = append(spans, match)
		return fmt.Sprintf("\x00wfout%d\x00", len(spans)-1)
	})
	return masked, spans
}

// UnmaskOutputRefs restores spans captured by MaskOutputRefs.
func UnmaskOutputRefs(s string, spans []string) string {
	for i, span := range spans {
		s = strings.ReplaceAll(s, fmt.Sprintf("\x00wfout%d\x00", i), span)
	}
	return s
}
