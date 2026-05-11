package composition

import (
	"fmt"
	"os"
)

// FetchToDir downloads an OCI composition package and extracts its compositions
// into destDir. Each exported composition becomes a subdirectory inside destDir,
// mirroring the compositions/ tree from the package (e.g. destDir/TerraformPlan/compositions.yaml).
// destDir must not already exist; the caller is responsible for pre-flight checks.
func FetchToDir(ociRef, destDir string) (string, error) {
	ref := normalizeOCIRef(ociRef)
	if ref == "" {
		return "", fmt.Errorf("invalid OCI ref: %q", ociRef)
	}

	digest, err := resolveOCIDigest(ref)
	if err != nil {
		return "", fmt.Errorf("failed to resolve %s: %w", ociRef, err)
	}

	cacheDir, err := ensureCachedOCI(ref, digest)
	if err != nil {
		return "", fmt.Errorf("failed to fetch OCI package: %w", err)
	}

	// Copy the full cache root: stack.yaml (or orun.yaml) + compositions/ tree.
	// The compositions layer already contains only these files.
	if err := os.CopyFS(destDir, os.DirFS(cacheDir)); err != nil {
		return "", fmt.Errorf("failed to write to %s: %w", destDir, err)
	}

	return destDir, nil
}
