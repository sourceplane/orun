package github

import (
	"context"
	"fmt"
	"os"

	"github.com/sourceplane/orun/internal/artifactstore"
	"github.com/sourceplane/orun/internal/runbundle"
)

// ManifestDetail holds the parsed manifest from a single shard artifact
// without requiring full hydration or log download.
type ManifestDetail struct {
	ShardName string
	Manifest  *runbundle.RunBundleShardManifest
}

// DownloadManifestOnly downloads a shard artifact ZIP to a temp directory,
// extracts it, reads only the manifest.json, and cleans up.
// This is the Level 2 detail path: manifest-only, no hydration, no logs.
func (c *Client) DownloadManifestOnly(ctx context.Context, shard artifactstore.RemoteShard) (*ManifestDetail, error) {
	destDir, err := os.MkdirTemp("", "orun-manifest-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(destDir)

	ds, err := c.Download(ctx, shard, destDir)
	if err != nil {
		return nil, fmt.Errorf("download shard %s: %w", shard.Name, err)
	}

	if ds.Shard == nil {
		// Download succeeded but no valid manifest — try reading explicitly
		manifest, readErr := runbundle.ReadShardManifest(destDir)
		if readErr != nil {
			return nil, fmt.Errorf("no valid manifest in shard %s: %w", shard.Name, readErr)
		}
		return &ManifestDetail{
			ShardName: shard.Name,
			Manifest:  manifest,
		}, nil
	}

	return &ManifestDetail{
		ShardName: shard.Name,
		Manifest:  ds.Shard,
	}, nil
}
