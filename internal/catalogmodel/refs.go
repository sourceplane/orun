package catalogmodel

// Standard ref names per data-model.md §8. The denormalized `SourceRef` /
// `CatalogRef` pointer structs they once named were store-only artifacts of the
// retired legacy catalog store; only the ref-name vocabulary survives, consumed
// by the object-model ref selectors (e.g. `orun catalog diff` defaults).
const (
	RefNameLatest  = "latest"
	RefNameCurrent = "current"
	RefNameMain    = "main"
)
