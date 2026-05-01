package composition

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	godigest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sourceplane/orun/internal/model"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"
)

const (
	compositionPackageArtifactType = "application/vnd.sourceplane.orun.composition.package.v1"
	compositionPackageLayerType    = "application/vnd.sourceplane.orun.composition.package.layer.v1.tar+gzip"

	// Stack OCI media types (orun.io/v1 / kind: Stack format).
	stackArtifactType            = "application/vnd.orun.stack.v1"
	compositionsLayerMediaType   = "application/vnd.orun.stack.compositions.layer.v1+tar+gzip"
	examplesLayerMediaType       = "application/vnd.orun.stack.examples.layer.v1+tar+gzip"
)

// BuildPackageArchive validates a composition package directory and writes a .tgz archive to disk.
func BuildPackageArchive(rootDir, outputPath string) error {
	rootDir = filepath.Clean(rootDir)
	outputPath = filepath.Clean(outputPath)

	info, err := os.Stat(rootDir)
	if err != nil {
		return fmt.Errorf("failed to access package root %s: %w", rootDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("package root is not a directory: %s", rootDir)
	}

	digest, err := hashDirectory(rootDir)
	if err != nil {
		return fmt.Errorf("failed to hash package root %s: %w", rootDir, err)
	}
	if _, err := loadPackageSource(rootDir, model.CompositionSource{Name: "local", Kind: "dir", Path: rootDir}, digest, 0); err != nil {
		return fmt.Errorf("package validation failed: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	outputFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create archive %s: %w", outputPath, err)
	}
	defer outputFile.Close()

	absOutput, err := filepath.Abs(outputPath)
	if err != nil {
		return fmt.Errorf("failed to resolve output path %s: %w", outputPath, err)
	}

	gzipWriter := gzip.NewWriter(outputFile)
	defer gzipWriter.Close()
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	return writeTarEntries(tarWriter, rootDir, absOutput)
}

// StreamPublishPackage streams a composition package to an OCI registry in a single pass.
// When the package root contains a compositions/ subdirectory (new Stack format), two
// separate OCI layers are produced:
//   - compositions layer (stack.yaml + compositions/ tree): media type compositionsLayerMediaType
//   - examples layer (examples/ tree, if present): media type examplesLayerMediaType
//
// Legacy packages (orun.yaml, flat layout) are pushed as a single layer.
// No temporary file is written to disk.
func StreamPublishPackage(rootDir, ociRef string) error {
	rootDir = filepath.Clean(rootDir)

	digest, err := hashDirectory(rootDir)
	if err != nil {
		return fmt.Errorf("failed to hash package root %s: %w", rootDir, err)
	}
	if _, err := loadPackageSource(rootDir, model.CompositionSource{Name: "local", Kind: "dir", Path: rootDir}, digest, 0); err != nil {
		return fmt.Errorf("package validation failed: %w", err)
	}

	// Detect new Stack layout: compositions/ subdirectory present.
	compositionsDir := filepath.Join(rootDir, "compositions")
	if info, err := os.Stat(compositionsDir); err == nil && info.IsDir() {
		return pushStackPackage(rootDir, ociRef)
	}

	// Legacy single-layer push.
	archiveBytes, digestStr, err := buildArchiveInMemory(rootDir)
	if err != nil {
		return err
	}
	return pushToRegistry(archiveBytes, digestStr, ociRef)
}

// pushStackPackage builds two OCI layers (compositions + optional examples) and pushes
// them as a multi-layer stack artifact.
func pushStackPackage(rootDir, ociRef string) error {
	ctx := context.Background()
	ociRef = strings.TrimPrefix(strings.TrimSpace(ociRef), "oci://")

	registry, repository, tag := splitRefParts(ociRef)
	if registry == "" || repository == "" {
		return fmt.Errorf("invalid OCI ref %q: expected <registry>/<repo>[:tag]", ociRef)
	}
	if tag == "" {
		tag = "latest"
	}

	repo, err := newOCIRepository(registry + "/" + repository)
	if err != nil {
		return err
	}

	store := memory.New()
	var layers []ocispec.Descriptor

	// Build compositions layer: stack.yaml at root + compositions/ tree.
	compBytes, compDigest, err := buildFilteredArchiveInMemory(rootDir, func(relPath string) bool {
		return relPath == "stack.yaml" || relPath == "orun.yaml" ||
			strings.HasPrefix(filepath.ToSlash(relPath), "compositions/")
	})
	if err != nil {
		return fmt.Errorf("failed to build compositions layer: %w", err)
	}
	compDesc := ocispec.Descriptor{
		MediaType: compositionsLayerMediaType,
		Digest:    godigest.Digest(compDigest),
		Size:      int64(len(compBytes)),
	}
	if err := store.Push(ctx, compDesc, bytes.NewReader(compBytes)); err != nil {
		return fmt.Errorf("failed to stage compositions layer: %w", err)
	}
	layers = append(layers, compDesc)

	// Build examples layer if examples/ directory exists and is non-empty.
	examplesDir := filepath.Join(rootDir, "examples")
	if info, statErr := os.Stat(examplesDir); statErr == nil && info.IsDir() {
		exBytes, exDigest, buildErr := buildFilteredArchiveInMemory(rootDir, func(relPath string) bool {
			return strings.HasPrefix(filepath.ToSlash(relPath), "examples/")
		})
		if buildErr != nil {
			return fmt.Errorf("failed to build examples layer: %w", buildErr)
		}
		if len(exBytes) > 0 {
			exDesc := ocispec.Descriptor{
				MediaType: examplesLayerMediaType,
				Digest:    godigest.Digest(exDigest),
				Size:      int64(len(exBytes)),
			}
			if err := store.Push(ctx, exDesc, bytes.NewReader(exBytes)); err != nil {
				return fmt.Errorf("failed to stage examples layer: %w", err)
			}
			layers = append(layers, exDesc)
		}
	}

	manifestDesc, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_1, stackArtifactType, oras.PackManifestOptions{
		Layers: layers,
	})
	if err != nil {
		return fmt.Errorf("failed to pack OCI manifest: %w", err)
	}
	if err := store.Tag(ctx, manifestDesc, tag); err != nil {
		return fmt.Errorf("failed to tag manifest: %w", err)
	}
	if _, err := oras.Copy(ctx, store, tag, repo, tag, oras.DefaultCopyOptions); err != nil {
		return fmt.Errorf("oras push failed: %w", err)
	}
	return nil
}

