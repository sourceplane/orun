package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/sourceplane/gluon/internal/model"
	"github.com/sourceplane/gluon/internal/state"
	"github.com/sourceplane/gluon/internal/ui"
)

var (
	restoreFromCachePattern = regexp.MustCompile(`(?i)^Restoring ['"]?([^'"]+)['"]? from cache$`)
	toolCachedPattern       = regexp.MustCompile(`(?i)^([A-Za-z0-9._ -]+) tool version '([^']+)' has been cached at .+$`)
	toolDownloadPattern     = regexp.MustCompile(`(?i)^Downloading ['"]?([^'"]+)['"]? from .+$`)
	urlPattern              = regexp.MustCompile(`https?://[^\s]+`)
)

type stepOutputView struct {
	summary   []string
	result    []string
	logs      []string
	links     []state.ExecutionLink
	cacheHits int
	headline  string
}

type jobReport struct {
	dryRun    bool
	stepCount int
	headline  string
	links     []state.ExecutionLink
	cacheHits int
	linkIndex map[string]struct{}
}

func newJobReport(job model.PlanJob, dryRun bool) *jobReport {
	return &jobReport{
		dryRun:    dryRun,
		stepCount: len(job.Steps),
		linkIndex: map[string]struct{}{},
	}
}

func (jr *jobReport) observeStep(jobID, stepID string, view stepOutputView) {
	jr.cacheHits += view.cacheHits
	if view.headline != "" {
		jr.headline = view.headline
	}
	for _, link := range view.links {
		if strings.TrimSpace(link.URL) == "" {
			continue
		}
		link.JobID = jobID
		link.StepID = stepID
		key := link.URL + "|" + link.Label
		if _, exists := jr.linkIndex[key]; exists {
			continue
		}
		jr.linkIndex[key] = struct{}{}
		jr.links = append(jr.links, link)
	}
}

func (jr *jobReport) defaultHeadline() string {
	if jr.headline != "" {
		return jr.headline
	}
	if jr.dryRun {
		return fmt.Sprintf("Would run %d step%s", jr.stepCount, pluralSuffix(jr.stepCount))
	}
	return "Done"
}

func (r *Runner) shouldPrintPreflight(jobs []model.PlanJob) bool {
	if r.DryRun {
		return true
	}
	if r.JobID != "" {
		return false
	}
	return len(jobs) > 1
}

func (r *Runner) withPrintLock(fn func()) {
	r.printMu.Lock()
	defer r.printMu.Unlock()
	fn()
}

func (r *Runner) printStepStart(stepID string, index, total int) {
	if !r.Verbose {
		return
	}
	r.withPrintLock(func() {
		fmt.Fprintf(r.Stdout, "\n  %s %s (%d/%d)\n", ui.Cyan(r.Color, "●"), ui.Bold(r.Color, stepID), index, total)
	})
}

func (r *Runner) printStepContext(step model.PlanStep, workingDir, timeoutValue string, retryCount int) {
	if !r.Verbose {
		return
	}
	meta := []string{
		fmt.Sprintf("runner: %s", displayRunnerName(r.Executor.Name())),
		fmt.Sprintf("cwd: %s", shortenDisplayLine(workingDir)),
	}
	r.withPrintLock(func() {
		fmt.Fprintf(r.Stdout, "  │ %s\n", strings.Join(meta, "   "))

		if strings.TrimSpace(step.Use) != "" {
			fmt.Fprintf(r.Stdout, "  │ use: %s\n", strings.TrimSpace(step.Use))
		}
		if strings.TrimSpace(step.Run) != "" {
			fmt.Fprintln(r.Stdout, "  │ run:")
			for _, line := range formatCommandPreview(step.Run) {
				fmt.Fprintf(r.Stdout, "  │   %s\n", line)
			}
		}

		settings := make([]string, 0, 2)
		if retryCount > 0 {
			settings = append(settings, fmt.Sprintf("retries: %d", retryCount))
		}
		if timeoutValue != "" {
			settings = append(settings, fmt.Sprintf("timeout: %s", timeoutValue))
		}
		if len(settings) > 0 {
			fmt.Fprintf(r.Stdout, "  │ %s\n", strings.Join(settings, "   "))
		}
	})
}

func (r *Runner) printStepRetry(attempt, attempts int) {
	r.withPrintLock(func() {
		fmt.Fprintf(r.Stdout, "  %s retrying attempt %d/%d\n", ui.Yellow(r.Color, "↻"), attempt, attempts)
	})
}

func (r *Runner) printStepDryRun() {
	if !r.Verbose {
		return
	}
	r.withPrintLock(func() {
		fmt.Fprintf(r.Stdout, "  %s dry-run\n", ui.Cyan(r.Color, "◌"))
	})
}

