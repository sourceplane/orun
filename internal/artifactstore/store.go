package artifactstore

import (
	"context"
	"time"

	"github.com/sourceplane/orun/internal/runbundle"
)

// Store is the generic interface for artifact storage backends.
// Implementations include GitHub Actions artifacts, local directories, R2, S3, and Orun Cloud.
type Store interface {
	// Upload uploads a shard and returns the result.
	Upload(ctx context.Context, shard *runbundle.Shard) (*UploadResult, error)

	// List returns remote shards matching the given options.
	List(ctx context.Context, opts ListOptions) ([]RemoteShard, error)

	// Download downloads a shard to the destination directory and
	// returns the downloaded shard reference.
	Download(ctx context.Context, shard RemoteShard, destDir string) (*DownloadedShard, error)
}

// RemoteShard represents a shard stored in a remote backend.
type RemoteShard struct {
	Name       string
	ID         string
	SizeBytes  int64
	CreatedAt  time.Time
	ExpiresAt  time.Time
	Parsed     *runbundle.ParsedShardName
	SourceMeta map[string]string // backend-specific metadata
}

// ListOptions filters artifact listings.
type ListOptions struct {
	RunID  int64
	ExecID string
	Prefix string // filter by name prefix
}

// UploadResult describes the result of a successful upload.
type UploadResult struct {
	ID     string
	Name   string
	Size   int64
	Digest string // backend-reported digest if available
}

// DownloadedShard describes a downloaded and extracted shard.
type DownloadedShard struct {
	Name  string
	Dir   string // extracted shard directory
	Shard *runbundle.RunBundleShardManifest
}