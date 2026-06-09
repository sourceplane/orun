package catalogmodel

// SourceSnapshot is the persisted record describing a workspace's VCS state
// at the moment a catalog refresh ran. See data-model.md §1.
//
// Stored at: sources/<sourceSnapshotKey>/source.json. Immutable after first
// write.
type SourceSnapshot struct {
	APIVersion        string `json:"apiVersion"`
	Kind              string `json:"kind"`
	SourceSnapshotKey string `json:"sourceSnapshotKey"`
	SourceSnapshotID  string `json:"sourceSnapshotId"`
	Repo              string `json:"repo"`
	RemoteURL         string `json:"remoteUrl"`
	Ref               string `json:"ref"`
	Branch            string `json:"branch"`
	SourceScope       string `json:"sourceScope"`
	HeadRevision      string `json:"headRevision"`
	TreeHash          string `json:"treeHash"`
	WorkingTree       string `json:"workingTree"`
	DirtyHash         string `json:"dirtyHash"`
	CatalogInputHash  string `json:"catalogInputHash"`
	CreatedAt         string `json:"createdAt"`
}

// SourceScope enum values per data-model.md §1.
const (
	SourceScopeBranchMain      = "branch-main"
	SourceScopeBranchProtected = "branch-protected"
	SourceScopeBranchFeature   = "branch-feature"
	SourceScopePR              = "pr"
	SourceScopeTag             = "tag"
	SourceScopeLocalDirty      = "local-dirty"
	SourceScopeLocalNoGit      = "local-nogit"
	SourceScopeCIEvent         = "ci-event"
)

// WorkingTree enum values per data-model.md §1.
const (
	WorkingTreeClean = "clean"
	WorkingTreeDirty = "dirty"
)

// APIVersion / Kind constants used by every persisted catalog object.
const (
	APIVersionV1Alpha1    = "orun.io/v1alpha1"
	KindSourceSnapshot    = "SourceSnapshot"
	KindCatalogSnapshot   = "CatalogSnapshot"
	KindComponentManifest = "ComponentManifest"
	KindCatalogGraph      = "CatalogGraph"
	KindComponentEvent    = "ComponentCatalogEvent"
	KindComponent         = "Component"
)
