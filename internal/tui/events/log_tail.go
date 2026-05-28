package events

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui/services"
)

// WaitForLogEvent returns a Cmd that blocks until the next LogEvent
// arrives on ch. When ch is closed it returns a sentinel LogEventMsg with
// an empty Line so the receiving model can detect end-of-stream and stop
// re-arming the wait command.
func WaitForLogEvent(ch <-chan services.LogEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return services.LogEventMsg{Event: services.LogEvent{Line: ""}}
		}
		return services.LogEventMsg{Event: event}
	}
}
