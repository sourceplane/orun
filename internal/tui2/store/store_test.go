package store

import "testing"

func TestSliceLifecycle(t *testing.T) {
	s := New()
	runs := Define[[]string](s, "runs")

	if got := runs.Get(); got != nil {
		t.Fatalf("unset slice must be zero, got %v", got)
	}
	if runs.Rev() != 0 {
		t.Fatal("unset slice must be rev 0")
	}

	runs.Set([]string{"a"})
	r1 := runs.Rev()
	if r1 == 0 {
		t.Fatal("set must bump rev")
	}
	runs.Update(func(v []string) []string { return append(v, "b") })
	if runs.Rev() <= r1 {
		t.Fatal("update must bump rev")
	}
	if got := runs.Get(); len(got) != 2 || got[1] != "b" {
		t.Fatalf("got %v", got)
	}
}

// TestRevsGloballyMonotonic pins the property composite memo keys rely on:
// two different write histories can never produce the same key.
func TestRevsGloballyMonotonic(t *testing.T) {
	s := New()
	a := Define[int](s, "a")
	b := Define[int](s, "b")

	a.Set(1)
	b.Set(1)
	a.Set(2)
	if !(a.Rev() > b.Rev()) {
		t.Fatalf("later write must carry greater rev: a=%d b=%d", a.Rev(), b.Rev())
	}
	if a.Key() == b.Key() {
		t.Fatal("distinct revisions must render distinct keys")
	}
}
