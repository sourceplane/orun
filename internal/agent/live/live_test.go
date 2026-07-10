package live

import (
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestWriteGetListRemove(t *testing.T) {
	dir := t.TempDir()
	e := Entry{SessionID: "as_live1", PID: os.Getpid(), Socket: "/tmp/x.sock",
		State: "running", AgentType: "implementer", Task: "ORN-1", StartedAt: time.Now()}
	if err := Write(dir, e); err != nil {
		t.Fatal(err)
	}
	got, err := Get(dir, "as_live1")
	if err != nil || got.PID != os.Getpid() || got.State != "running" {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if err := UpdateState(dir, "as_live1", "completed"); err != nil {
		t.Fatal(err)
	}
	got, _ = Get(dir, "as_live1")
	if got.State != "completed" {
		t.Fatalf("state = %s", got.State)
	}
	list, err := List(dir)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	Remove(dir, "as_live1")
	if _, err := Get(dir, "as_live1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after remove = %v", err)
	}
}

func TestListSweepsDeadBodies(t *testing.T) {
	dir := t.TempDir()
	// A real short-lived process that is certainly dead by the time we list.
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Skip("cannot start helper process")
	}
	deadPID := cmd.Process.Pid
	_ = cmd.Wait()

	if err := Write(dir, Entry{SessionID: "as_dead", PID: deadPID, Socket: "x", State: "running", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := Write(dir, Entry{SessionID: "as_alive", PID: os.Getpid(), Socket: "x", State: "running", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	list, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].SessionID != "as_alive" {
		t.Fatalf("list = %+v", list)
	}
	// The dead entry was swept from disk, not just filtered.
	if _, err := Get(dir, "as_dead"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dead entry not swept: %v", err)
	}
	if _, err := Resolve(dir, "as_alive"); err != nil {
		t.Fatalf("resolve alive: %v", err)
	}
}

func TestWriteRejectsBadID(t *testing.T) {
	if err := Write(t.TempDir(), Entry{SessionID: "nope"}); err == nil {
		t.Fatal("bad id accepted")
	}
}

func TestListEmptyDirIsFine(t *testing.T) {
	list, err := List(t.TempDir() + "/missing")
	if err != nil || list != nil {
		t.Fatalf("list = %v, %v", list, err)
	}
}
