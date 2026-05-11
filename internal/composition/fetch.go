package composition

import (
	"fmt"
	"os"
	"path/filepath"
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

	// Copy the compositions/ subtree so destDir contains only composition files,
	// not the package metadata (stack.yaml / orun.yaml).
	compositionsDir := filepath.Join(cacheDir, "compositions")
	if _, statErr := os.Stat(compositionsDir); statErr != nil {
		// Legacy flat packages have no compositions/ subdir; fall back to the full root.
		compositionsDir = cacheDir
	}

	if err := os.CopyFS(destDir, os.DirFS(compositionsDir)); err != nil {
		return "", fmt.Errorf("failed to write to %s: %w", destDir, err)
	}

	return destDir, nil
}
