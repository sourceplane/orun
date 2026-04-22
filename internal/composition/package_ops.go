package composition

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sourceplane/gluon/internal/model"
)

const (
	compositionPackageArtifactType = "application/vnd.sourceplane.gluon.composition.package.v1"
	compositionPackageLayerType    = "application/vnd.sourceplane.gluon.composition.package.layer.v1.tar+gzip"
)

// BuildPackageArchive validates a composition package directory and writes a .tgz archive.
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

	gzipWriter := gzip.NewWriter(outputFile)
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	absOutput, err := filepath.Abs(outputPath)
	if err != nil {
		return fmt.Errorf("failed to resolve output path %s: %w", outputPath, err)
	}

	files := make([]string, 0)
	err = filepath.Walk(rootDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if absPath == absOutput {
			return nil
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
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if _, err := tarWriter.Write(data); err != nil {
			return err
		}
	}

	return nil
}

// PushPackageArchive publishes a package archive to an OCI registry using oras.
func PushPackageArchive(archivePath, ref string) error {
	archivePath = filepath.Clean(archivePath)
	if _, err := os.Stat(archivePath); err != nil {
		return fmt.Errorf("failed to access package archive %s: %w", archivePath, err)
	}
	if _, err := exec.LookPath("oras"); err != nil {
		return fmt.Errorf("oras is required to push composition packages")
	}

	remoteRef := strings.TrimPrefix(strings.TrimSpace(ref), "oci://")
	if remoteRef == "" {
		return fmt.Errorf("oci reference cannot be empty")
	}

	cmd := exec.Command(
		"oras",
		"push",
		remoteRef,
		"--artifact-type",
		compositionPackageArtifactType,
		fmt.Sprintf("%s:%s", archivePath, compositionPackageLayerType),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("oras push failed: %w\n%s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
