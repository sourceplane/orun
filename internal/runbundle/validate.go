package runbundle

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var supportedSchemaVersions = map[string]bool{
	"1.0.0": true,
}

// ValidateShardManifest checks that a shard manifest has valid fields.
func ValidateShardManifest(m *RunBundleShardManifest) error {
	if m == nil {
		return fmt.Errorf("manifest is nil")
	}
	if m.APIVersion == "" {
		return fmt.Errorf("manifest.apiVersion is required")
	}
	if m.Kind != manifestKind {
		return fmt.Errorf("manifest.kind: got %q, want %q", m.Kind, manifestKind)
	}
	if !supportedSchemaVersions[m.SchemaVersion] {
		return fmt.Errorf("unsupported schema version: %q", m.SchemaVersion)
	}
	if m.Role != ShardRolePlan && m.Role != ShardRoleJob {
		return fmt.Errorf("invalid shard role: %q", m.Role)
	}
	if m.ExecID == "" {
		return fmt.Errorf("manifest.execId is required")
	}
	if m.PlanID == "" {
		return fmt.Errorf("manifest.planId is required")
	}
	return nil
}

// ValidateShardFiles checks that all files listed in the manifest exist and
// that their checksums match (when checksums.json is present).
func ValidateShardFiles(shardDir string, m *RunBundleShardManifest) error {
	if m == nil || m.Files == nil {
		return fmt.Errorf("manifest or file list is nil")
	}

	// Check all listed files exist and no path traversal
	for logicalName, relPath := range m.Files {
		if err := validateFilePath(shardDir, relPath); err != nil {
			return fmt.Errorf("file %q (%s): %w", logicalName, relPath, err)
		}
		fullPath := filepath.Join(shardDir, relPath)
		info, err := os.Stat(fullPath)
		if err != nil {
			return fmt.Errorf("file %q (%s): %w", logicalName, relPath, err)
		}
		if info.IsDir() {
			continue // directories are valid entries (e.g. logs/)
		}
	}

	// Validate checksums if present
	if checksumsPath, ok := m.Files["checksums"]; ok {
		fullPath := filepath.Join(shardDir, checksumsPath)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return fmt.Errorf("failed to read checksums file: %w", err)
		}

		var checksums Checksums
		if err := json.Unmarshal(data, &checksums); err != nil {
			return fmt.Errorf("failed to parse checksums: %w", err)
		}

		for relPath, expectedDigest := range checksums.Files {
			fullPath := filepath.Join(shardDir, relPath)
			fileData, err := os.ReadFile(fullPath)
			if err != nil {
				return fmt.Errorf("failed to read %s for checksum verification: %w", relPath, err)
			}
			h := sha256.Sum256(fileData)
			actualDigest := fmt.Sprintf("%x", h)
			if actualDigest != expectedDigest {
				return fmt.Errorf("checksum mismatch for %s: got %s, want %s", relPath, actualDigest, expectedDigest)
			}
		}
	}

	return nil
}

// validateFilePath ensures relPath does not escape shardDir via path traversal.
func validateFilePath(shardDir, relPath string) error {
	clean := filepath.Clean(relPath)
	if strings.HasPrefix(clean, "..") || strings.HasPrefix(clean, "/") {
		return fmt.Errorf("path traversal detected: %q", relPath)
	}
	fullPath := filepath.Join(shardDir, clean)
	absBase, err := filepath.Abs(shardDir)
	if err != nil {
		return fmt.Errorf("failed to resolve shard dir: %w", err)
	}
	absFull, err := filepath.Abs(fullPath)
	if err != nil {
		return fmt.Errorf("failed to resolve file path: %w", err)
	}
	if !strings.HasPrefix(absFull, absBase) {
		return fmt.Errorf("path traversal detected: %q escapes %q", relPath, shardDir)
	}
	return nil
}