func (r *Runner) printStepSkipped(stepID string, index, total int) {
	if !r.Verbose {
		return
	}
	r.withPrintLock(func() {
		fmt.Fprintf(r.Stdout, "  %s %s (%d/%d) already completed\n", ui.Yellow(r.Color, "↷"), stepID, index, total)
	})
}

func (r *Runner) printStepSuccess(step model.PlanStep, view stepOutputView, duration time.Duration) {
	if !r.Verbose {
		return
	}
	printed := false
	if len(view.summary) > 0 {
		r.printBlock("summary", view.summary)
		printed = true
	}
	if len(view.result) > 0 {
		r.printBlock("result", view.result)
		printed = true
	}
	if len(view.logs) > 0 {
		r.printBlock("logs", view.logs)
		printed = true
	}
	if printed {
		r.withPrintLock(func() {
			fmt.Fprintln(r.Stdout, "  │")
			fmt.Fprintf(r.Stdout, "  %s completed in %s\n", ui.Green(r.Color, "✓"), formatStepDuration(duration))
		})
	}
}

func (r *Runner) printStepFailure(job model.PlanJob, step model.PlanStep, view stepOutputView, duration time.Duration, err error, workingDir string) {
	if r.Verbose {
		printed := false
		if len(view.summary) > 0 {
			r.printBlock("summary", view.summary)
			printed = true
		}
		if len(view.result) > 0 {
			r.printBlock("result", view.result)
			printed = true
		}
		if len(view.logs) > 0 {
			r.printBlock("logs", view.logs)
			printed = true
		}
		r.withPrintLock(func() {
			if printed {
				fmt.Fprintln(r.Stdout, "  │")
			}
			fmt.Fprintf(r.Stdout, "  %s failed in %s: %s\n", ui.Red(r.Color, "✗"), formatStepDuration(duration), ui.Red(r.Color, summarizeExecError(err)))
			if hint := stepFailureHint(err, workingDir); hint != "" {
				fmt.Fprintf(r.Stdout, "  %s %s\n", ui.Yellow(r.Color, "hint:"), hint)
			}
		})
		return
	}

	headline := view.headline
	if headline == "" {
		headline = summarizeExecError(err)
	}
	label := jobDisplayName(job)
	r.withPrintLock(func() {
		fmt.Fprintf(r.Stdout, "%s %-22s %6s   %s\n", ui.Red(r.Color, "✗"), ui.Bold(r.Color, label), formatStepDuration(duration), ui.Red(r.Color, headline))
		fmt.Fprintf(r.Stdout, "  %s %s\n", ui.Dim(r.Color, "step"), stepIdentifier(step))
		fmt.Fprintf(r.Stdout, "  %s %s\n", ui.Dim(r.Color, "error"), ui.Red(r.Color, summarizeExecError(err)))
		if hint := stepFailureHint(err, workingDir); hint != "" {
			fmt.Fprintf(r.Stdout, "  %s %s\n", ui.Dim(r.Color, "hint"), hint)
		}
		if r.ExecID != "" {
			fmt.Fprintf(r.Stdout, "  %s gluon logs --exec-id %s --job %s\n", ui.Dim(r.Color, "details"), r.ExecID, job.ID)
		}
	})
}

func (r *Runner) printStepContinuation() {
	r.withPrintLock(func() {
		fmt.Fprintf(r.Stdout, "  %s continuing after failure\n", ui.Yellow(r.Color, "→"))
	})
}

func (r *Runner) printJobResumed(job model.PlanJob) {
	r.withPrintLock(func() {
		fmt.Fprintf(r.Stdout, "%s %-22s reused previous run\n", ui.Cyan(r.Color, "⚡"), ui.Bold(r.Color, jobDisplayName(job)))
	})
}

func (r *Runner) printJobFooter(job model.PlanJob, report *jobReport, success bool, duration time.Duration) {
	label := jobDisplayName(job)
	r.withPrintLock(func() {
		if success {
			fmt.Fprintf(r.Stdout, "%s %-22s %6s   %s\n", ui.Green(r.Color, "✓"), ui.Bold(r.Color, label), formatStepDuration(duration), report.defaultHeadline())
			for _, link := range report.links {
				fmt.Fprintf(r.Stdout, "  %s %-11s %s\n", ui.Cyan(r.Color, "↗"), linkLabel(link.Label), link.URL)
			}
			return
		}

		fmt.Fprintf(r.Stdout, "%s %-22s %6s   %s\n", ui.Red(r.Color, "✗"), ui.Bold(r.Color, label), formatStepDuration(duration), ui.Red(r.Color, "Job failed"))
		if report.defaultHeadline() != "Done" {
			fmt.Fprintf(r.Stdout, "  %s %s\n", ui.Dim(r.Color, "summary"), report.defaultHeadline())
		}
		if r.ExecID != "" {
			fmt.Fprintf(r.Stdout, "  %s gluon logs --exec-id %s --job %s\n", ui.Dim(r.Color, "details"), r.ExecID, job.ID)
		}
	})
}

