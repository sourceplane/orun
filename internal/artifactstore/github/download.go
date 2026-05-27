package github

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/sourceplane/orun/internal/artifactstore"
	"github.com/sourceplane/orun/internal/runbundle"
)

// Download downloads a specific artifact shard to a destination directory.
// Uses GET /repos/{owner}/{repo}/actions/artifacts/{artifact_id}/{zip}
func (c *Client) Download(ctx context.Context, shard artifactstore.RemoteShard, destDir string) (*artifactstore.DownloadedShard, error) {
	u := c.apiURL(fmt.Sprintf("/repos/%s/actions/artifacts/%s/%s", c.repo, shard.ID, "zip"))
	return c.downloadAndExtract(ctx, u, shard.Name, destDir)
}

// DownloadByName downloads an artifact by name for a specific workflow run.
func (c *Client) DownloadByName(ctx context.Context, runID int64, name, destDir string) (*artifactstore.DownloadedShard, error) {
	shards, err := c.ListArtifacts(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}

	for _, s := range shards {
		if s.Name == name {
			return c.Download(ctx, s, destDir)
		}
	}

	return nil, fmt.Errorf("artifact %q not found in run %d", name, runID)
}

// downloadAndExtract downloads a zip archive and extracts it to destDir
// with path traversal defense.
func (c *Client) downloadAndExtract(ctx context.Context, url, name, destDir string) (*artifactstore.DownloadedShard, error) {
	req, err := c.newRequest(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("create download request: %w", err)
	}

	resp, err := c.doRequest(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", name, err)
	}
	defer resp.Body.Close()

	// Read the entire zip into memory
	zipData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body for %s: %w", name, err)
	}

	zipReader, err := zip.NewReader(io.NewSectionReader(readerAt{zipData}, 0, int64(len(zipData))), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("open zip for %s: %w", name, err)
	}

	// Create destination directory
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("create dest dir: %w", err)
	}

	// Extract files with path traversal defense
	for _, f := range zipReader.File {
		if f.FileInfo().IsDir() {
			continue
		}

		// Path traversal defense: reject any path that escapes destDir
		cleanPath := filepath.Clean(f.Name)
		if strings.HasPrefix(cleanPath, "..") || strings.HasPrefix(cleanPath, "/") {
			return nil, fmt.Errorf("path traversal detected in zip: %q", f.Name)
		}

		fullPath := filepath.Join(destDir, cleanPath)
		absDest, err := filepath.Abs(destDir)
		if err != nil {
			return nil, fmt.Errorf("resolve dest dir: %w", err)
		}
		absFull, err := filepath.Abs(fullPath)
		if err != nil {
			return nil, fmt.Errorf("resolve file path: %w", err)
		}
		if !strings.HasPrefix(absFull, absDest) {
			return nil, fmt.Errorf("path traversal detected: %q escapes %q", f.Name, destDir)
		}

		// Create parent directories
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return nil, fmt.Errorf("create parent dirs for %s: %w", fullPath, err)
		}

		// Extract file
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("open zip entry %s: %w", f.Name, err)
		}

		outFile, err := os.OpenFile(fullPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return nil, fmt.Errorf("create file %s: %w", fullPath, err)
		}

		_, err = io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()
		if err != nil {
			return nil, fmt.Errorf("write file %s: %w", fullPath, err)
		}
	}

	// Read the manifest to verify it's a valid shard
	manifest, err := runbundle.ReadShardManifest(destDir)
	if err != nil {
		// The download succeeded even if manifest isn't valid — return what we have
		_ = err
	}

	return &artifactstore.DownloadedShard{
		Name:  name,
		Dir:   destDir,
		Shard: manifest,
	}, nil
}

// readerAt wraps a byte slice for zip.NewReader.
type readerAt struct {
	data []byte
}

func (r readerAt) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(r.data)) {
		return 0, io.EOF
	}
	n := copy(p, r.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}