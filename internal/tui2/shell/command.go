package shell

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Command is one user-visible action. Every capability in the cockpit is a
// registered command: the palette lists them, help is generated from them,
// keybindings are shortcuts into them, and tests invoke them directly
// (design §13.4 — palette and help are never hand-maintained).
type Command struct {
	// ID is stable and dot-namespaced: "goto.agents", "app.quit".
	ID string
	// Title is what the palette and help show.
	Title string
	// Keys are the bound shortcuts, for display ("1", "ctrl+k").
	Keys []string
	// Run produces the command's effect. It must not mutate shell state
	// directly; shell-owned effects are requested via messages (GotoMsg,
	// OpenHelpMsg, …) that the shell folds on the next Update.
	Run func() tea.Cmd
}

// Registry is the command bus. Registration order is preserved for help;
// matching is fuzzy-subsequence for the palette.
type Registry struct {
	cmds []Command
	byID map[string]Command
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{byID: make(map[string]Command)} }

// Register adds a command; a duplicate ID replaces the earlier entry.
func (r *Registry) Register(c Command) {
	if _, dup := r.byID[c.ID]; dup {
		for i := range r.cmds {
			if r.cmds[i].ID == c.ID {
				r.cmds[i] = c
				break
			}
		}
	} else {
		r.cmds = append(r.cmds, c)
	}
	r.byID[c.ID] = c
}

// Get looks a command up by ID.
func (r *Registry) Get(id string) (Command, bool) {
	c, ok := r.byID[id]
	return c, ok
}

// All returns the commands in registration order.
func (r *Registry) All() []Command { return r.cmds }

// Match returns commands whose ID or title fuzzily contains query, best
// matches first. An empty query returns everything.
func (r *Registry) Match(query string) []Command {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		out := make([]Command, len(r.cmds))
		copy(out, r.cmds)
		return out
	}
	type scored struct {
		c     Command
		score int
	}
	var hits []scored
	for _, c := range r.cmds {
		s := bestFuzzyScore(query, strings.ToLower(c.Title), strings.ToLower(c.ID))
		if s >= 0 {
			hits = append(hits, scored{c, s})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score < hits[j].score })
	out := make([]Command, len(hits))
	for i, h := range hits {
		out[i] = h.c
	}
	return out
}

// bestFuzzyScore returns the best (lowest) subsequence spread of query in
// any of the candidates, or -1 when none matches.
func bestFuzzyScore(query string, candidates ...string) int {
	best := -1
	for _, cand := range candidates {
		if s := fuzzyScore(query, cand); s >= 0 && (best < 0 || s < best) {
			best = s
		}
	}
	return best
}

// fuzzyScore reports whether query is a subsequence of s, scored by how far
// the match spreads (tighter is better). Exact substring matches score 0.
func fuzzyScore(query, s string) int {
	if idx := strings.Index(s, query); idx >= 0 {
		return 0
	}
	start := -1
	pos := 0
	for _, qr := range query {
		found := strings.IndexRune(s[pos:], qr)
		if found < 0 {
			return -1
		}
		if start < 0 {
			start = pos + found
		}
		pos += found + 1
	}
	return pos - start - len(query) + 1
}
