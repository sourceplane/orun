package statebackend

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestFileLock_AcquireRelease(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	lk := NewFileLock(filepath.Join(dir, ".lock"))

	ctx := context.Background()
	if err := lk.Lock(ctx); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	if err := lk.Unlock(); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
}

func TestFileLock_TryLock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, ".lock")

	lk1 := NewFileLock(path)
	ok, err := lk1.TryLock()
	if err != nil {
		t.Fatalf("TryLock: %v", err)
	}
	if !ok {
		t.Fatal("expected TryLock to succeed on uncontended lock")
	}

	lk2 := NewFileLock(path)
	ok2, err := lk2.TryLock()
	if err != nil {
		t.Fatalf("TryLock second: %v", err)
	}
	if ok2 {
		t.Fatal("expected TryLock to fail while lock is held")
	}

	lk1.Unlock()

	lk3 := NewFileLock(path)
	ok3, err := lk3.TryLock()
	if err != nil {
		t.Fatalf("TryLock after release: %v", err)
	}
	if !ok3 {
		t.Fatal("expected TryLock to succeed after release")
	}
	lk3.Unlock()
}

func TestFileLock_Contention(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, ".lock")

	lk1 := NewFileLock(path)
	ctx := context.Background()
	if err := lk1.Lock(ctx); err != nil {
		t.Fatalf("Lock: %v", err)
	}

	acquired := make(chan struct{})
	go func() {
		lk2 := NewFileLock(path)
		if err := lk2.Lock(ctx); err != nil {
			t.Errorf("Lock goroutine: %v", err)
			return
		}
		close(acquired)
		lk2.Unlock()
	}()

	select {
	case <-acquired:
		t.Fatal("second lock acquired while first still held")
	case <-time.After(200 * time.Millisecond):
	}

	lk1.Unlock()

	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("second lock did not acquire after release")
	}
}

func TestFileLock_ContextCancellation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, ".lock")

	lk1 := NewFileLock(path)
	ctx := context.Background()
	if err := lk1.Lock(ctx); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	defer lk1.Unlock()

	ctx2, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	lk2 := NewFileLock(path)
	err := lk2.Lock(ctx2)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if !contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFileLock_ConcurrentLockers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, ".lock")

	const N = 10
	var mu sync.Mutex
	var count int
	var maxConcurrent int

	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			lk := NewFileLock(path)
			if err := lk.Lock(context.Background()); err != nil {
				t.Errorf("Lock: %v", err)
				return
			}
			mu.Lock()
			count++
			if count > maxConcurrent {
				maxConcurrent = count
			}
			mu.Unlock()

			time.Sleep(10 * time.Millisecond)

			mu.Lock()
			count--
			mu.Unlock()
			lk.Unlock()
		}()
	}
	wg.Wait()

	if maxConcurrent > 1 {
		t.Fatalf("expected exclusive access, but saw %d concurrent holders", maxConcurrent)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsAt(s, sub))
}

func containsAt(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
