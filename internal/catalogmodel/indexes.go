package catalogmodel

// ComponentExecutionRow is one denormalized execution-history entry for a
// component — a flat row carrying the per-execution metadata needed for fast
// listing. It is the shape `orun catalog describe`/`history` render after
// reconstructing the history join from the object graph (`objread`).
type ComponentExecutionRow struct {
	ComponentKey       string `json:"componentKey,omitempty"`
	SourceSnapshotKey  string `json:"sourceSnapshotKey,omitempty"`
	CatalogSnapshotKey string `json:"catalogSnapshotKey,omitempty"`
	RevisionKey        string `json:"revisionKey"`
	ExecutionKey       string `json:"executionKey"`
	TriggerName        string `json:"triggerName"`
	Profile            string `json:"profile"`
	Environment        string `json:"environment"`
	Status             string `json:"status"`
	CreatedAt          string `json:"createdAt"`
}
