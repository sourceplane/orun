package gha

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
