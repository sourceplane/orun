// Package viewmodel turns raw .orun state into surface-agnostic structs that
// the cockpit renderers (CLI + TUI) consume.
//
// Nothing here knows about ANSI, lipgloss, or fsnotify. Pure value objects,
// trivially testable.
package viewmodel

import (
	"sort"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/execmodel"
)

// Counts mirrors execmodel.ExecutionCounts but lives here so renderers don't
// need to import the persistence layer.
type Counts struct {
	Total     int
	Completed int
	Failed    int
	Running   int
	Pending   int
}

// Percent returns the completion percentage (0-100) including failures
// as terminal states. Returns 0 when Total is 0.
func (c Counts) Percent() int {
	if c.Total <= 0 {
		return 0
	}
	return (c.Completed + c.Failed) * 100 / c.Total
}

// Step is one entry in a job's step ledger.
type Step struct {
	ID     string
	Status string
}

// Job is the cockpit's view of a single job in an execution.
type Job struct {
	ID          string
	Component   string
	Environment string
	Short       string // job name with component.env. prefix stripped
	Status      string
	Error       string
	StartedAt   time.Time
	FinishedAt  time.Time
	Steps       []Step
}

// Duration returns the job's wall-clock duration; falls back to "since
// started" while running, and to zero when not started.
func (j Job) Duration(now time.Time) time.Duration {
	if j.StartedAt.IsZero() {
		return 0
	}
	end := j.FinishedAt
	if end.IsZero() {
		end = now
	}
	return end.Sub(j.StartedAt)
}

// ComponentGroup buckets jobs by component for grouped rendering.
type ComponentGroup struct {
	Component string
	Jobs      []Job
}

// RunView is the cockpit's view of a single execution. Both
// `orun status` and the TUI's Inspector/Run panes consume this.
type RunView struct {
	ExecID     string
	PlanID     string
	PlanName   string
	Status     string
	Trigger    string
	DryRun     bool
	StartedAt  time.Time
	FinishedAt time.Time
	Counts     Counts
	Jobs       []Job
	Groups     []ComponentGroup
	Components []string
	Links      []Link
	MultiEnv   bool
}

// Link is an external URL surfaced under the run header (GHA run page,
// dashboard URL, terraform plan output, etc.).
type Link struct {
	Label string
	URL   string
}

// RunListView is the cockpit's view of N executions (orun get runs).
type RunListView struct {
	Runs []RunSummary
}

// RunSummary is one row in a run listing.
type RunSummary struct {
	ExecID     string
	PlanName   string
	Status     string
	Counts     Counts
	StartedAt  time.Time
	FinishedAt time.Time
}

// Duration returns wall-clock duration for a finished run, or 0 while
// in-flight.
func (r RunSummary) Duration() time.Duration {
	if r.StartedAt.IsZero() || r.FinishedAt.IsZero() {
		return 0
	}
	return r.FinishedAt.Sub(r.StartedAt)
}

// BuildRunView assembles a RunView from a execmodel.ExecMetadata + ExecState
// pair. Either argument may be nil; the resulting view is best-effort.
func BuildRunView(execID string, meta *execmodel.ExecMetadata, st *execmodel.ExecState) RunView {
	v := RunView{ExecID: execID}
	if meta != nil {
		v.PlanID = strings.TrimSpace(meta.PlanID)
		v.PlanName = strings.TrimSpace(meta.PlanName)
		v.Status = meta.Status
		v.Trigger = meta.Trigger
		v.DryRun = meta.DryRun
		v.StartedAt = parseTime(meta.StartedAt)
		v.FinishedAt = parseTime(meta.FinishedAt)
		for _, l := range meta.Links {
			if strings.TrimSpace(l.URL) == "" {
				continue
			}
			v.Links = append(v.Links, Link{Label: l.Label, URL: l.URL})
		}
	}
	if v.PlanName == "" {
		v.PlanName = "plan"
	}

	jobs := collectJobs(st)
	v.Jobs = jobs
	v.Groups = groupByComponent(jobs)
	v.Counts = summarize(jobs, meta)
	v.MultiEnv = detectMultiEnv(jobs)

	seen := map[string]struct{}{}
	for _, j := range jobs {
		if j.Component == "" {
			continue
		}
		if _, ok := seen[j.Component]; ok {
			continue
		}
		seen[j.Component] = struct{}{}
		v.Components = append(v.Components, j.Component)
	}
	sort.Strings(v.Components)

	return v
}

