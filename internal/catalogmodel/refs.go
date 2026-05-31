package catalogmodel

// SourceRef is the small denormalized pointer file under refs/sources/.
// One file per ref name (`latest`, `current`, `main`, `branches/<branch>`,
// `prs/<num>`). Mutable — atomically rewritten by the writer.
//
// See data-model.md §8.1.
type SourceRef struct {
	APIVersion        string `json:"apiVersion"`
	Kind              string `json:"kind"`
	Name              string `json:"name"`
	SourceScope       string `json:"sourceScope"`
	SourceSnapshotKey string `json:"sourceSnapshotKey"`
	HeadRevision      string `json:"headRevision"`
	TreeHash          string `json:"treeHash"`
	WorkingTree       string `json:"workingTree"`
	Authoritative     bool   `json:"authoritative"`
	UpdatedAt         string `json:"updatedAt"`
}

// CatalogRef is the small denormalized pointer file under refs/catalogs/.
// Same shape conventions as SourceRef. See data-model.md §8.2.
type CatalogRef struct {
	APIVersion         string `json:"apiVersion"`
	Kind               string `json:"kind"`
	Name               string `json:"name"`
	SourceScope        string `json:"sourceScope"`
	SourceSnapshotKey  string `json:"sourceSnapshotKey"`
	CatalogSnapshotKey string `json:"catalogSnapshotKey"`
	CatalogHash        string `json:"catalogHash"`
	HeadRevision       string `json:"headRevision"`
	TreeHash           string `json:"treeHash"`
	Authoritative      bool   `json:"authoritative"`
	Preview            bool   `json:"preview"`
	UpdatedAt          string `json:"updatedAt"`
}

// Standard ref names per data-model.md §8.
const (
	RefNameLatest  = "latest"
	RefNameCurrent = "current"
	RefNameMain    = "main"
)
