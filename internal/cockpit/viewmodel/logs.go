package viewmodel

import (
	"sort"
	"strings"
)

// LogEntry is one job/step log block surfaced by `orun logs`.
type LogEntry struct {
	JobID       string
	Component   string
	Environment string
	Short       string
	StepID      string
	Status      string
	Lines       []string // pre-split, non-empty
	TotalLines  int      // full count even if Lines is truncated
}

// LogsView is the cockpit view of an execution's logs. Driven by the
// CLI's `orun logs` and (Phase 3) the TUI's log pane.
type LogsView struct {
	ExecID  string
	Run     RunView // re-use the run header for context
	Entries []LogEntry
}

// SortLogEntries orders entries by component, env, job id so grouped
// rendering stays deterministic regardless of filesystem walk order.
func SortLogEntries(entries []LogEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Component != entries[j].Component {
			return entries[i].Component < entries[j].Component
		}
		if entries[i].Environment != entries[j].Environment {
			return entries[i].Environment < entries[j].Environment
		}
		if entries[i].JobID != entries[j].JobID {
			return entries[i].JobID < entries[j].JobID
		}
		return entries[i].StepID < entries[j].StepID
	})
}

// SplitLines turns a raw log payload into trimmed non-empty lines.
// Provided here so callers don't reimplement the same compaction logic.
func SplitLines(content string) []string {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	parts := strings.Split(strings.TrimSpace(content), "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimRight(p, "\r")
		if strings.TrimSpace(p) == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}
