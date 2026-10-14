package main

import (
	"fmt"
	"strings"

	"github.com/sourceplane/orun/internal/lanes"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/ui"
)

// laneSummary is the plan summary's `lanes` line: lanes per environment and
// the effective mode, e.g. `stage 3 · prod 3 · dev 2  (auto)`. Empty when
// the plan carries no lanes (specs/orun-lanes §10).
func laneSummary(plan *model.Plan, res lanes.Result) string {
	if plan == nil || len(plan.Lanes) == 0 {
		return ""
	}
	var envs []string
	count := map[string]int{}
	for _, l := range plan.Lanes {
		if _, ok := count[l.Environment]; !ok {
			envs = append(envs, l.Environment)
		}
		count[l.Environment]++
	}
	parts := make([]string, len(envs))
	for i, e := range envs {
		parts[i] = fmt.Sprintf("%s %d", e, count[e])
	}
	line := strings.Join(parts, " · ")
	if res.Mode != "" {
		line += "  (" + res.Mode
		if m := plan.Metadata.Lanes; m != nil && (m.MaxLanesFrom == "flag" || m.MaxLanesFrom == "env") {
			line += fmt.Sprintf(", maxLanes %d from %s", m.MaxLanes, m.MaxLanesFrom)
		}
		line += ")"
	}
	return line
}

// laneConcurrencyLabel renders a plan lane's concurrency: 0 is auto, sized
// to the runner's cores by `orun run --lane`.
func laneConcurrencyLabel(n int) string {
	if n == 0 {
		return "auto"
	}
	return fmt.Sprintf("%d", n)
}

// renderLanesView is `orun plan --view lanes`: every lane per environment with
// its source, concurrency and jobs, then the cycle repairs (§10).
func renderLanesView(plan *model.Plan, res lanes.Result, color bool) string {
	var b strings.Builder
	if plan == nil || len(plan.Lanes) == 0 {
		b.WriteString("No lanes: execution.lanes is not set, so every job is its own lane (orun run --job).\n")
		return b.String()
	}
	fmt.Fprintf(&b, "%s  %d lanes · mode %s\n", ui.Bold(color, "Lanes"), len(plan.Lanes), res.Mode)
	env := ""
	for _, l := range plan.Lanes {
		if l.Environment != env {
			env = l.Environment
			fmt.Fprintf(&b, "\n  %s\n", ui.Bold(color, env))
		}
		detail := fmt.Sprintf("%s · concurrency %s · level %d", l.Source, laneConcurrencyLabel(l.Concurrency), l.Level)
		if l.Group != "" {
			detail += " · group " + l.Group
		}
		if l.RunsOn != "" {
			detail += " · runs-on " + l.RunsOn
		}
		if l.SplitFrom != "" {
			detail += " · split from " + l.SplitFrom
		}
		if len(l.UpstreamLanes) > 0 {
			detail += " · after " + strings.Join(l.UpstreamLanes, ", ")
		}
		fmt.Fprintf(&b, "  ├─ %s  %s\n", ui.Bold(color, l.Name), ui.Dim(color, "["+l.ID+"]"))
		fmt.Fprintf(&b, "  │    %s\n", ui.Dim(color, detail))
		for _, j := range l.Jobs {
			fmt.Fprintf(&b, "  │    · %s\n", j)
		}
	}
	if len(res.Repairs) > 0 {
		fmt.Fprintf(&b, "\n  %s\n", ui.Bold(color, "Cycle repairs"))
		for _, r := range res.Repairs {
			fmt.Fprintf(&b, "  %s %s\n", ui.Yellow(color, "⚠"), r)
		}
	}
	return b.String()
}
