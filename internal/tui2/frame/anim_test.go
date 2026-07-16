package frame

import "testing"

// TestSchedulerLifecycle walks the arm/tick/disarm cycle and pins the two
// invariants: no tick chain without animators, and exactly one chain ever.
func TestSchedulerLifecycle(t *testing.T) {
	s := NewScheduler()

	if s.Active() || s.Armed() {
		t.Fatal("fresh scheduler must be inert")
	}
	if cmd := s.Arm(); cmd != nil {
		t.Fatal("Arm with no animators must be nil — idle holds zero tickers")
	}

	s.Add("run")
	if cmd := s.Arm(); cmd == nil {
		t.Fatal("Arm with an animator must start the chain")
	}
	if !s.Armed() {
		t.Fatal("chain should be in flight")
	}
	if cmd := s.Arm(); cmd != nil {
		t.Fatal("second Arm while in flight must be nil — one chain only")
	}

	// Tick arrives while the animator lives: chain re-arms.
	if cmd := s.OnTick(); cmd == nil {
		t.Fatal("OnTick with animators must re-arm")
	}

	// Animator leaves; the in-flight tick lands and the chain ends.
	s.Remove("run")
	if cmd := s.OnTick(); cmd != nil {
		t.Fatal("OnTick with no animators must end the chain")
	}
	if s.Armed() {
		t.Fatal("chain must be down after final tick")
	}
}

func TestSchedulerMultipleAnimators(t *testing.T) {
	s := NewScheduler()
	s.Add("a")
	s.Add("b")
	_ = s.Arm()
	s.Remove("a")
	if cmd := s.OnTick(); cmd == nil {
		t.Fatal("chain must survive while any animator remains")
	}
	s.Remove("b")
	if cmd := s.OnTick(); cmd != nil {
		t.Fatal("chain must end when the last animator leaves")
	}
}

func TestMemo(t *testing.T) {
	var m Memo
	calls := 0
	render := func() string { calls++; return "out" }
	size := Size{Width: 3, Height: 1}

	m.Render("k1", size, render)
	m.Render("k1", size, render)
	if calls != 1 {
		t.Fatalf("same key+size must hit: %d renders", calls)
	}
	m.Render("k2", size, render)
	if calls != 2 {
		t.Fatalf("key change must miss: %d renders", calls)
	}
	m.Render("k2", Size{Width: 4, Height: 1}, render)
	if calls != 3 {
		t.Fatalf("size change must miss: %d renders", calls)
	}
	m.Invalidate()
	m.Render("k2", Size{Width: 4, Height: 1}, render)
	if calls != 4 {
		t.Fatalf("invalidate must force a render: %d renders", calls)
	}
	if hits, misses := m.Stats(); hits != 1 || misses != 4 {
		t.Fatalf("stats = %d/%d, want 1 hit / 4 misses", hits, misses)
	}
}
