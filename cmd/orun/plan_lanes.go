package main

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/ui"
)

// maxLaneJobs bounds the lane view printed under `orun plan` on a terminal.
// Past it the summary stays compact and `--view dag` is the way in.
const maxLaneJobs = 24

// renderPlanLanes renders a compact, environment-by-environment view of a
// plan: each job's component, profile and steps, and what it waits on.
// Environments appear in plan order (which is topological), so a promotion
// chain reads top to bottom.
//
//	staging
//	├─ api   verify    test → build
//	└─ web   verify    test → build            after api
//	production                                 after staging
//	├─ api   release   test → build → package
//	└─ web   release   test → build → package  after api
func renderPlanLanes(w io.Writer, plan *model.Plan, color bool) {
	if plan == nil || len(plan.Jobs) == 0 {
		return
	}
	type lane struct {
		env   string
		jobs  []model.PlanJob
		after map[string]bool // environments this lane waits on
	}
	var lanes []*lane
	byEnv := map[string]*lane{}
	envOf := map[string]string{}
	compOf := map[string]string{}
	for _, j := range plan.Jobs {
		envOf[j.ID] = j.Environment
		compOf[j.ID] = j.Component
	}
	for _, j := range plan.Jobs {
		l, ok := byEnv[j.Environment]
		if !ok {
			l = &lane{env: j.Environment, after: map[string]bool{}}
			byEnv[j.Environment] = l
			lanes = append(lanes, l)
		}
		l.jobs = append(l.jobs, j)
		for _, dep := range j.DependsOn {
			if e := envOf[dep]; e != "" && e != j.Environment {
				l.after[e] = true
			}
		}
	}

	compW, profW := 0, 0
	for _, j := range plan.Jobs {
		compW = max(compW, utf8.RuneCountInString(j.Component))
		profW = max(profW, utf8.RuneCountInString(shortProfile(j.Profile)))
	}
	stepsOf := func(j model.PlanJob) string {
		names := make([]string, 0, len(j.Steps))
		for _, s := range j.Steps {
			n := s.Name
			if n == "" {
				n = s.ID
			}
			names = append(names, n)
		}
		const keep = 5
		if len(names) > keep {
			names = append(names[:keep], fmt.Sprintf("+%d", len(names)-keep))
		}
		return strings.Join(names, " → ")
	}
	stepsW := 0
	for _, j := range plan.Jobs {
		stepsW = max(stepsW, utf8.RuneCountInString(stepsOf(j)))
	}
	pad := func(s string, n int) string {
		if gap := n - utf8.RuneCountInString(s); gap > 0 {
			return s + strings.Repeat(" ", gap)
		}
		return s
	}
	// The env header's "after" lines up with the jobs' "after" column.
	rowW := 3 + compW + 2 + profW + 2 + stepsW

	for _, l := range lanes {
		line := "  " + ui.Bold(color, l.env)
		if after := sortedKeysOf(l.after); len(after) > 0 {
			line = "  " + ui.Bold(color, l.env) + strings.Repeat(" ", rowW-utf8.RuneCountInString(l.env)) +
				"  " + ui.Dim(color, "after "+strings.Join(after, ", "))
		}
		fmt.Fprintln(w, strings.TrimRight(line, " "))
		for i, j := range l.jobs {
			branch := "├─ "
			if i == len(l.jobs)-1 {
				branch = "└─ "
			}
			var same []string
			for _, dep := range j.DependsOn {
				if envOf[dep] == j.Environment && compOf[dep] != "" {
					same = append(same, compOf[dep])
				}
			}
			steps := stepsOf(j)
			if len(same) > 0 {
				steps = pad(steps, stepsW)
			}
			row := "  " + ui.Dim(color, branch) + pad(j.Component, compW) + "  " +
				ui.Cyan(color, pad(shortProfile(j.Profile), profW)) + "  " +
				ui.Dim(color, steps)
			if len(same) > 0 {
				row += "  " + ui.Dim(color, "after "+strings.Join(same, ", "))
			}
			fmt.Fprintln(w, strings.TrimRight(row, " "))
		}
	}
}

// shortProfile drops the composition prefix from a resolved profile name
// ("node-service.release" → "release").
func shortProfile(p string) string {
	if i := strings.LastIndex(p, "."); i >= 0 {
		return p[i+1:]
	}
	return p
}

func sortedKeysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
