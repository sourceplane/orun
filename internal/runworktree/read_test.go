package runworktree

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/nodes"
)

func TestListLiveEmpty(t *testing.T) {
	h := newHarness(t)
	got, err := ListLive(h.root)
	if err != nil {
		t.Fatalf("list live: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no live trees, got %d", len(got))
	}
}

func TestLoadLiveAbsent(t *testing.T) {
	h := newHarness(t)
	if _, ok, err := LoadLive(h.root, "nope"); err != nil || ok {
		t.Fatalf("LoadLive absent = ok %v, err %v", ok, err)
	}
}

func TestListAndLoadLive(t *testing.T) {
	h := newHarness(t)
	a := h.open(t, "exec_a")
	_ = a.StartJob("j")
	h.clk.advance(time.Minute)
	b := h.open(t, "exec_b")
	_ = b.StartJob("k")

	got, err := ListLive(h.root)
	if err != nil {
		t.Fatalf("list live: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 live trees, got %d", len(got))
	}
	// Newest (exec_b) first.
	if got[0].ExecutionID != "exec_b" || got[1].ExecutionID != "exec_a" {
		t.Fatalf("live order wrong: %s, %s", got[0].ExecutionID, got[1].ExecutionID)
	}

	snap, ok, err := LoadLive(h.root, "exec_a")
	if err != nil || !ok {
		t.Fatalf("LoadLive present = ok %v err %v", ok, err)
	}
	if snap.ExecutionID != "exec_a" || len(snap.Jobs) != 1 {
		t.Fatalf("loaded snapshot wrong: %+v", snap)
	}
	// LogPath is rooted under the working tree's logs dir.
	lp := snap.LogPath(h.root, snap.Jobs[0].Folder, "build")
	if filepath.Dir(filepath.Dir(lp)) != filepath.Join(LiveRoot(h.root), "exec_a", logsDir) {
		t.Fatalf("LogPath shape wrong: %s", lp)
	}
}

func TestListLiveSkipsGarbled(t *testing.T) {
	h := newHarness(t)
	_ = h.open(t, "exec_ok")
	// A junk dir without a run.json must be skipped.
	if err := os.MkdirAll(filepath.Join(LiveRoot(h.root), "junk"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	got, err := ListLive(h.root)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].ExecutionID != "exec_ok" {
		t.Fatalf("garbled tree not skipped: %+v", got)
	}
}

// liveSnapshotSurvivesSeal documents that LoadLive returns false once sealed.
func TestLoadLiveAfterSeal(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	wt := h.open(t, "exec_seal_read")
	_ = wt.StartJob("j")
	_ = wt.FinishJob("j", nodes.StatusSucceeded, "")
	if _, err := wt.Seal(ctx, nodes.StatusSucceeded, time.Time{}); err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, ok, _ := LoadLive(h.root, "exec_seal_read"); ok {
		t.Fatalf("live snapshot should be gone after seal")
	}
}
