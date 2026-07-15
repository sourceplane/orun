package scaffold

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// Render is the locked engine seam (design §7.1): Go stdlib text/template with
// the constrained funcmap and no ambient env/os/host namespace. The model's
// dot (.) is exactly the validated inputs map, so {{ .serviceName }} resolves a
// collected input and nothing reaches the filesystem, a process, or the clock.
// Rendering is deterministic by construction: identical (body, values) ⇒
// byte-identical output.
//
// missingkey=error makes a reference to an undeclared input a hard failure
// rather than an empty string — fail closed (design §9).
func Render(name, body string, values map[string]any) ([]byte, error) {
	tmpl, err := template.New(name).
		Option("missingkey=error").
		Funcs(constrainedFuncMap()).
		Parse(body)
	if err != nil {
		return nil, fmt.Errorf("template %q: parse: %w", name, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, values); err != nil {
		return nil, fmt.Errorf("template %q: execute: %w", name, err)
	}
	return buf.Bytes(), nil
}

// RenderPath renders a single path string (a module's from/to) through the same
// engine. Paths are small and must render deterministically to a contained
// location (containment checked separately, design §9).
func RenderPath(body string, values map[string]any) (string, error) {
	out, err := Render("path", body, values)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// referencesInputs reports whether a template body references the input model
// (a `.` action). Used by the bind lint (design §4): a template outside a
// module's bind list that interpolates inputs is a lint error, keeping the
// interpolation surface auditable.
func referencesInputs(body string) bool {
	// A template action referencing the dot pipeline: {{ .Foo }}, {{.}},
	// {{ range .Items }}, etc. We look for "{{" followed (after optional
	// whitespace and keywords) by a ".". This is a conservative lint, not a
	// parser; false positives are acceptable for an auditability gate.
	for {
		i := strings.Index(body, "{{")
		if i < 0 {
			return false
		}
		rest := body[i+2:]
		if strings.ContainsRune(actionUpToClose(rest), '.') {
			return true
		}
		body = rest
	}
}

func actionUpToClose(s string) string {
	if j := strings.Index(s, "}}"); j >= 0 {
		return s[:j]
	}
	return s
}
