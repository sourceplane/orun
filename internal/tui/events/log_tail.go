package events

import (
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui/services"
)

// logStreamSeq issues process-unique stream ids for log tails. Every Attach
// gets a fresh id and every message a pump emits carries it, so a consumer
// can discard messages from a superseded tail (the stale pump of a previous
// attach, or the close-sentinel of a cancelled stream) instead of letting
// them contaminate the current view.
var logStreamSeq atomic.Int64

// NextLogStream returns a fresh log-stream id.
func NextLogStream() int64 { return logStreamSeq.Add(1) }

// maxLogBatch caps how many events one WaitForLogBatch drains. Bounding the
// batch keeps a single Update cheap while still coalescing bursts (a package
// install can emit hundreds of lines between frames) into one render instead
// of one render per line.
const maxLogBatch = 256

// WaitForLogBatch returns a Cmd that blocks until at least one LogEvent
// arrives on ch, then drains whatever else is immediately available (up to
// maxLogBatch) into a single LogBatchMsg. Closed reports end-of-stream; a
// batch can carry both trailing events and Closed when the channel closes
// mid-drain. The consumer re-arms by calling WaitForLogBatch again with the
// same stream id.
func WaitForLogBatch(ch <-chan services.LogEvent, stream int64) tea.Cmd {
	return func() tea.Msg {
		first, ok := <-ch
		if !ok {
			return services.LogBatchMsg{Stream: stream, Closed: true}
		}
		batch := services.LogBatchMsg{Stream: stream, Events: []services.LogEvent{first}}
		for len(batch.Events) < maxLogBatch {
			select {
			case ev, ok := <-ch:
				if !ok {
					batch.Closed = true
					return batch
				}
				batch.Events = append(batch.Events, ev)
			default:
				return batch
			}
		}
		return batch
	}
}
