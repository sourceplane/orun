package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/sourceplane/orun/internal/scaffold"
	"github.com/sourceplane/orun/internal/ui"
)

// planCounts is the shape of the plan generatePlan last compiled, for callers
// that run it as a check and report the result in their own words.
type planCounts struct {
	components, envs, jobs int
	id                     string
}

var lastPlanCounts planCounts

// printScaffoldDetail is the full placement report: every phase, batch, and
// file. `orun new --progress verbose` (and json) print it.
func printScaffoldDetail(res *scaffold.Result) {
	fmt.Printf("✓ scaffolded %d file(s) into %s\n", len(res.Files), scaffoldOut)
	phased := len(res.Phases) > 1 || (len(res.Phases) == 1 && res.Phases[0].Name != "")
	for _, phase := range res.Phases {
		if phased {
			label := phase.Name
			if phase.Hooks.Len() > 0 {
				all := phase.Hooks.All()
				hookIDs := make([]string, len(all))
				for i, h := range all {
					hookIDs[i] = h.ID
				}
				label = fmt.Sprintf("%s (hooks: %s)", phase.Name, strings.Join(hookIDs, ", "))
			}
			fmt.Printf("  ▸ phase: %s\n", label)
		}
		for _, batch := range phase.Batches {
			indent := "  "
			if phased {
				indent = "    "
			}
			fmt.Printf("%s· batch: %s\n", indent, strings.Join(batch, ", "))
		}
	}
	for _, f := range res.Files {
		fmt.Printf("    %s\n", f)
	}
	if len(res.Consumed) > 0 {
		fmt.Printf("  consumed (pinned deps, no bytes):\n")
		for _, c := range res.Consumed {
			fmt.Printf("    %s (source %s, from %s)\n", c.Module, c.Source, c.From)
		}
	}
	fmt.Printf("  provenance: %s (inputs %s)\n", filepath.Join(scaffoldOut, scaffold.ProvenanceRelPath), res.Provenance.InputsHash)
}

// printScaffoldSummary is the default `orun new` report.
//
//	▲ orun new  acme-saas → acme-shop
//
//	  ✓ Placed      14 files · standards → product
//	  ✓ Verified    intent valid · 2 components × 2 envs → 4 jobs
//	  ✓ Provenance  acme-shop/.orun/provenance.lock
//
//	  → cd acme-shop && orun plan
func printScaffoldSummary(blueprint string, res *scaffold.Result, gated bool, gateErr error) {
	color := ui.ColorEnabledForWriter(os.Stdout)
	ok := ui.Green(color, "✓")
	label := func(s string) string { return ui.Bold(color, fmt.Sprintf("%-11s", s)) }
	sep := ui.Dim(color, " · ")

	name := blueprint
	if name == "" {
		name = "blueprint"
	}
	fmt.Printf("\n%s %s  %s %s %s\n\n", ui.Style(color, "▲", "38;5;141"), ui.Bold(color, "orun new"),
		name, ui.Dim(color, "→"), ui.Bold(color, scaffoldOut))

	placed := fmt.Sprintf("%d files", len(res.Files))
	var phases []string
	for _, p := range res.Phases {
		if p.Name != "" {
			phases = append(phases, p.Name)
		}
	}
	if len(phases) > 1 {
		placed += sep + strings.Join(phases, ui.Dim(color, " → "))
	}
	if len(res.Consumed) > 0 {
		placed += sep + fmt.Sprintf("%d pinned dependencies", len(res.Consumed))
	}
	fmt.Printf("  %s %s %s\n", ok, label("Placed"), placed)
	if len(res.HooksRun) > 0 {
		fmt.Printf("  %s %s %s\n", ok, label("Hooks"), strings.Join(res.HooksRun, ", "))
	}

	if gated {
		if gateErr != nil {
			fmt.Printf("  %s %s %s\n", ui.Red(color, "✕"), label("Verified"), "the scaffolded intent does not plan:")
		} else {
			c := lastPlanCounts
			fmt.Printf("  %s %s intent valid%s%d components %s %d envs %s %s\n", ok, label("Verified"), sep,
				c.components, ui.Dim(color, "×"), c.envs, ui.Dim(color, "→"), ui.Bold(color, fmt.Sprintf("%d jobs", c.jobs)))
		}
	}
	fmt.Printf("  %s %s %s\n", ok, label("Provenance"), ui.Dim(color, filepath.Join(scaffoldOut, scaffold.ProvenanceRelPath)))

	if gated && gateErr == nil {
		fmt.Printf("\n  %s cd %s && orun plan\n\n", ui.Dim(color, "→"), scaffoldOut)
	} else {
		fmt.Println()
	}
}

// captureOutput runs fn with stdout and stderr redirected, returning what it
// printed. It lets a command run another command's pipeline as a check and
// show that pipeline's output only when the check fails.
func captureOutput(fn func() error) (string, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return "", fn()
	}
	savedOut, savedErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w
	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	runErr := fn()
	os.Stdout, os.Stderr = savedOut, savedErr
	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out, runErr
}
