package catalogresolve

import (
	"sort"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// validate is stage 9 of the resolver. It applies the rule table from
// resolution-pipeline.md §6 against every resolved manifest plus
// resolver-wide checks (uniqueness, cycles). Issues are appended in a
// stable order: per-manifest rules in (sourceFile, code) order, then
// resolver-wide rules in (code, key) order.
//
// Strict mode promotes every Warning to Error; in default mode warnings
// are collected but the resolver continues. The caller decides when to
// abort based on whether any issue has SeverityError.
func validate(authored []AuthoredManifest, manifests []*catalogmodel.ComponentManifest, strict bool) []ValidationIssue {
	var issues []ValidationIssue

	add := func(i ValidationIssue) {
		if strict && i.Severity == SeverityWarning {
			i.Severity = SeverityError
		}
		issues = append(issues, i)
	}

	// Per-component rules.
	for i := range manifests {
		cm := manifests[i]
		am := authored[i]
		file := am.SourceFile

		// metadata.name set — error in both modes.
		if cm.Identity.Name == "" {
			add(ValidationIssue{
				File:     file,
				Pointer:  "/metadata/name",
				Code:     "component.metadata.name.missing",
				Message:  "metadata.name is required",
				Severity: SeverityError,
			})
		}

		// component name segment shape — warn default, error strict.
		if cm.Identity.Name != "" {
			if err := catalogmodel.ValidateComponentKey(cm.Identity.ComponentKey); err != nil {
				add(ValidationIssue{
					File:     file,
					Pointer:  "/metadata/name",
					Code:     "component.name.invalid",
					Message:  "component name segments must match [a-z0-9._-]+: " + cm.Identity.ComponentKey,
					Severity: SeverityWarning,
				})
			}
		}

		// metadata.owner — warn default, error strict.
		if cm.Metadata.Owner == "" {
			add(ValidationIssue{
				File:     file,
				Pointer:  "/spec/owner",
				Code:     "component.metadata.owner.missing",
				Message:  "metadata.owner (spec.owner) should be set",
				Severity: SeverityWarning,
			})
		}

		// spec.lifecycle — warn default, error strict.
		if cm.Spec.Lifecycle == "" {
			add(ValidationIssue{
				File:     file,
				Pointer:  "/spec/lifecycle",
				Code:     "component.spec.lifecycle.missing",
				Message:  "spec.lifecycle should be set",
				Severity: SeverityWarning,
			})
		}
	}

	// Resolver-wide: component key uniqueness — error in both modes.
	dupes := map[string][]string{}
	for i := range manifests {
		key := manifests[i].Identity.ComponentKey
		dupes[key] = append(dupes[key], authored[i].SourceFile)
	}
	dupKeys := make([]string, 0)
	for k, paths := range dupes {
		if len(paths) > 1 {
			dupKeys = append(dupKeys, k)
		}
	}
	sort.Strings(dupKeys)
	for _, k := range dupKeys {
		paths := append([]string(nil), dupes[k]...)
		sort.Strings(paths)
		add(ValidationIssue{
			File:     "",
			Pointer:  "",
			Code:     "component.key.duplicate",
			Message:  "duplicate componentKey " + k,
			Severity: SeverityError,
			Detail: map[string]any{
				"componentKey": k,
				"paths":        paths,
			},
		})
	}

	// Cycle detection per resolution-pipeline.md §5: deploy-after cycles
	// are errors regardless of strict; calls/depends-on cycles are
	// warnings (promoted in strict).
	for _, edge := range []string{catalogmodel.RelDeployAfter, catalogmodel.RelCalls, catalogmodel.RelDependsOn} {
		cycles := findCycles(manifests, edge)
		for _, cyc := range cycles {
			sev := SeverityWarning
			if edge == catalogmodel.RelDeployAfter {
				sev = SeverityError
			}
			issue := ValidationIssue{
				Code:     "component.dependency.cycle",
				Message:  "cycle detected in " + edge + " edges",
				Severity: sev,
				Detail: map[string]any{
					"edge":  edge,
					"cycle": cyc,
				},
			}
			if sev == SeverityWarning && strict {
				issue.Severity = SeverityError
			}
			issues = append(issues, issue)
		}
	}

	// Final stable ordering: by (severity desc, code, file, pointer).
	sort.SliceStable(issues, func(a, b int) bool {
		if issues[a].Severity != issues[b].Severity {
			return issues[a].Severity > issues[b].Severity
		}
		if issues[a].Code != issues[b].Code {
			return issues[a].Code < issues[b].Code
		}
		if issues[a].File != issues[b].File {
			return issues[a].File < issues[b].File
		}
		return issues[a].Pointer < issues[b].Pointer
	})

	return issues
}

// hasError reports whether any issue in the list is SeverityError.
func hasError(issues []ValidationIssue) bool {
	for _, i := range issues {
		if i.Severity == SeverityError {
			return true
		}
	}
	return false
}

// firstError returns a pointer to the first SeverityError in the list,
// or nil if none. Used by the resolver to surface a typed error
// alongside the issue list.
func firstError(issues []ValidationIssue) *ValidationIssue {
	for i := range issues {
		if issues[i].Severity == SeverityError {
			return &issues[i]
		}
	}
	return nil
}
