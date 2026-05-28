// Package events bridges streaming channels (RunEvent, LogEvent) into the
// Bubble Tea event loop using the canonical waitForMsg pattern.
package events

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui/services"
)

// WaitForRunEvent returns a Cmd that blocks until the next RunEvent
// arrives on ch, then wraps it in a RunEventMsg. When ch is closed the
// returned message carries a RunEventRunDone sentinel so the event loop
// has an unambiguous "end of stream" marker.
func WaitForRunEvent(ch <-chan services.RunEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return services.RunEventMsg{Event: services.RunEvent{Kind: services.RunEventRunDone}}
		}
		return services.RunEventMsg{Event: event}
	}
}
