package sourcectx

import (
	"errors"
	"path"
	"strings"
)

// ErrCIEventNoMatch is the sentinel returned by ResolveSourceSnapshot when
// the caller injects a CIEventInjection (PR/tag/event) that cannot be
// reconciled with the actual workspace state.
//
// Mirrors triggerctx.ErrNoMatchingBinding (Phase 1 §11) so the higher-level
// `--from-ci` plumbing can surface a single deterministic exit code.
//
// Callers MUST detect this with errors.Is rather than string sniffing.
var ErrCIEventNoMatch = errors.New("sourcectx: CI event injection did not match workspace state")

// CIEventNoMatchError is the structured envelope wrapping ErrCIEventNoMatch.
// It carries enough context for the CLI to render an actionable diagnostic
// without re-deriving the fields.
type CIEventNoMatchError struct {
	Provider string
	Event    string
	Action   string
	// Reason names the specific mismatch ("pr-without-head",
	// "tag-not-at-head", "scope-injected-without-repo", ...). Stable
	// strings — surface them in JSON envelopes.
	Reason string
}

func (e *CIEventNoMatchError) Error() string {
	parts := []string{"sourcectx: CI event no-match"}
	if e.Reason != "" {
		parts = append(parts, "reason="+e.Reason)
	}
	if e.Provider != "" {
		parts = append(parts, "provider="+e.Provider)
	}
	if e.Event != "" {
		parts = append(parts, "event="+e.Event)
	}
	if e.Action != "" {
		parts = append(parts, "action="+e.Action)
	}
	return strings.Join(parts, " ")
}

// Unwrap exposes the sentinel so errors.Is succeeds.
func (e *CIEventNoMatchError) Unwrap() error { return ErrCIEventNoMatch }

// CatalogRelevant reports whether a repo-relative POSIX path is in the
// catalog-relevant file set per identity-and-keys.md §7. The decision is a
// pure function of (path, inferenceFlags) — used by both the dirty-file
// probe and tests.
//
// The "always-on" set is:
//
//   - intent.yaml at any depth
//   - any file named component.yaml
//   - any file named stack.yaml or composition.yaml
//
// Inference-gated additions:
//
//   - package.json     (inference.PackageJSON)
//   - Dockerfile       (inference.Dockerfile)
//   - Chart.yaml       (inference.Helm)
//   - *.tf             (inference.Terraform)
//   - README.md        (inference.Readme)
//
// Anything else (notes.txt, vendor/foo.go, .github/workflows/*, …) is
// excluded — that exclusion is the property test T-IDK-4 asserts.
func CatalogRelevant(relPath string, flags InferenceFlags) bool {
	relPath = strings.TrimPrefix(relPath, "./")
	if relPath == "" {
		return false
	}
	base := path.Base(relPath)
	switch base {
	case "intent.yaml", "component.yaml", "stack.yaml", "composition.yaml":
		return true
	case "package.json":
		return flags.PackageJSON
	case "Dockerfile":
		return flags.Dockerfile
	case "Chart.yaml":
		return flags.Helm
	case "README.md":
		return flags.Readme
	}
	if flags.Terraform && strings.HasSuffix(base, ".tf") {
		return true
	}
	return false
}
