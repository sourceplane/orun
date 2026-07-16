package shell

import "testing"

func reg() *Registry {
	r := NewRegistry()
	r.Register(Command{ID: "goto.home", Title: "Go to Home"})
	r.Register(Command{ID: "goto.agents", Title: "Go to Agents"})
	r.Register(Command{ID: "app.quit", Title: "Quit"})
	return r
}

func TestMatchEmptyReturnsAll(t *testing.T) {
	if got := len(reg().Match("")); got != 3 {
		t.Fatalf("got %d", got)
	}
}

func TestMatchSubstringOutranksSpread(t *testing.T) {
	m := reg().Match("agents")
	if len(m) == 0 || m[0].ID != "goto.agents" {
		t.Fatalf("got %v", m)
	}
}

func TestMatchSubsequence(t *testing.T) {
	m := reg().Match("gth") // g…t…h spread across "go to home"
	if len(m) == 0 || m[0].ID != "goto.home" {
		t.Fatalf("got %v", m)
	}
}

func TestMatchMiss(t *testing.T) {
	if m := reg().Match("zzz"); len(m) != 0 {
		t.Fatalf("got %v", m)
	}
}

func TestRegisterReplacesByID(t *testing.T) {
	r := reg()
	r.Register(Command{ID: "app.quit", Title: "Quit now"})
	if len(r.All()) != 3 {
		t.Fatal("replace must not grow the registry")
	}
	c, _ := r.Get("app.quit")
	if c.Title != "Quit now" {
		t.Fatal("replace must take effect")
	}
}

// stubProvider is a minimal CommandProvider for registration tests.
type stubProvider struct{ Surface }

func (stubProvider) ID() string    { return "stub" }
func (stubProvider) Title() string { return "Stub" }
func (stubProvider) Commands() []Command {
	return []Command{{ID: "stub.action", Title: "Stub action"}}
}

// TestSurfaceCommandsRegistered pins design §13.4's mechanism: surface
// capabilities become commands at construction, so palette and help are
// complete by construction.
func TestSurfaceCommandsRegistered(t *testing.T) {
	sh := New(Config{Surfaces: []Surface{stubProvider{}}})
	if _, ok := sh.Registry().Get("stub.action"); !ok {
		t.Fatal("surface command must register at construction")
	}
}
