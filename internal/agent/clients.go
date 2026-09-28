package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// clients.go — where each coding-agent client discovers SKILL.md files
// (saas-agent-skills SK4, design §5). The table is data with a test, not
// scattered string literals: these paths move as the clients settle, and
// when one does it changes here once. `--dir` always wins over it.

// SkillsClient names a coding-agent client `orun skills pull --client`
// accepts.
type SkillsClient string

const (
	ClientClaudeCode SkillsClient = "claude-code"
	ClientCodex      SkillsClient = "codex"
	ClientCursor     SkillsClient = "cursor"
	ClientVSCode     SkillsClient = "vscode"
)

// clientDirs maps a client to its user-level and project-level skills
// directories. User-level paths are relative to $HOME; project-level to
// the repository root.
var clientDirs = map[SkillsClient]struct{ user, project string }{
	ClientClaudeCode: {user: filepath.Join(".claude", "skills"), project: filepath.Join(".claude", "skills")},
	ClientCodex:      {user: filepath.Join(".codex", "skills"), project: filepath.Join(".codex", "skills")},
	ClientCursor:     {user: filepath.Join(".cursor", "skills"), project: filepath.Join(".cursor", "skills")},
	ClientVSCode:     {user: filepath.Join(".copilot", "skills"), project: filepath.Join(".github", "skills")},
}

// SkillsClients lists the accepted client names, for help text and errors.
func SkillsClients() []string {
	out := make([]string, 0, len(clientDirs))
	for c := range clientDirs {
		out = append(out, string(c))
	}
	sort.Strings(out)
	return out
}

// ClientSkillsDir resolves the directory a client discovers skills in:
// the user-level one by default, the project-level one (relative to the
// current directory) when project is set.
func ClientSkillsDir(client string, project bool) (string, error) {
	d, ok := clientDirs[SkillsClient(strings.ToLower(strings.TrimSpace(client)))]
	if !ok {
		return "", fmt.Errorf("unknown client %q (one of %s)", client, strings.Join(SkillsClients(), ", "))
	}
	if project {
		return d.project, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving the home directory for --client %s: %w", client, err)
	}
	return filepath.Join(home, d.user), nil
}
