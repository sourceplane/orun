package services

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// followPollInterval is how often follow-mode rescans a job's log directory
// for newly written step logs. The runner writes each step's log file once,
// atomically, when the step completes (see runner.writeStepLog), so polling
// for new/changed files — rather than byte-tailing a single file — matches
// how logs actually land on disk.
const followPollInterval = 250 * time.Millisecond

// TailLogs streams log lines for a job from local on-disk state.
//
// Two modes:
//
//   - Follow == false: read every existing step log for the job once, in
//     deterministic order, then close. Used for completed/historical runs.
//   - Follow == true: emit existing step logs, then keep watching the job's
//     log directory for newly written step logs until the context is
//     cancelled. Used for the in-flight run so logs appear in the run and
//     activity views while the run is executing. The caller owns the
//     lifetime via ctx — cancelling it drains and closes the channel.
//
// Remote-state log retrieval remains gated behind its own phase.
func (s *LiveOrunService) TailLogs(ctx context.Context, req LogRequest) (<-chan LogEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req.RemoteState {
		return nil, errors.New("TailLogs: remote-state log retrieval not yet implemented")
	}
	if s.cfg.Store == nil {
		return nil, errors.New("TailLogs: no state store configured")
	}
	if req.ExecID == "" {
		return nil, errors.New("TailLogs: ExecID is required")
	}
	if req.JobID == "" {
		return nil, errors.New("TailLogs: JobID is required")
	}

	// Resolve the execID. For an in-flight follow the execution directory
	// may not exist yet (the first step has not finished writing), so fall
	// back to the caller-supplied ID and let the follow loop pick up files
	// as they appear rather than failing closed.
	resolvedExecID, err := s.cfg.Store.ResolveExecID(req.ExecID)
	if err != nil {
		if !req.Follow {
			return nil, err
		}
		resolvedExecID = req.ExecID
	}

	logDir := s.cfg.Store.LogDir(resolvedExecID, req.JobID)

	// stepPaths resolves the set of step log files to read. With a StepID it
	// targets exactly one file; otherwise it lists every *.log in the job's
	// log directory in sorted (deterministic) order.
	stepPaths := func() []string {
		if req.StepID != "" {
			return []string{s.cfg.Store.LogPath(resolvedExecID, req.JobID, req.StepID)}
		}
		entries, derr := os.ReadDir(logDir)
		if derr != nil {
			return nil
		}
		var paths []string
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
				continue
			}
			paths = append(paths, filepath.Join(logDir, e.Name()))
		}
		sort.Strings(paths)
		return paths
	}

	ch := make(chan LogEvent, 64)

	if !req.Follow {
		go func() {
			defer close(ch)
			for _, path := range stepPaths() {
				if ctx.Err() != nil {
					return
				}
				streamLogFile(ctx, ch, path, req.JobID, deriveStepID(path))
			}
		}()
		return ch, nil
	}

	go func() {
		defer close(ch)
		// emitted tracks step log files already streamed so each is sent
		// exactly once even though we rescan the directory every tick.
		emitted := map[string]bool{}
		drain := func() {
			for _, path := range stepPaths() {
				if emitted[path] {
					continue
				}
				if ctx.Err() != nil {
					return
				}
				if streamLogFile(ctx, ch, path, req.JobID, deriveStepID(path)) {
					emitted[path] = true
				}
			}
		}

		drain()
		ticker := time.NewTicker(followPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				drain()
			}
		}
	}()
	return ch, nil
}

// streamLogFile reads a single step log file and emits one LogEvent per
// line. It returns true when the file existed and was read (so follow mode
// can mark it consumed) and false when the file is not yet present, in
// which case the caller should retry on a later poll.
func streamLogFile(ctx context.Context, ch chan<- LogEvent, path, jobID, stepID string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	now := time.Now()
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return true
		case ch <- LogEvent{
			JobID:     jobID,
			StepID:    stepID,
			Line:      scanner.Text(),
			Timestamp: now,
		}:
		}
	}
	return true
}

func deriveStepID(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, ".log")
}
