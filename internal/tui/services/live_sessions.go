package services

import (
	"path/filepath"
	"time"

	"github.com/sourceplane/orun/internal/agent/live"
)

// liveDir is the session registry directory, matching the CLI body host
// (cmd/orun agentLiveDir): <.orun>/agents/live.
func (s *LiveOrunService) liveDir() string {
	return filepath.Join(s.cfg.ObjectModelRoot, "agents", "live")
}

// LiveSessionRow is the cockpit projection of one live session body from the
// registry (orun-agents-live AL3): the data the Agent surface's sessions
// sidebar renders. Read from .orun/agents/live, pid-swept — a dead body never
// shows.
type LiveSessionRow struct {
	SessionID string
	State     string
	AgentType string
	Task      string
	Driver    string
	Socket    string
	StartedAt time.Time
}

// LiveSessions lists the live session bodies on this machine, newest first.
// Best-effort: an absent registry returns nil.
func (s *LiveOrunService) LiveSessions() ([]LiveSessionRow, error) {
	entries, err := live.List(s.liveDir())
	if err != nil {
		return nil, err
	}
	rows := make([]LiveSessionRow, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, LiveSessionRow{
			SessionID: e.SessionID, State: e.State, AgentType: e.AgentType,
			Task: e.Task, Driver: e.Driver, Socket: e.Socket, StartedAt: e.StartedAt,
		})
	}
	return rows, nil
}
