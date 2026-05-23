package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sourceplane/orun/internal/artifactstore"
	"github.com/sourceplane/orun/internal/runbundle"
)

// InMemoryStore implements artifactstore.Store with an in-memory map.
// Useful for unit testing.
type InMemoryStore struct {
	mu     sync.RWMutex
	shards map[string]*memoryShard
	idSeq  int64
}

type memoryShard struct {
	shard     *runbundle.Shard
	createdAt time.Time
	manifest  *runbundle.RunBundleShardManifest
}

// New creates a new empty InMemoryStore.
func New() *InMemoryStore {
	return &InMemoryStore{
		shards: make(map[string]*memoryShard),
	}
}

// Upload stores a shard in memory.
func (s *InMemoryStore) Upload(ctx context.Context, shard *runbundle.Shard) (*artifactstore.UploadResult, error) {
	if shard == nil {
		return nil, fmt.Errorf("shard is required")
	}
	if shard.Manifest == nil {
		return nil, fmt.Errorf("shard manifest is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.idSeq++
	name := runbundle.ArtifactName(shard.ExecID, shard.Role, shard.Suffix, shard.Status)

	s.shards[name] = &memoryShard{
		shard:     shard,
		createdAt: time.Now().UTC(),
		manifest:  shard.Manifest,
	}

	return &artifactstore.UploadResult{
		ID:   fmt.Sprintf("mem-%d", s.idSeq),
		Name: name,
		Size: 0, // in-memory, size not tracked
	}, nil
}

// List returns all shards matching the filter criteria.
func (s *InMemoryStore) List(ctx context.Context, opts artifactstore.ListOptions) ([]artifactstore.RemoteShard, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []artifactstore.RemoteShard
	for name, ms := range s.shards {
		if opts.Prefix != "" && !hasPrefix(name, opts.Prefix) {
			continue
		}

		parsed := runbundle.ParseShardName(name)

		results = append(results, artifactstore.RemoteShard{
			Name:      name,
			CreatedAt: ms.createdAt,
			Parsed:    parsed,
			SourceMeta: map[string]string{
				"execId": ms.manifest.ExecID,
				"role":   string(ms.manifest.Role),
				"status": ms.manifest.Status,
			},
		})
	}

	return results, nil
}

// Download returns a reference to the in-memory shard by creating a
// temporary directory with the shard files.
func (s *InMemoryStore) Download(ctx context.Context, remote artifactstore.RemoteShard, destDir string) (*artifactstore.DownloadedShard, error) {
	s.mu.RLock()
	ms, ok := s.shards[remote.Name]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("shard not found: %s", remote.Name)
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create dest dir: %w", err)
	}

	// Copy shard files if they exist on disk
	if ms.shard.Dir != "" {
		if err := copyDir(ms.shard.Dir, destDir); err != nil {
			return nil, fmt.Errorf("failed to copy shard files: %w", err)
		}
	}

	return &artifactstore.DownloadedShard{
		Name:  remote.Name,
		Dir:   destDir,
		Shard: ms.manifest,
	}, nil
}

// hasPrefix checks if s starts with prefix.
func hasPrefix(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return s[:len(prefix)] == prefix
}

// copyDir copies a directory recursively.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dstPath, data, info.Mode())
	})
}