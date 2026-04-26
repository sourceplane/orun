package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/sourceplane/gluon/internal/discovery"
	"github.com/sourceplane/gluon/internal/expand"
	"github.com/sourceplane/gluon/internal/model"
	"github.com/sourceplane/gluon/internal/normalize"
	"github.com/sourceplane/gluon/internal/ui"
)

// ScopeResult holds the outcome of context-aware scoping.
type ScopeResult struct {
	DetectedComponent string
	ScopedComponents  []string
	WasAutoScoped     bool
}

// ResolveScope determines whether CWD-based scoping should apply.
// explicitComponents is the current value of --component flags.
func ResolveScope(
	intent *model.Intent,
	explicitComponents []string,
	all bool,
	jsonMode bool,
) (*ScopeResult, error) {
	result := &ScopeResult{}

	if len(explicitComponents) > 0 {
		return result, nil
	}
	if all {
		return result, nil
	}
	if intentRoot == "" {
		return result, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return result, nil
	}

	componentName, err := discovery.DetectComponentContext(cwd, intentRoot, intent.Components)
	if err != nil || componentName == "" {
		return result, nil
	}

	result.DetectedComponent = componentName

	normalized, err := normalize.NormalizeIntent(intent)
	if err != nil {
		return result, nil
	}

	resolver := expand.NewDependencyResolver(normalized)
	seedSet := map[string]bool{componentName: true}
	included := resolver.ResolveComponentSet(seedSet)

	scoped := make([]string, 0, len(included))
	for name := range included {
		scoped = append(scoped, name)
	}
	sort.Strings(scoped)

	result.ScopedComponents = scoped
	result.WasAutoScoped = true

	if !jsonMode {
		printContextBanner(componentName, scoped)
	}

	return result, nil
}

func printContextBanner(detected string, scoped []string) {
	color := ui.ColorEnabledForWriter(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s auto-scoped to component %s",
		ui.Cyan(color, "context:"), ui.Bold(color, detected))
	if len(scoped) > 1 {
		deps := make([]string, 0, len(scoped)-1)
		for _, s := range scoped {
			if s != detected {
				deps = append(deps, s)
			}
		}
		fmt.Fprintf(os.Stderr, " (+ %d %s: %s)",
			len(deps), pluralize(len(deps), "dependency", "dependencies"),
			strings.Join(deps, ", "))
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s pass --all to include all components\n",
		ui.Dim(color, "hint:"))
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

func sameStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
