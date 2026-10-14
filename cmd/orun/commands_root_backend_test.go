package main

import "testing"

// `orun status --remote-state` (and logs, tui) read through the coordination
// backend, which can long-poll the run's events: --watch is woken by the run
// instead of polling every second (specs/orun-sync-efficiency F5).
func TestNewRemoteBackend_CanLongPollRunEvents(t *testing.T) {
	t.Setenv("ORUN_TOKEN", "static-test-token")
	t.Setenv("ORUN_COORDINATION", "")
	b, err := newRemoteBackend("http://127.0.0.1:9")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := b.(runEventWaiter); !ok {
		t.Fatalf("%T cannot long-poll the run's events", b)
	}

	t.Setenv("ORUN_COORDINATION", "legacy")
	b, err = newRemoteBackend("http://127.0.0.1:9")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := b.(runEventWaiter); ok {
		t.Fatalf("the legacy escape hatch keeps the plain backend, got %T", b)
	}
}
