package catalogstore

import (
	"fmt"
	"strings"
)

// selector.go is the C5 PR-1 ref-selector parser seam. The CLI accepts a
// `--catalog-source` / `--source` string and an optional
// `--catalog-snapshot` pin, and every `orun catalog *` subcommand turns
// those two strings into a single RefSelector before calling a Resolver
// method. This parser is the one shared helper for that mapping so the
// grammar lives in exactly one tested place (task-0036 Integration Note 3:
// "Do not bury this logic untested inside the cobra RunE").
//
// Grammar (cli-surface.md §1):
//
//	current                 → {Kind: "current"}
//	main                    → {Kind: "main"}
//	latest                  → {Kind: "latest"}
//	branches/<name>         → {Kind: "branch", Branch: <name>}
//	prs/<n>                 → {Kind: "pr",     PR: <n>}
//	pr-<n>                  → {Kind: "pr",     PR: <n>}   (CLI convenience alias)
//	cat-<key>               → {Snapshot: "cat-<key>"}     (explicit snapshot pin)
//
// An explicit snapshot pin (`--catalog-snapshot <key>`) always wins over a
// source selector: when snapshot is non-empty ParseRefSelector returns a
// pin selector and ignores the source string. An empty source string with
// no snapshot pin defaults to the `current` ref.

// ErrInvalidSelector is returned by ParseRefSelector for a malformed
// selector string. Callers map this to the CLI's "invalid argument" exit.
type ErrInvalidSelector struct {
	Input  string
	Reason string
}

func (e *ErrInvalidSelector) Error() string {
	return fmt.Sprintf("catalogstore: invalid catalog selector %q: %s", e.Input, e.Reason)
}

// catSnapshotPrefix is the prefix every explicit catalogSnapshotKey carries
// (identity-and-keys.md §2). A bare source selector that happens to start
// with this prefix is treated as a snapshot pin.
const catSnapshotPrefix = "cat-"

// ParseRefSelector maps the CLI's `--catalog-source` (source) and
// `--catalog-snapshot` (snapshot) strings into a single RefSelector.
//
// Precedence:
//  1. snapshot non-empty → {Snapshot: snapshot} (source ignored).
//  2. source empty       → {Kind: "current"} (default ref).
//  3. otherwise parse source per the grammar above.
//
// Returns *ErrInvalidSelector for a malformed source string (empty path
// segment after branches/ or prs/, unknown bare keyword, non-cat-* token).
func ParseRefSelector(source, snapshot string) (RefSelector, error) {
	if snapshot != "" {
		snap := strings.TrimSpace(snapshot)
		if snap == "" {
			return RefSelector{}, &ErrInvalidSelector{Input: snapshot, Reason: "snapshot pin is whitespace-only"}
		}
		if !strings.HasPrefix(snap, catSnapshotPrefix) {
			return RefSelector{}, &ErrInvalidSelector{Input: snapshot, Reason: fmt.Sprintf("snapshot pin must start with %q", catSnapshotPrefix)}
		}
		return RefSelector{Snapshot: snap}, nil
	}

	src := strings.TrimSpace(source)
	if src == "" {
		return RefSelector{Kind: "current"}, nil
	}

	switch src {
	case "current":
		return RefSelector{Kind: "current"}, nil
	case "main":
		return RefSelector{Kind: "main"}, nil
	case "latest":
		return RefSelector{Kind: "latest"}, nil
	}

	if rest, ok := strings.CutPrefix(src, "branches/"); ok {
		if rest == "" || strings.Contains(rest, "/") {
			return RefSelector{}, &ErrInvalidSelector{Input: source, Reason: "branches/<name> requires a single non-empty branch segment"}
		}
		return RefSelector{Kind: "branch", Branch: rest}, nil
	}

	if rest, ok := strings.CutPrefix(src, "prs/"); ok {
		if rest == "" || strings.Contains(rest, "/") {
			return RefSelector{}, &ErrInvalidSelector{Input: source, Reason: "prs/<n> requires a single non-empty pr segment"}
		}
		return RefSelector{Kind: "pr", PR: rest}, nil
	}

	// CLI convenience alias: `pr-<n>` is accepted for `prs/<n>` so users
	// can type the same form the text output prints (e.g. "pr-139").
	if rest, ok := strings.CutPrefix(src, "pr-"); ok {
		if rest == "" || strings.Contains(rest, "/") {
			return RefSelector{}, &ErrInvalidSelector{Input: source, Reason: "pr-<n> requires a non-empty pr number"}
		}
		return RefSelector{Kind: "pr", PR: rest}, nil
	}

	// Explicit snapshot pin passed via the source flag.
	if strings.HasPrefix(src, catSnapshotPrefix) {
		return RefSelector{Snapshot: src}, nil
	}

	return RefSelector{}, &ErrInvalidSelector{
		Input:  source,
		Reason: "expected one of current|main|latest|branches/<name>|prs/<n>|cat-<key>",
	}
}
