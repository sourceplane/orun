package catalogmodel

// ComponentHistoryEvent is an append-only event recorded under a component's
// history directory. See data-model.md §5.
//
// Stored at:
//
//	sources/<sourceSnapshotKey>/catalogs/<catalogSnapshotKey>/history/components/<name>/events/<seq>-<eventKind>.json
//
// `<seq>` is a zero-padded 9-digit monotonic counter scoped per component
// per catalog, allocated by the writer (C3) via a sentinel file.
type ComponentHistoryEvent struct {
	APIVersion         string `json:"apiVersion"`
	Kind               string `json:"kind"`
	EventType          string `json:"eventType"`
	ComponentKey       string `json:"componentKey"`
	SourceSnapshotKey  string `json:"sourceSnapshotKey"`
	CatalogSnapshotKey string `json:"catalogSnapshotKey"`
	RevisionKey        string `json:"revisionKey"`
	ExecutionKey       string `json:"executionKey"`
	TriggerName        string `json:"triggerName"`
	Profile            string `json:"profile"`
	Environment        string `json:"environment"`
	Status             string `json:"status"`
	At                 string `json:"at"`
}

// EventType enum values per data-model.md §5.
const (
	EventCatalogResolved    = "catalog.resolved"
	EventManifestChanged    = "manifest.changed"
	EventPlanCreated        = "plan.created"
	EventPlanFailed         = "plan.failed"
	EventExecutionStarted   = "execution.started"
	EventExecutionCompleted = "execution.completed"
	EventExecutionFailed    = "execution.failed"
	EventDependencyAdded    = "dependency.added"
	EventDependencyRemoved  = "dependency.removed"
)
