package agent

import (
	"path/filepath"
	"strings"
	"testing"
)

// SK4: the client table is the one place these paths live; a test pins
// each of them so a move is a deliberate, reviewed change.

func TestClientSkillsDir(t *testing.T) {
	t.Setenv("HOME", "/home/dana")
	cases := []struct {
		client  string
		project bool
		want    string
	}{
		{"claude-code", false, "/home/dana/.claude/skills"},
		{"claude-code", true, ".claude/skills"},
		{"codex", false, "/home/dana/.codex/skills"},
		{"cursor", false, "/home/dana/.cursor/skills"},
		{"vscode", false, "/home/dana/.copilot/skills"},
		{"vscode", true, filepath.Join(".github", "skills")},
		{"VSCode", true, filepath.Join(".github", "skills")}, // case-insensitive
	}
	for _, c := range cases {
		got, err := ClientSkillsDir(c.client, c.project)
		if err != nil {
			t.Fatalf("%s project=%v: %v", c.client, c.project, err)
		}
		if filepath.ToSlash(got) != filepath.ToSlash(c.want) {
			t.Errorf("%s project=%v = %q, want %q", c.client, c.project, got, c.want)
		}
	}
	if _, err := ClientSkillsDir("emacs", false); err == nil || !strings.Contains(err.Error(), "claude-code, codex, cursor, vscode") {
		t.Errorf("unknown client should name the accepted ones, got %v", err)
	}
}
