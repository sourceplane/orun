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

// TailLogs streams log lines for a job from local on-disk state.
//
// Phase 1 boundary: this implementation reads the existing log files for
// the requested execID + jobID once and closes the channel. Live "follow"
// tailing and the remote-state code path are deferred — when Follow=true
// or RemoteState=true is requested, a clear error is returned rather than
// silently degrading to a non-follow read, so callers can surface the
// limitation explicitly.
func (s *LiveOrunService) TailLogs(ctx context.Context, req LogRequest) (<-chan LogEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req.RemoteState {
		return nil, errors.New("TailLogs: remote-state log retrieval not yet implemented (Phase 3)")
	}
	if req.Follow {
		return nil, errors.New("TailLogs: follow-mode tailing not yet implemented (Phase 3)")
	}
	if s.cfg.Store == nil {
		return nil, errors.New("TailLogs: no state store configured")
	}
	if req.ExecID == "" {
		return nil, errors.New("TailLogs: ExecID is required")
	}

	resolvedExecID, err := s.cfg.Store.ResolveExecID(req.ExecID)
	if err != nil {
		return nil, err
	}

	jobID := req.JobID
	if jobID == "" {
		return nil, errors.New("TailLogs: JobID is required (job-wide log aggregation arrives in Phase 3)")
	}

	logDir := s.cfg.Store.LogDir(resolvedExecID, jobID)

	// Resolve step files: if StepID is set, just that one; otherwise all
	// *.log files in sorted order so output is deterministic.
	var logPaths []string
	if req.StepID != "" {
		logPaths = []string{s.cfg.Store.LogPath(resolvedExecID, jobID, req.StepID)}
	} else {
		entries, derr := os.ReadDir(logDir)
		if derr != nil {
			if os.IsNotExist(derr) {
				ch := make(chan LogEvent)
				close(ch)
				return ch, nil
			}
			return nil, derr
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
				continue
			}
			logPaths = append(logPaths, filepath.Join(logDir, e.Name()))
		}
		sort.Strings(logPaths)
	}

	ch := make(chan LogEvent, 64)
	go func() {
		defer close(ch)
		for _, path := range logPaths {
			if err := ctx.Err(); err != nil {
				return
			}
			streamLogFile(ctx, ch, path, jobID, deriveStepID(path))
		}
	}()
	return ch, nil
}

func streamLogFile(ctx context.Context, ch chan<- LogEvent, path, jobID, stepID string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	now := time.Now()
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		case ch <- LogEvent{
			JobID:     jobID,
			StepID:    stepID,
			Line:      scanner.Text(),
			Timestamp: now,
		}:
		}
	}
}

func deriveStepID(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, ".log")
}
