package catalogmodel

// The lazy schema up-conversion seam (orun-service-catalog/design.md §8,
// migration.md §2). L1 blobs are immutable per snapshot, so an old snapshot
// keeps its `apiVersion` forever and converts on read — never a destructive
// in-place migration (S-3).
//
// SC0 stands the seam up with no field rewrites: v1alpha1 → v1 is recognized
// and the version string graduates, but the body is unchanged. The total,
// tested field codemod lands in SC11.

// UpConvertAPIVersion maps a stored apiVersion onto the current one. v1alpha1
// up-converts to v1; v1 passes through. The boolean reports whether the input
// was a recognized version that maps to v1 — an unknown/empty version returns
// (input, false) so callers can decide how to treat it.
func UpConvertAPIVersion(stored string) (current string, recognized bool) {
	switch stored {
	case APIVersionV1Alpha1:
		return APIVersionV1, true
	case APIVersionV1:
		return APIVersionV1, true
	default:
		return stored, false
	}
}

// IsCurrentAPIVersion reports whether apiVersion is already the graduated v1
// schema (i.e. needs no up-conversion).
func IsCurrentAPIVersion(apiVersion string) bool {
	return apiVersion == APIVersionV1
}
