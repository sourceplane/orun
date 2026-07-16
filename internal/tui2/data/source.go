// Package data is the cockpit v2 data plane (specs/orun-tui-v2 §8): one
// Source interface with local, cloud (TR8), and mock implementations.
//
// The model is snapshot + cursor: Snapshot reads return fresh view models,
// and Subscribe delivers payloadless change notifications per topic — a
// delta tells a surface that its slice is stale, and the surface
// re-snapshots. Streams that carry payloads (run events, attach frames,
// log batches) ride their own typed channels in TR3/TR4; the watch layer
// stays deliberately dumb so reconnect logic is trivial.
//
// Surfaces never see this package's implementations — the shell folds
// snapshots into store slices and deltas into re-snapshot commands, so
// Update never performs I/O (design §13.2).
package data

import (
	"context"

	"github.com/sourceplane/orun/internal/agent/live"
	"github.com/sourceplane/orun/internal/cockpit/viewmodel"
)

// Topic identifies a subscribable slice of the workspace.
type Topic string

const (
	// TopicCatalog fires when the catalog / object-model graph changes.
	TopicCatalog Topic = "catalog"
	// TopicRuns fires when run refs or live run state change.
	TopicRuns Topic = "runs"
	// TopicSessions fires when the live agent-session registry changes.
	TopicSessions Topic = "sessions"
	// TopicWork fires when sealed epic/spec snapshots change.
	TopicWork Topic = "work"
)

// AllTopics lists every topic, in stable order.
var AllTopics = []Topic{TopicCatalog, TopicRuns, TopicSessions, TopicWork}

// Delta is one change notification. Seq is monotonic per Source so
// consumers can detect gaps after reconnects; Degraded marks deltas that
// come from the fallback ticker rather than the watcher (the status line
// surfaces it).
type Delta struct {
	Topic    Topic
	Seq      uint64
	Degraded bool
}

// Caps declares what a Source can do; surfaces gate affordances on it
// rather than on the source's concrete type (design §14 keeps remote
// execution droppable-in later).
type Caps struct {
	// Execute: the source can dispatch plan runs.
	Execute bool
	// Remote: reads come from a cloud backend.
	Remote bool
}

// Source is the one seam between surfaces and data.
type Source interface {
	// Capabilities is static for the source's lifetime.
	Capabilities() Caps
	// Scope names what the header shows ("local", "acme/platform").
	Scope() string

	// Catalog reads the component/entity catalog with the change overlay.
	Catalog(ctx context.Context) (viewmodel.CatalogView, error)
	// Component reads one component's detail page; ok=false when the key
	// is not in the catalog.
	Component(ctx context.Context, key string) (viewmodel.ComponentView, bool, error)
	// Runs reads the run history, newest first.
	Runs(ctx context.Context) (viewmodel.RunListView, error)
	// Run reads one run's jobs and steps.
	Run(ctx context.Context, execID string) (viewmodel.RunView, error)
	// Sessions reads the live agent-session registry.
	Sessions(ctx context.Context) ([]live.Entry, error)
	// StepLog reads one step's captured output (sealed blob or live
	// working-tree file).
	StepLog(ctx context.Context, execID, jobID, stepID string) ([]byte, error)
	// Work reads the local Work lane: approval-sealed epic snapshots.
	Work(ctx context.Context) ([]EpicView, error)

	// Subscribe streams change notifications for the given topics (all
	// topics when none given) until ctx ends. The channel closes on
	// cancellation; a Source never blocks on a slow consumer — deltas
	// coalesce instead (at most one pending per topic).
	Subscribe(ctx context.Context, topics ...Topic) (<-chan Delta, error)

	// Close releases watchers and handles.
	Close() error
}