// buildFilteredArchiveInMemory builds a tar+gzip archive from the files in rootDir
// that satisfy the keep predicate (relPath is slash-separated relative to rootDir).
func buildFilteredArchiveInMemory(rootDir string, keep func(relPath string) bool) ([]byte, string, error) {
	var buf bytes.Buffer
	h := sha256.New()
	mw := io.MultiWriter(&buf, h)

	gw := gzip.NewWriter(mw)
	tw := tar.NewWriter(gw)

	if err := writeFilteredTarEntries(tw, rootDir, keep); err != nil {
		return nil, "", fmt.Errorf("failed to build filtered archive: %w", err)
	}
	if err := tw.Close(); err != nil {
		return nil, "", err
	}
	if err := gw.Close(); err != nil {
		return nil, "", err
	}

	digestStr := "sha256:" + hex.EncodeToString(h.Sum(nil))
	return buf.Bytes(), digestStr, nil
}

// writeFilteredTarEntries writes files under rootDir that satisfy the keep predicate.
func writeFilteredTarEntries(tw *tar.Writer, rootDir string, keep func(relPath string) bool) error {
	var files []string
	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(rootDir, path)
		if err != nil {
			return err
		}
		if keep(relPath) {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk package root %s: %w", rootDir, err)
	}
	sort.Strings(files)

	for _, path := range files {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(rootDir, path)
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relPath)
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if _, err := tw.Write(data); err != nil {
			return err
		}
	}
	return nil
}

