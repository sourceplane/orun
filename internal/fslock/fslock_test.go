package fslock

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestTryAcquireIsExclusiveUntilReleased(t *testing.T) {
	path := filepath.Join(t.TempDir(), "slots", "a.lock")
	first, ok, err := TryAcquire(path)
	if err != nil || !ok {
		t.Fatalf("first TryAcquire = %v, %v", ok, err)
	}
	// A second open of the same file in this process contends like another
	// process would: the lock belongs to the open file description.
	if _, ok, err := TryAcquire(path); err != nil || ok {
		t.Fatalf("second TryAcquire while held = %v, %v; want not acquired", ok, err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	second, ok, err := TryAcquire(path)
	if err != nil || !ok {
		t.Fatalf("TryAcquire after release = %v, %v", ok, err)
	}
	_ = second.Release()
}

func TestAcquireWaitsForTheHolderAndHonoursItsContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.lock")
	holder, ok, err := TryAcquire(path)
	if err != nil || !ok {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := Acquire(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Acquire while held = %v, want the context's deadline", err)
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = holder.Release()
	}()
	start := time.Now()
	waited, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer waited.Release()
	if time.Since(start) < 80*time.Millisecond {
		t.Fatalf("Acquire returned in %s, before the holder released", time.Since(start))
	}
	if (*Lock)(nil).Release() != nil {
		t.Fatal("a nil lock releases nothing")
	}
}
