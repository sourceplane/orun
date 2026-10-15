package gha

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func withLockTimings(t *testing.T, touch, stale time.Duration) {
	t.Helper()
	prevTouch, prevStale := lockTouchInterval, lockStaleAfter
	lockTouchInterval, lockStaleAfter = touch, stale
	t.Cleanup(func() { lockTouchInterval, lockStaleAfter = prevTouch, prevStale })
}

func TestWithLockReclaimsALockWhoseHolderDied(t *testing.T) {
	withLockTimings(t, 10*time.Millisecond, 50*time.Millisecond)
	lock := filepath.Join(t.TempDir(), "action.lock")
	if err := os.WriteFile(lock, []byte("4242 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	ran := false
	if err := withLock(lock, 2*time.Second, func() error { ran = true; return nil }); err != nil {
		t.Fatalf("withLock() error = %v; a lock whose holder died is reclaimed, not waited out", err)
	}
	if !ran {
		t.Fatal("fn did not run")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("reclaiming the stale lock took %s", elapsed)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatalf("lock left behind after fn: stat err = %v", err)
	}
}

func TestWithLockWaitsForALiveHolder(t *testing.T) {
	withLockTimings(t, 10*time.Millisecond, 60*time.Millisecond)
	lock := filepath.Join(t.TempDir(), "action.lock")

	var mu sync.Mutex
	var order []string
	holderDone := make(chan struct{})
	go func() {
		defer close(holderDone)
		_ = withLock(lock, 2*time.Second, func() error {
			// Longer than lockStaleAfter: only the holder's heartbeat keeps a
			// waiter from reclaiming the lock under it.
			time.Sleep(250 * time.Millisecond)
			mu.Lock()
			order = append(order, "holder")
			mu.Unlock()
			return nil
		})
	}()
	time.Sleep(30 * time.Millisecond)
	if err := withLock(lock, 2*time.Second, func() error {
		mu.Lock()
		order = append(order, "waiter")
		mu.Unlock()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	<-holderDone

	mu.Lock()
	defer mu.Unlock()
	if strings.Join(order, ",") != "holder,waiter" {
		t.Fatalf("order = %v; the waiter runs after the live holder, never over it", order)
	}
}

func TestWithLockRecordsItsHolder(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "action.lock")
	var holder string
	if err := withLock(lock, time.Second, func() error {
		data, err := os.ReadFile(lock)
		holder = string(data)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(holder, strconv.Itoa(os.Getpid())+" ") {
		t.Fatalf("lock holder = %q, want this process's pid", holder)
	}
}

// actionTarball is a GitHub-shaped repository tarball: one top-level
// directory, an action.yml and an incompressible dist file, so the gzip
// stream is a few KB a server can dribble out.
func actionTarball(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	rng := rand.New(rand.NewSource(1))
	dist := make([]byte, 6*1024)
	rng.Read(dist)
	entries := []struct {
		name string
		dir  bool
		body []byte
	}{
		{"owner-repo-abc123/", true, nil},
		{"owner-repo-abc123/action.yml", false, []byte("name: fake\nruns:\n  using: composite\n  steps: []\n")},
		{"owner-repo-abc123/dist/", true, nil},
		{"owner-repo-abc123/dist/index.js", false, dist},
	}
	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, Mode: 0o644}
		if entry.dir {
			header.Typeflag = tar.TypeDir
			header.Mode = 0o755
		} else {
			header.Typeflag = tar.TypeReg
			header.Size = int64(len(entry.body))
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if !entry.dir {
			if _, err := tw.Write(entry.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// dribblingServer serves tarball in chunks with gap between them, and once,
// after stallAfter bytes, a pause of stall.
func dribblingServer(t *testing.T, tarball []byte, chunk int, gap time.Duration, stallAfter int, stall time.Duration) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		stalled := false
		for i := 0; i < len(tarball); i += chunk {
			if stallAfter > 0 && !stalled && i >= stallAfter {
				stalled = true
				time.Sleep(stall)
			}
			end := i + chunk
			if end > len(tarball) {
				end = len(tarball)
			}
			if _, err := w.Write(tarball[i:end]); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(gap)
		}
	}))
}

func withDownloadIdleTimeout(t *testing.T, idle time.Duration) {
	t.Helper()
	prev := downloadIdleTimeout
	downloadIdleTimeout = idle
	t.Cleanup(func() { downloadIdleTimeout = prev })
}

func TestDownloadTarballCompletesASlowBodyThatKeepsMoving(t *testing.T) {
	withDownloadIdleTimeout(t, 150*time.Millisecond)
	tarball := actionTarball(t)
	// The whole body takes longer than the idle bound; no single gap does.
	server := dribblingServer(t, tarball, 256, 20*time.Millisecond, 0, 0)
	defer server.Close()

	engine := NewEngine(Options{HTTPClient: server.Client()})
	dest := t.TempDir()
	start := time.Now()
	if err := engine.downloadAndExtractTarball(context.Background(), server.URL+"/tarball/v1", "", dest); err != nil {
		t.Fatalf("download error = %v after %s; a moving body is never cut off", err, time.Since(start))
	}
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond {
		t.Fatalf("the body arrived in %s; the test meant to outlast the idle bound", elapsed)
	}
	if _, err := os.Stat(filepath.Join(dest, "action.yml")); err != nil {
		t.Fatalf("action.yml not extracted: %v", err)
	}
	if info, err := os.Stat(filepath.Join(dest, "dist", "index.js")); err != nil || info.Size() != 6*1024 {
		t.Fatalf("dist/index.js = %v, %v", info, err)
	}
}

func TestDownloadTarballFailsWhenTheBodyStalls(t *testing.T) {
	withDownloadIdleTimeout(t, 150*time.Millisecond)
	tarball := actionTarball(t)
	server := dribblingServer(t, tarball, 256, 0, 1024, 600*time.Millisecond)
	defer server.Close()

	engine := NewEngine(Options{HTTPClient: server.Client()})
	err := engine.downloadAndExtractTarball(context.Background(), server.URL+"/tarball/v1", "", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "no data received for") {
		t.Fatalf("download error = %v, want the stall named", err)
	}
}

// refServer stands in for the GitHub API for one repository, o/r:
// /repos/o/r/commits/<ref> answers the configured SHA (or fails on demand),
// /repos/o/r/tarball/<sha> the tarball, and any other tarball path 404, so a
// download by the ref as written is caught.
type refServer struct {
	*httptest.Server
	sha      string
	tarball  []byte
	resolves atomic.Int32
	fail     atomic.Bool
	mu       sync.Mutex
	tarballs []string
}

func newRefServer(t *testing.T, sha string, tarball []byte) *refServer {
	t.Helper()
	rs := &refServer{sha: sha, tarball: tarball}
	rs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/repos/o/r/commits/"):
			rs.resolves.Add(1)
			if rs.fail.Load() {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte(`{"sha":"` + rs.sha + `"}`))
		case strings.HasPrefix(r.URL.Path, "/repos/o/r/tarball/"):
			ref := strings.TrimPrefix(r.URL.Path, "/repos/o/r/tarball/")
			rs.mu.Lock()
			rs.tarballs = append(rs.tarballs, ref)
			rs.mu.Unlock()
			if ref != rs.sha {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write(rs.tarball)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(rs.Close)
	return rs
}

func (rs *refServer) tarballRefs() []string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return append([]string(nil), rs.tarballs...)
}

const testSHA = "0123456789abcdef0123456789abcdef01234567"

func mustRef(t *testing.T, raw string) ActionReference {
	t.Helper()
	ref, err := ParseActionReference(raw)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func markActionReady(t *testing.T, cacheDir, sha string) {
	t.Helper()
	dir := filepath.Join(cacheDir, "o", "r", sha)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".ready"), []byte(sha), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeRefFile(t *testing.T, cacheDir, slug, sha string, resolvedAt time.Time) {
	t.Helper()
	path := filepath.Join(cacheDir, "o", "r", "refs", slug)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(fmt.Sprintf("%s %d\n", sha, resolvedAt.Unix())), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFetchRemoteActionDownloadsByTheResolvedSHA(t *testing.T) {
	server := newRefServer(t, testSHA, actionTarball(t))
	cacheDir := t.TempDir()
	engine := NewEngine(Options{CacheDir: cacheDir, HTTPClient: server.Client()})

	resolved, err := engine.fetchRemoteAction(context.Background(), server.URL, "", mustRef(t, "o/r@v1"))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ResolvedRef != testSHA {
		t.Fatalf("resolved ref = %q", resolved.ResolvedRef)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "o", "r", testSHA, ".ready")); err != nil {
		t.Fatalf("the action is cached under its SHA: %v", err)
	}
	if refs := server.tarballRefs(); len(refs) != 1 || refs[0] != testSHA {
		t.Fatalf("tarball fetched by %v, want by the resolved SHA only", refs)
	}
	data, err := os.ReadFile(filepath.Join(cacheDir, "o", "r", "refs", "v1"))
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 || fields[0] != testSHA {
		t.Fatalf("ref entry = %q, want the SHA and the time it was resolved", data)
	}
}

func TestResolveRemoteRefTrustsTheDiskEntryWithinItsTTL(t *testing.T) {
	server := newRefServer(t, testSHA, actionTarball(t))
	cacheDir := t.TempDir()
	first := NewEngine(Options{CacheDir: cacheDir, HTTPClient: server.Client()})
	if _, err := first.fetchRemoteAction(context.Background(), server.URL, "", mustRef(t, "o/r@v1")); err != nil {
		t.Fatal(err)
	}
	if got := server.resolves.Load(); got != 1 {
		t.Fatalf("resolves after the first fetch = %d", got)
	}

	// A new process: nothing in memory, the entry on disk is fresh.
	second := NewEngine(Options{CacheDir: cacheDir, HTTPClient: server.Client()})
	sha, err := second.resolveRemoteRef(context.Background(), server.URL, "", mustRef(t, "o/r@v1"))
	if err != nil || sha != testSHA {
		t.Fatalf("resolve = %q, %v", sha, err)
	}
	if got := server.resolves.Load(); got != 1 {
		t.Fatalf("a fresh entry is trusted without the API: resolves = %d", got)
	}
}

func TestResolveRemoteRefResolvesAgainPastTheTTL(t *testing.T) {
	server := newRefServer(t, testSHA, nil)
	cacheDir := t.TempDir()
	markActionReady(t, cacheDir, testSHA)
	writeRefFile(t, cacheDir, "main", testSHA, time.Now().Add(-2*time.Hour))

	engine := NewEngine(Options{CacheDir: cacheDir, HTTPClient: server.Client()})
	sha, err := engine.resolveRemoteRef(context.Background(), server.URL, "", mustRef(t, "o/r@main"))
	if err != nil || sha != testSHA {
		t.Fatalf("resolve = %q, %v", sha, err)
	}
	if got := server.resolves.Load(); got != 1 {
		t.Fatalf("a branch entry two hours old is resolved again: resolves = %d", got)
	}
	data, _ := os.ReadFile(filepath.Join(cacheDir, "o", "r", "refs", "main"))
	fields := strings.Fields(string(data))
	if len(fields) != 2 {
		t.Fatalf("ref entry = %q", data)
	}
	if at, _ := strconv.ParseInt(fields[1], 10, 64); time.Since(time.Unix(at, 0)) > time.Minute {
		t.Fatalf("the entry's time was not refreshed: %s", data)
	}

	// The same entry, a tag: two hours is within a tag's day.
	writeRefFile(t, cacheDir, "v1", testSHA, time.Now().Add(-2*time.Hour))
	if _, err := engine.resolveRemoteRef(context.Background(), server.URL, "", mustRef(t, "o/r@v1")); err != nil {
		t.Fatal(err)
	}
	if got := server.resolves.Load(); got != 1 {
		t.Fatalf("a tag entry two hours old is trusted: resolves = %d", got)
	}
}

func TestResolveRemoteRefKeepsAStaleEntryWhenTheAPIFails(t *testing.T) {
	server := newRefServer(t, testSHA, nil)
	server.fail.Store(true)
	cacheDir := t.TempDir()
	markActionReady(t, cacheDir, testSHA)
	writeRefFile(t, cacheDir, "main", testSHA, time.Now().Add(-3*time.Hour))

	var warned []string
	engine := NewEngine(Options{CacheDir: cacheDir, HTTPClient: server.Client(), Warn: func(m string) { warned = append(warned, m) }})
	sha, err := engine.resolveRemoteRef(context.Background(), server.URL, "", mustRef(t, "o/r@main"))
	if err != nil || sha != testSHA {
		t.Fatalf("resolve = %q, %v; the entry on disk still serves when the API fails", sha, err)
	}
	if len(warned) != 1 || !strings.Contains(warned[0], "could not resolve o/r@main") {
		t.Fatalf("warnings = %q", warned)
	}

	// Without an entry on disk the failure is the caller's.
	if _, err := engine.resolveRemoteRef(context.Background(), server.URL, "", mustRef(t, "o/r@other")); err == nil {
		t.Fatal("a ref never resolved cannot be served from disk")
	}
}

func TestRefreshActionsResolvesAFreshEntryAgain(t *testing.T) {
	server := newRefServer(t, testSHA, nil)
	cacheDir := t.TempDir()
	markActionReady(t, cacheDir, testSHA)
	writeRefFile(t, cacheDir, "v1", testSHA, time.Now())

	engine := NewEngine(Options{CacheDir: cacheDir, HTTPClient: server.Client(), RefreshActions: true})
	if _, err := engine.resolveRemoteRef(context.Background(), server.URL, "", mustRef(t, "o/r@v1")); err != nil {
		t.Fatal(err)
	}
	if got := server.resolves.Load(); got != 1 {
		t.Fatalf("--refresh-actions resolves a fresh entry again: resolves = %d", got)
	}
}

func TestRefTTL(t *testing.T) {
	t.Parallel()
	for _, tag := range []string{"v4", "v4.1", "v4.1.0", "1.2.3", "v2.0.0-rc.1"} {
		if refTTL(tag) != refTagTTL {
			t.Errorf("%q: a version tag keeps for %s", tag, refTagTTL)
		}
	}
	for _, branch := range []string{"main", "master", "release", "feature/x", "latest"} {
		if refTTL(branch) != refBranchTTL {
			t.Errorf("%q: a branch keeps for %s", branch, refBranchTTL)
		}
	}
}
