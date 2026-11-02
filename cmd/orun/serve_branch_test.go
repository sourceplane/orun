package main

import "testing"

// saas-session-continuity CT3: a session grounds on the platform's task
// branch when it names this task, so it starts where its predecessor pushed.
func TestSessionBranch(t *testing.T) {
	cases := []struct{ task, typ, platform, want string }{
		{"APP-1", "bootstrapper", "orun/APP-1-home-page", "orun/APP-1-home-page"},
		{"APP-1", "bootstrapper", "", "agent/APP-1-bootstrapper"},
		// Another task's branch, or one off the grammar, is not taken.
		{"APP-1", "bootstrapper", "orun/APP-2-other", "agent/APP-1-bootstrapper"},
		{"APP-1", "bootstrapper", "feature/x", "agent/APP-1-bootstrapper"},
		{"APP-1", "bootstrapper", "orun/APP-12-x", "agent/APP-1-bootstrapper"},
		{"", "bootstrapper", "orun/APP-1-home", ""},
	}
	for _, c := range cases {
		if got := sessionBranch(c.task, c.typ, c.platform); got != c.want {
			t.Errorf("sessionBranch(%q, %q, %q) = %q, want %q", c.task, c.typ, c.platform, got, c.want)
		}
	}
}