func (r *Runner) printBlock(title string, lines []string) {
	if len(lines) == 0 {
		return
	}
	r.withPrintLock(func() {
		fmt.Fprintln(r.Stdout, "  │")
		fmt.Fprintf(r.Stdout, "  │ %s:\n", title)
		for _, line := range lines {
			fmt.Fprintf(r.Stdout, "  │   %s\n", line)
		}
	})
}

func (r *Runner) printInlineDetail(label, value string) {
	r.withPrintLock(func() {
		fmt.Fprintf(r.Stdout, "  %s %-11s %s\n", ui.Dim(r.Color, "·"), label, ui.Dim(r.Color, value))
	})
}

func (r *Runner) printRunSummary(summary *runSummary, finalStatus string) {
	if summary == nil {
		return
	}

	snap := summary.snapshot()
	stats := make([]string, 0, 4)
	switch {
	case r.DryRun:
		stats = append(stats, fmt.Sprintf("%d selected", snap.completed))
	default:
		if snap.completed > 0 {
			stats = append(stats, fmt.Sprintf("%d succeeded", snap.completed))
		}
		if snap.failed > 0 {
			stats = append(stats, fmt.Sprintf("%d failed", snap.failed))
		}
	}
	if snap.resumed > 0 {
		stats = append(stats, fmt.Sprintf("%d resumed", snap.resumed))
	}
	if snap.waiting > 0 {
		stats = append(stats, fmt.Sprintf("%d waiting", snap.waiting))
	}

	statusLine := fmt.Sprintf("Succeeded in %s", formatStepDuration(snap.duration))
	if r.DryRun {
		statusLine = fmt.Sprintf("Preview ready in %s", formatStepDuration(snap.duration))
	} else if strings.EqualFold(finalStatus, "failed") {
		statusLine = fmt.Sprintf("Failed in %s", formatStepDuration(snap.duration))
	}

	r.withPrintLock(func() {
		fmt.Fprintln(r.Stdout)
		fmt.Fprintln(r.Stdout, ui.Bold(r.Color, statusLine))
		if len(stats) > 0 {
			fmt.Fprintln(r.Stdout, strings.Join(stats, " · "))
		}
		if snap.cacheHits > 0 {
			fmt.Fprintf(r.Stdout, "%-12s %d local hit%s\n", ui.Dim(r.Color, "Cache"), snap.cacheHits, pluralSuffix(snap.cacheHits))
		}
		for _, link := range snap.links {
			fmt.Fprintf(r.Stdout, "%-12s %s\n", linkLabel(link.Label), link.URL)
		}
		if r.ExecID != "" && !r.DryRun {
			fmt.Fprintf(r.Stdout, "%-12s gluon status --exec-id %s\n", ui.Dim(r.Color, "Status"), r.ExecID)
			logsCommand := fmt.Sprintf("gluon logs --exec-id %s", r.ExecID)
			if strings.EqualFold(finalStatus, "failed") {
				logsCommand += " --failed"
			}
			fmt.Fprintf(r.Stdout, "%-12s %s\n", ui.Dim(r.Color, "Logs"), logsCommand)
		}
	})
}

func analyzeStepOutput(step model.PlanStep, output string) stepOutputView {
	lines := splitDisplayLines(output)
	view := stepOutputView{
		links: extractOutputLinks(lines),
	}
	if len(lines) == 0 {
		return view
	}

	cacheHit := false
	for _, line := range lines {
		if restoreFromCachePattern.MatchString(line) || toolCachedPattern.MatchString(line) {
			cacheHit = true
			break
		}
	}
	if cacheHit {
		view.cacheHits = 1
	}

	if strings.TrimSpace(step.Use) != "" {
		view.summary = summarizeUseOutput(lines)
		view.logs = lines
	} else {
		view.result = lines
	}
	view.headline = primaryOutputHeadline(view)
	return view
}

func summarizeUseOutput(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}

	installed := ""
	cached := ""
	downloaded := ""

	for _, line := range lines {
		if match := toolCachedPattern.FindStringSubmatch(line); match != nil {
			installed = fmt.Sprintf("Installed %s %s", strings.ToLower(strings.TrimSpace(match[1])), match[2])
			continue
		}
		if restoreFromCachePattern.MatchString(line) {
			cached = "Cached locally"
			continue
		}
		if match := toolDownloadPattern.FindStringSubmatch(line); match != nil {
			downloaded = fmt.Sprintf("Downloaded %s", match[1])
		}
	}

	summary := make([]string, 0, 3)
	for _, line := range []string{installed, cached, downloaded} {
		if strings.TrimSpace(line) != "" {
			summary = append(summary, line)
		}
	}
	if len(summary) > 0 {
		return summary
	}

	limit := len(lines)
	if limit > 3 {
		limit = 3
	}
	return append([]string{}, lines[:limit]...)
}