// BuildRunListView builds a list view from state.Store entries.
func BuildRunListView(entries []execmodel.ExecEntry) RunListView {
	out := RunListView{Runs: make([]RunSummary, 0, len(entries))}
	for _, e := range entries {
		out.Runs = append(out.Runs, RunSummary{
			ExecID:   e.ID,
			PlanName: e.PlanName,
			Status:   e.Status,
			Counts: Counts{
				Total:     e.JobTotal,
				Completed: e.JobDone,
				Failed:    e.JobFailed,
			},
			StartedAt:  parseTime(e.StartedAt),
			FinishedAt: parseTime(e.FinishedAt),
		})
	}
	sort.SliceStable(out.Runs, func(i, j int) bool {
		ir := strings.EqualFold(out.Runs[i].Status, "running")
		jr := strings.EqualFold(out.Runs[j].Status, "running")
		if ir != jr {
			return ir
		}
		return out.Runs[i].StartedAt.After(out.Runs[j].StartedAt)
	})
	return out
}

// --- internals --------------------------------------------------------

func collectJobs(st *execmodel.ExecState) []Job {
	if st == nil {
		return nil
	}
	out := make([]Job, 0, len(st.Jobs))
	for id, js := range st.Jobs {
		if js == nil {
			continue
		}
		comp, env, short := splitJobID(id)
		j := Job{
			ID:          id,
			Component:   comp,
			Environment: env,
			Short:       short,
			Status:      js.Status,
			Error:       js.LastError,
			StartedAt:   parseTime(js.StartedAt),
			FinishedAt:  parseTime(js.FinishedAt),
		}
		for sid, sst := range js.Steps {
			j.Steps = append(j.Steps, Step{ID: sid, Status: sst})
		}
		sort.SliceStable(j.Steps, func(a, b int) bool {
			return j.Steps[a].ID < j.Steps[b].ID
		})
		out = append(out, j)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Component != out[j].Component {
			return out[i].Component < out[j].Component
		}
		if out[i].Environment != out[j].Environment {
			return out[i].Environment < out[j].Environment
		}
		oi, oj := statusOrder(out[i].Status), statusOrder(out[j].Status)
		if oi != oj {
			return oi < oj
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func groupByComponent(jobs []Job) []ComponentGroup {
	if len(jobs) == 0 {
		return nil
	}
	var groups []ComponentGroup
	idx := map[string]int{}
	for _, j := range jobs {
		key := j.Component
		if key == "" {
			key = j.Short
		}
		if i, ok := idx[key]; ok {
			groups[i].Jobs = append(groups[i].Jobs, j)
			continue
		}
		idx[key] = len(groups)
		groups = append(groups, ComponentGroup{Component: key, Jobs: []Job{j}})
	}
	return groups
}

func summarize(jobs []Job, meta *execmodel.ExecMetadata) Counts {
	c := Counts{}
	for _, j := range jobs {
		c.Total++
		switch normalize(j.Status) {
		case "completed", "success":
			c.Completed++
		case "failed", "error":
			c.Failed++
		case "running", "in_progress":
			c.Running++
		default:
			c.Pending++
		}
	}
	// If the state file is empty but metadata has totals (early-stage run),
	// trust the metadata.
	if c.Total == 0 && meta != nil && meta.JobTotal > 0 {
		c.Total = meta.JobTotal
		c.Completed = meta.JobDone
		c.Failed = meta.JobFailed
		c.Pending = meta.JobTotal - meta.JobDone - meta.JobFailed
		if c.Pending < 0 {
			c.Pending = 0
		}
	}
	return c
}

func detectMultiEnv(jobs []Job) bool {
	seen := ""
	for _, j := range jobs {
		if j.Environment == "" {
			continue
		}
		if seen == "" {
			seen = j.Environment
			continue
		}
		if j.Environment != seen {
			return true
		}
	}
	return false
}

func statusOrder(s string) int {
	switch normalize(s) {
	case "failed", "error":
		return 0
	case "running", "in_progress":
		return 1
	case "pending", "queued":
		return 2
	case "completed", "success":
		return 3
	}
	return 4
}

func splitJobID(id string) (component, env, short string) {
	parts := strings.SplitN(id, ".", 3)
	switch len(parts) {
	case 3:
		return parts[0], parts[1], parts[2]
	case 2:
		return parts[0], "", parts[1]
	default:
		return "", "", id
	}
}

func parseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05Z"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