// buildArchiveInMemory builds a tar+gzip archive in memory, computing the sha256 digest
// simultaneously in a single pass. No file I/O is performed.
func buildArchiveInMemory(rootDir string) ([]byte, string, error) {
	var buf bytes.Buffer
	h := sha256.New()
	mw := io.MultiWriter(&buf, h)

	gw := gzip.NewWriter(mw)
	tw := tar.NewWriter(gw)

	if err := writeTarEntries(tw, rootDir, ""); err != nil {
		return nil, "", fmt.Errorf("failed to build archive: %w", err)
	}
	if err := tw.Close(); err != nil {
		return nil, "", err
	}
	if err := gw.Close(); err != nil {
		return nil, "", err
	}

	digestStr := "sha256:" + hex.EncodeToString(h.Sum(nil))
	return buf.Bytes(), digestStr, nil
}

// writeTarEntries writes all files under rootDir into tw with relative paths.
// skipAbsPath, if non-empty, is excluded from the archive (used by BuildPackageArchive
// to skip the output file when it lives inside rootDir).
func writeTarEntries(tw *tar.Writer, rootDir, skipAbsPath string) error {
	var files []string
	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if skipAbsPath != "" {
			absPath, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			if absPath == skipAbsPath {
				return nil
			}
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk package root %s: %w", rootDir, err)
	}
	sort.Strings(files)

	for _, path := range files {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(rootDir, path)
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relPath)
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if _, err := tw.Write(data); err != nil {
			return err
		}
	}
	return nil
}

// PushArchiveFile reads an existing .tgz archive from disk and pushes it to an OCI registry.
func PushArchiveFile(archivePath, ociRef string) error {
	archivePath = filepath.Clean(archivePath)
	data, err := os.ReadFile(archivePath)
	if err != nil {
		return fmt.Errorf("failed to read archive %s: %w", archivePath, err)
	}
	h := sha256.New()
	h.Write(data)
	digestStr := "sha256:" + hex.EncodeToString(h.Sum(nil))
	return pushToRegistry(data, digestStr, ociRef)
}

// pushToRegistry pushes archiveBytes to the OCI registry described by ociRef using oras-go.
// Credentials are read from the Docker credential store (~/.docker/config.json).
func pushToRegistry(archiveBytes []byte, digestStr, ociRef string) error {
	ctx := context.Background()
	ociRef = strings.TrimPrefix(strings.TrimSpace(ociRef), "oci://")

	registry, repository, tag := splitRefParts(ociRef)
	if registry == "" || repository == "" {
		return fmt.Errorf("invalid OCI ref %q: expected <registry>/<repo>[:tag]", ociRef)
	}
	if tag == "" {
		tag = "latest"
	}

	repo, err := newOCIRepository(registry + "/" + repository)
	if err != nil {
		return err
	}

	store := memory.New()

	layerDesc := ocispec.Descriptor{
		MediaType: compositionPackageLayerType,
		Digest:    godigest.Digest(digestStr),
		Size:      int64(len(archiveBytes)),
	}
	if err := store.Push(ctx, layerDesc, bytes.NewReader(archiveBytes)); err != nil {
		return fmt.Errorf("failed to stage layer: %w", err)
	}

	manifestDesc, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_1, compositionPackageArtifactType, oras.PackManifestOptions{
		Layers: []ocispec.Descriptor{layerDesc},
	})
	if err != nil {
		return fmt.Errorf("failed to pack OCI manifest: %w", err)
	}

	if err := store.Tag(ctx, manifestDesc, tag); err != nil {
		return fmt.Errorf("failed to tag manifest: %w", err)
	}

	if _, err := oras.Copy(ctx, store, tag, repo, tag, oras.DefaultCopyOptions); err != nil {
		return fmt.Errorf("oras push failed: %w", err)
	}

	return nil
}