func splitDisplayLines(output string) []string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return nil
	}

	lines := make([]string, 0)
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, shortenDisplayLine(strings.TrimSpace(line)))
	}
	return lines
}

func extractOutputLinks(lines []string) []state.ExecutionLink {
	links := make([]state.ExecutionLink, 0)
	seen := map[string]struct{}{}
	for _, line := range lines {
		matches := urlPattern.FindAllStringIndex(line, -1)
		for _, match := range matches {
			url := line[match[0]:match[1]]
			if _, exists := seen[url]; exists {
				continue
			}
			seen[url] = struct{}{}
			links = append(links, state.ExecutionLink{
				Label: normalizeLinkLabel(line, url),
				URL:   url,
			})
		}
	}
	return links
}

func normalizeLinkLabel(line, url string) string {
	prefix := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(strings.Split(line, url)[0]), ":"))
	prefix = strings.Trim(prefix, "- ")
	if prefix == "" {
		return "Link"
	}
	if len(prefix) > 20 {
		return prefix[:17] + "..."
	}
	return prefix
}

func primaryOutputHeadline(view stepOutputView) string {
	for _, group := range [][]string{view.summary, view.result, view.logs} {
		if line := firstMeaningfulLine(group); line != "" {
			return line
		}
	}
	return ""
}

func firstMeaningfulLine(lines []string) string {
	fallback := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if fallback == "" {
			fallback = trimmed
		}
		if !urlPattern.MatchString(trimmed) {
			return trimmed
		}
	}
	return fallback
}

func formatCommandPreview(command string) []string {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return nil
	}

	lines := make([]string, 0)
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func formatStepDuration(duration time.Duration) string {
	rounded := duration.Round(100 * time.Millisecond)
	if rounded < time.Minute {
		return fmt.Sprintf("%.1fs", rounded.Seconds())
	}
	minutes := int(rounded / time.Minute)
	seconds := rounded % time.Minute
	if seconds%time.Second == 0 {
		return fmt.Sprintf("%dm%ds", minutes, int(seconds/time.Second))
	}
	return fmt.Sprintf("%dm%.1fs", minutes, seconds.Seconds())
}

func displayRunnerName(name string) string {
	switch strings.TrimSpace(name) {
	case "github-actions":
		return "gha"
	default:
		return strings.TrimSpace(name)
	}
}

func shortenDisplayLine(line string) string {
	homeDir, err := os.UserHomeDir()
	if err == nil && homeDir != "" {
		line = strings.ReplaceAll(line, homeDir, "~")
	}
	if prefix, suffix, ok := strings.Cut(line, " at "); ok && isLikelyPath(suffix) {
		return prefix + " at " + shortenPathDisplay(suffix)
	}
	if isLikelyPath(line) {
		return shortenPathDisplay(line)
	}
	return line
}

func shortenPathDisplay(value string) string {
	if value == "" {
		return value
	}

	cleaned := filepath.Clean(value)
	separator := string(filepath.Separator)
	parts := strings.Split(cleaned, separator)
	if len(cleaned) <= 64 && len(parts) <= 7 {
		return cleaned
	}

	prefixCount := 5
	if parts[0] == "" {
		prefixCount = 4
	}
	if len(parts) <= prefixCount+1 {
		return cleaned
	}

	prefix := parts[:prefixCount]
	suffix := parts[len(parts)-1:]
	if parts[0] == "" {
		return separator + strings.Join(append(prefix[1:], append([]string{"..."}, suffix...)...), separator)
	}
	return strings.Join(append(prefix, append([]string{"..."}, suffix...)...), separator)
}

func isLikelyPath(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.ContainsAny(trimmed, " \t") {
		return false
	}
	if strings.HasPrefix(trimmed, "~/") || strings.HasPrefix(trimmed, "./") || strings.HasPrefix(trimmed, "../") || strings.HasPrefix(trimmed, "/") {
		return true
	}
	return strings.Contains(trimmed, string(filepath.Separator))
}

func jobDisplayName(job model.PlanJob) string {
	if strings.TrimSpace(job.Component) != "" && strings.TrimSpace(job.Name) != "" {
		return fmt.Sprintf("%s:%s", job.Component, strings.TrimSpace(job.Name))
	}
	if strings.TrimSpace(job.Name) != "" {
		return strings.TrimSpace(job.Name)
	}
	return strings.TrimSpace(job.ID)
}

func linkLabel(label string) string {
	trimmed := strings.TrimSpace(label)
	if trimmed == "" {
		return "Link"
	}
	return trimmed
}

func pluralSuffix(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}
