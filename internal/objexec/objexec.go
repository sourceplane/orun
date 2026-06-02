// Package objexec is the transitional bridge that turns a completed run's legacy
// execution state (internal/state) into a native object-model execution and
// seals it via internal/execseal. It exists so `orun run` can record executions
// in the content-addressed graph without rewriting the runner's hot path; it is
// deliberately the only object-model-adjacent package that imports the legacy
// state types, and it is removed at the M12 cutover once the runner writes the
// working tree natively. For that reason it is intentionally excluded from the
// object-model lint gate's internal/state ban.
package objexec

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/execseal"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/state"
)

// FromLegacyState builds an execseal.SealInput from a finished run's legacy
// ExecState + metadata, attributing it to the given object-model revision (and
// optional trigger). The legacy model has no attempt dimension, so a single
// attempt (1) is synthesized per job. Job/step ordering is made deterministic by
// sorting keys, and the execution status is forced terminal (derived from the
// job tally when the metadata status is non-terminal).
func FromLegacyState(revisionID objectstore.ObjectID, triggerID, execID, execKey string, st *state.ExecState, meta *state.ExecMetadata) execseal.SealInput {
	in := execseal.SealInput{
		RevisionID:   revisionID,
		TriggerID:    triggerID,
		ExecutionID:  firstNonEmpty(execID, metaExecID(meta), stateExecID(st)),
		ExecutionKey: execKey,
		Status:       terminalStatus(st, meta),
	}
	if meta != nil {
		in.StartedAt = parseTime(meta.StartedAt)
		in.FinishedAt = parseTime(meta.FinishedAt)
		in.DryRun = meta.DryRun
		in.Links = mapLinks(meta.Links)
	}
	in.Jobs = mapJobs(st)
	return in
}

func mapJobs(st *state.ExecState) []nodes.JobInput {
	if st == nil || len(st.Jobs) == 0 {
		return nil
	}
	jobIDs := make([]string, 0, len(st.Jobs))
	for id := range st.Jobs {
		jobIDs = append(jobIDs, id)
	}
	sort.Strings(jobIDs)

	jobs := make([]nodes.JobInput, 0, len(jobIDs))
	for _, jobID := range jobIDs {
		js := st.Jobs[jobID]
		status := mapStatus(js.Status)
		jr := nodes.JobRun{
			Kind:       nodes.KindJobRun,
			JobID:      jobID,
			Folder:     jobFolder(jobID),
			Status:     status,
			StartedAt:  parseTimePtr(js.StartedAt),
			FinishedAt: parseTimePtr(js.FinishedAt),
			LastError:  js.LastError,
		}
		attempt := nodes.JobAttempt{Kind: nodes.KindJobAttempt, Attempt: 1, Status: status}
		steps := make([]nodes.StepInput, 0, len(js.Steps))
		stepIDs := make([]string, 0, len(js.Steps))
		for sid := range js.Steps {
			stepIDs = append(stepIDs, sid)
		}
		sort.Strings(stepIDs)
		for _, sid := range stepIDs {
			steps = append(steps, nodes.StepInput{Record: nodes.StepAttempt{
				Kind:   nodes.KindStepAttempt,
				StepID: sid,
				Status: mapStatus(js.Steps[sid]),
			}})
		}
		jobs = append(jobs, nodes.JobInput{
			Record:   jr,
			Attempts: []nodes.AttemptInput{{Record: attempt, Steps: steps}},
		})
	}
	return jobs
}

func mapLinks(links []state.ExecutionLink) []nodes.ExecLink {
	if len(links) == 0 {
		return nil
	}
	out := make([]nodes.ExecLink, 0, len(links))
	for _, l := range links {
		out = append(out, nodes.ExecLink{Label: l.Label, URL: l.URL, JobID: l.JobID, StepID: l.StepID})
	}
	return out
}

// jobFolder derives the sanitized j-<shortHash> folder name for a job id. The
// original (possibly @/./ -bearing) job id is preserved in the JobRun record.
func jobFolder(jobID string) string {
	sum := sha256.Sum256([]byte(jobID))
	return "j-" + hex.EncodeToString(sum[:])[:8]
}

// mapStatus folds the runner's status vocabulary onto the node status set.
func mapStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "success", "succeeded", "completed", "complete", "ok", "passed":
		return nodes.StatusSucceeded
	case "failed", "failure", "error", "errored":
		return nodes.StatusFailed
	case "cancelled", "canceled", "skipped":
		return nodes.StatusCancelled
	case "running", "in_progress", "in-progress", "started":
		return nodes.StatusRunning
	default:
		return nodes.StatusPending
	}
}

// terminalStatus returns a terminal execution status, deriving one from the job
// tally when the metadata status is missing or non-terminal.
func terminalStatus(st *state.ExecState, meta *state.ExecMetadata) string {
	if meta != nil {
		if s := mapStatus(meta.Status); nodes.IsTerminalStatus(s) {
			return s
		}
		if meta.JobFailed > 0 {
			return nodes.StatusFailed
		}
	}
	if st != nil {
		for _, js := range st.Jobs {
			if mapStatus(js.Status) == nodes.StatusFailed {
				return nodes.StatusFailed
			}
		}
	}
	return nodes.StatusSucceeded
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func parseTimePtr(s string) *time.Time {
	t := parseTime(s)
	if t.IsZero() {
		return nil
	}
	return &t
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func metaExecID(m *state.ExecMetadata) string {
	if m == nil {
		return ""
	}
	return m.ExecID
}

func stateExecID(s *state.ExecState) string {
	if s == nil {
		return ""
	}
	return s.ExecID
}
