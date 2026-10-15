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
	"sync/atomic"
	"time"
)

var shaPattern = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)

// The action cache's time bounds (specs/orun-cache-efficiency CE0). A
// download is bounded by progress, not by a wall-clock total: it fails when
// no byte has arrived for downloadIdleTimeout, and a slow link that keeps
// moving completes. A process waiting for another's download of the same
// action waits actionFetchLockWait, longer than any download the idle bound
// lets through, and reclaims a lock whose holder stopped refreshing it.
var (
	downloadIdleTimeout = 30 * time.Second
	refResolveTimeout   = 30 * time.Second
	actionFetchLockWait = 10 * time.Minute
	lockTouchInterval   = 5 * time.Second
	lockStaleAfter      = 60 * time.Second
)

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

	actionKey := reference.CachePath() + "@" + resolvedRef + "#" + reference.Path
	e.actionCacheMu.RLock()
	if cached := e.actionCache[actionKey]; cached != nil {
		e.actionCacheMu.RUnlock()
		return cached, nil
	}
	e.actionCacheMu.RUnlock()

	result, err, _ := e.actionFetchSF.Do(actionKey, func() (interface{}, error) {
		e.actionCacheMu.RLock()
		if cached := e.actionCache[actionKey]; cached != nil {
			e.actionCacheMu.RUnlock()
			return cached, nil
		}
		e.actionCacheMu.RUnlock()

		cacheDir := filepath.Join(e.cacheDir, reference.CachePath(), resolvedRef)
		markerPath := filepath.Join(cacheDir, ".ready")
		lockPath := cacheDir + ".lock"

		if _, err := os.Stat(markerPath); err == nil {
			loaded, err := e.loadCachedAction(reference, resolvedRef, cacheDir)
			if err != nil {
				return nil, err
			}
			e.actionCacheMu.Lock()
			e.actionCache[actionKey] = loaded
			e.actionCacheMu.Unlock()
			return loaded, nil
		}

		if err := os.MkdirAll(filepath.Dir(cacheDir), 0755); err != nil {
			return nil, fmt.Errorf("create cache parent for %s: %w", reference.Repository(), err)
		}

		if err := withLock(lockPath, actionFetchLockWait, func() error {
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

		loaded, err := e.loadCachedAction(reference, resolvedRef, cacheDir)
		if err != nil {
			return nil, err
		}
		e.actionCacheMu.Lock()
		e.actionCache[actionKey] = loaded
		e.actionCacheMu.Unlock()
		return loaded, nil
	})
	if err != nil {
		return nil, err
	}
	return result.(*resolvedAction), nil
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

	cacheKey := reference.Repository() + "@" + reference.Ref
	e.refCacheMu.RLock()
	if sha, ok := e.refCache[cacheKey]; ok {
		e.refCacheMu.RUnlock()
		return sha, nil
	}
	e.refCacheMu.RUnlock()

	if sha := e.readRefFromDisk(reference); sha != "" {
		e.refCacheMu.Lock()
		e.refCache[cacheKey] = sha
		e.refCacheMu.Unlock()
		return sha, nil
	}

	result, err, _ := e.refResolveSF.Do(cacheKey, func() (interface{}, error) {
		e.refCacheMu.RLock()
		if sha, ok := e.refCache[cacheKey]; ok {
			e.refCacheMu.RUnlock()
			return sha, nil
		}
		e.refCacheMu.RUnlock()

		sha, err := e.resolveRemoteRefAPI(ctx, apiURL, token, reference)
		if err != nil {
			return "", err
		}
		e.refCacheMu.Lock()
		e.refCache[cacheKey] = sha
		e.refCacheMu.Unlock()
		e.writeRefToDisk(reference, sha)
		return sha, nil
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

func (e *Engine) resolveRemoteRefAPI(ctx context.Context, apiURL string, token string, reference ActionReference) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, refResolveTimeout)
	defer cancel()
	url := strings.TrimRight(apiURL, "/") + "/repos/" + reference.Repository() + "/commits/" + reference.Ref
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create ref resolution request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "orun")
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
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
	if err != nil {
		return fmt.Errorf("create tarball request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "orun")
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

	// Bounded by progress: the body may take as long as it takes while bytes
	// keep arriving; a stall of downloadIdleTimeout cancels it.
	body := newProgressReader(response.Body, downloadIdleTimeout, cancel)
	defer body.Close()
	if err := extractTarGzStream(body, destination); err != nil {
		if body.stalled.Load() {
			return fmt.Errorf("download tarball: no data received for %s", downloadIdleTimeout)
		}
		return err
	}
	return nil
}

// progressReader cancels a download that stops making progress: every read
// that returns bytes pushes the deadline out by idle; a deadline that passes
// cancels the request's context and marks the reader stalled, so the caller
// can report the stall rather than a bare cancellation.
type progressReader struct {
	r       io.ReadCloser
	idle    time.Duration
	timer   *time.Timer
	stalled atomic.Bool
}

func newProgressReader(r io.ReadCloser, idle time.Duration, cancel context.CancelFunc) *progressReader {
	p := &progressReader{r: r, idle: idle}
	p.timer = time.AfterFunc(idle, func() {
		p.stalled.Store(true)
		cancel()
	})
	return p
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.timer.Reset(p.idle)
	}
	return n, err
}

func (p *progressReader) Close() error {
	p.timer.Stop()
	return p.r.Close()
}

// extractTarGzStream extracts a GitHub repository tarball (one top-level
// directory, dropped) from r into destination.
func extractTarGzStream(r io.Reader, destination string) error {
	gzipReader, err := gzip.NewReader(r)
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

var refSlugPattern = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func (e *Engine) refDiskPath(reference ActionReference) string {
	slug := refSlugPattern.ReplaceAllString(reference.Ref, "_")
	if slug == "" {
		return ""
	}
	return filepath.Join(e.cacheDir, reference.CachePath(), "refs", slug)
}

func (e *Engine) readRefFromDisk(reference ActionReference) string {
	path := e.refDiskPath(reference)
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sha := strings.TrimSpace(string(data))
	if !shaPattern.MatchString(sha) {
		return ""
	}
	cacheDir := filepath.Join(e.cacheDir, reference.CachePath(), strings.ToLower(sha))
	if _, err := os.Stat(filepath.Join(cacheDir, ".ready")); err != nil {
		return ""
	}
	return strings.ToLower(sha)
}

func (e *Engine) writeRefToDisk(reference ActionReference, sha string) {
	path := e.refDiskPath(reference)
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(sha), 0644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// withLock runs fn holding the lock file at lockPath, which other orun
// processes on the machine contend for. The holder records its pid and
// refreshes the file's mtime every lockTouchInterval while fn runs; a waiter
// that finds a lock untouched for lockStaleAfter knows its holder died
// (killed mid-download, out of memory, Ctrl-C) and reclaims it, so one crash
// never blocks every later run on the machine. The reclaim is a rename, so
// of several waiters only one takes the stale file and the holder that
// replaces it is never deleted by another.
func withLock(lockPath string, timeout time.Duration, fn func() error) error {
	deadline := time.Now().Add(timeout)
	for {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			_, _ = fmt.Fprintf(file, "%d %d\n", os.Getpid(), time.Now().Unix())
			_ = file.Close()
			stop := make(chan struct{})
			done := make(chan struct{})
			go func() {
				defer close(done)
				ticker := time.NewTicker(lockTouchInterval)
				defer ticker.Stop()
				for {
					select {
					case <-stop:
						return
					case <-ticker.C:
						now := time.Now()
						_ = os.Chtimes(lockPath, now, now)
					}
				}
			}()
			fnErr := fn()
			close(stop)
			<-done
			_ = os.Remove(lockPath)
			return fnErr
		}
		if !os.IsExist(err) {
			return fmt.Errorf("acquire lock %s: %w", lockPath, err)
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > lockStaleAfter {
			stale := lockPath + ".stale"
			if os.Rename(lockPath, stale) == nil {
				_ = os.Remove(stale)
			}
			continue
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for action cache lock %s", lockPath)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
