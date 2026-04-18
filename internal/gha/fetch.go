package gha

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var shaPattern = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)

type resolvedAction struct {
	Reference   ActionReference
	ResolvedRef string
	CacheDir    string
	ActionDir   string
	Metadata    *ActionMetadata
}

func (e *Engine) resolveAction(ctx context.Context, baseDir string, apiURL string, token string, raw string) (*resolvedAction, error) {
	reference, err := ParseActionReference(raw)
	if err != nil {
		return nil, err
	}

	switch reference.Kind {
	case referenceKindLocal:
		actionDir := filepath.Clean(filepath.Join(baseDir, reference.Local))
		metadata, _, err := LoadActionMetadata(actionDir)
		if err != nil {
			return nil, err
		}
		return &resolvedAction{Reference: reference, ActionDir: actionDir, Metadata: metadata}, nil
	case referenceKindRemote:
		return e.fetchRemoteAction(ctx, apiURL, token, reference)
	default:
		return &resolvedAction{Reference: reference}, nil
	}
}

func (e *Engine) fetchRemoteAction(ctx context.Context, apiURL string, token string, reference ActionReference) (*resolvedAction, error) {
	resolvedRef, err := e.resolveRemoteRef(ctx, apiURL, token, reference)
	if err != nil {
		return nil, err
	}

	cacheDir := filepath.Join(e.cacheDir, reference.CachePath(), resolvedRef)
	markerPath := filepath.Join(cacheDir, ".ready")
	lockPath := cacheDir + ".lock"

	if _, err := os.Stat(markerPath); err == nil {
		return e.loadCachedAction(reference, resolvedRef, cacheDir)
	}

	if err := os.MkdirAll(filepath.Dir(cacheDir), 0755); err != nil {
		return nil, fmt.Errorf("create cache parent for %s: %w", reference.Repository(), err)
	}

	if err := withLock(lockPath, 30*time.Second, func() error {
		if _, err := os.Stat(markerPath); err == nil {
			return nil
		}

		tempDir := cacheDir + ".tmp"
		_ = os.RemoveAll(tempDir)
		if err := os.MkdirAll(tempDir, 0755); err != nil {
			return fmt.Errorf("create temp cache directory: %w", err)
		}

		archiveURL := strings.TrimRight(apiURL, "/") + "/repos/" + reference.Repository() + "/tarball/" + reference.Ref
		if err := e.downloadAndExtractTarball(ctx, archiveURL, token, tempDir); err != nil {
			_ = os.RemoveAll(tempDir)
			return err
		}

		if err := os.WriteFile(filepath.Join(tempDir, ".ready"), []byte(resolvedRef), 0644); err != nil {
			_ = os.RemoveAll(tempDir)
			return fmt.Errorf("write cache marker: %w", err)
		}

		_ = os.RemoveAll(cacheDir)
		if err := os.Rename(tempDir, cacheDir); err != nil {
			_ = os.RemoveAll(tempDir)
			return fmt.Errorf("promote cached action: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return e.loadCachedAction(reference, resolvedRef, cacheDir)
}

func (e *Engine) loadCachedAction(reference ActionReference, resolvedRef string, cacheDir string) (*resolvedAction, error) {
	actionDir := cacheDir
	if reference.Path != "" {
		actionDir = filepath.Join(cacheDir, filepath.FromSlash(reference.Path))
	}

	metadata, _, err := LoadActionMetadata(actionDir)
	if err != nil {
		return nil, err
	}

	return &resolvedAction{
		Reference:   reference,
		ResolvedRef: resolvedRef,
		CacheDir:    cacheDir,
		ActionDir:   actionDir,
		Metadata:    metadata,
	}, nil
}

func (e *Engine) resolveRemoteRef(ctx context.Context, apiURL string, token string, reference ActionReference) (string, error) {
	if shaPattern.MatchString(reference.Ref) {
		return strings.ToLower(reference.Ref), nil
	}

	url := strings.TrimRight(apiURL, "/") + "/repos/" + reference.Repository() + "/commits/" + reference.Ref
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create ref resolution request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "arx")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := e.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("resolve %s@%s: %w", reference.Repository(), reference.Ref, err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return "", fmt.Errorf("resolve %s@%s: unexpected status %d: %s", reference.Repository(), reference.Ref, response.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode resolved ref for %s@%s: %w", reference.Repository(), reference.Ref, err)
	}
	if !shaPattern.MatchString(payload.SHA) {
		return "", fmt.Errorf("resolve %s@%s returned invalid sha %q", reference.Repository(), reference.Ref, payload.SHA)
	}
	return strings.ToLower(payload.SHA), nil
}

func (e *Engine) downloadAndExtractTarball(ctx context.Context, archiveURL string, token string, destination string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
	if err != nil {
		return fmt.Errorf("create tarball request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "arx")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := e.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download tarball: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("download tarball: unexpected status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	gzipReader, err := gzip.NewReader(response.Body)
	if err != nil {
		return fmt.Errorf("open action tarball: %w", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read action tar entry: %w", err)
		}

		name := strings.TrimPrefix(header.Name, "./")
		parts := strings.SplitN(name, "/", 2)
		if len(parts) != 2 {
			continue
		}
		relativePath := parts[1]
		if relativePath == "" {
			continue
		}

		targetPath := filepath.Join(destination, filepath.FromSlash(relativePath))
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("create extracted directory %s: %w", targetPath, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return fmt.Errorf("create parent directory for %s: %w", targetPath, err)
			}
			file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("create extracted file %s: %w", targetPath, err)
			}
			if _, err := io.Copy(file, tarReader); err != nil {
				_ = file.Close()
				return fmt.Errorf("write extracted file %s: %w", targetPath, err)
			}
			if err := file.Close(); err != nil {
				return fmt.Errorf("close extracted file %s: %w", targetPath, err)
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return fmt.Errorf("create parent directory for symlink %s: %w", targetPath, err)
			}
			if err := os.Symlink(header.Linkname, targetPath); err != nil && !os.IsExist(err) {
				return fmt.Errorf("create extracted symlink %s: %w", targetPath, err)
			}
		}
	}

	return nil
}

func withLock(lockPath string, timeout time.Duration, fn func() error) error {
	deadline := time.Now().Add(timeout)
	for {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			_ = file.Close()
			defer os.Remove(lockPath)
			return fn()
		}
		if !os.IsExist(err) {
			return fmt.Errorf("acquire lock %s: %w", lockPath, err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for action cache lock %s", lockPath)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
