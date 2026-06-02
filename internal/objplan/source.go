// Package objplan adapts the existing resolver outputs (internal/sourcectx,
// internal/catalogresolve / internal/catalogmodel) into the object-model node
// types and drives the tolerant-strict write walk via internal/nodewriter. It
// is the integration seam that lets `orun plan` write the content-addressed
// object graph without the resolver or the writer knowing about each other.
//
// Nothing here replaces the legacy plan path; the CLI calls into objplan
// additively (flag-gated) until the M12 cutover.
package objplan

import (
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/sourcectx"
)

// BuildSourceNode maps a resolved sourcectx.WorkspaceState to a
// nodes.SourceSnapshot. It collapses the resolver's fine-grained scope
// vocabulary to the node model's coarse {main,branch,pr,local-nogit} and honors
// the degenerate local-nogit case so the tolerant-strict walk always has a
// valid source terminal.
func BuildSourceNode(ws sourcectx.WorkspaceState, humanKey string) nodes.SourceSnapshot {
	src := nodes.SourceSnapshot{
		Kind:         nodes.KindSourceSnapshot,
		HumanKey:     humanKey,
		Scope:        mapScope(ws),
		Repo:         ws.Repo,
		HeadRevision: ws.HeadRevision,
		TreeHash:     ws.TreeHash,
		Branch:       ws.Branch,
		DirtyHash:    ws.DirtyHash,
	}
	if ws.PRNumber > 0 {
		src.PR = itoa(ws.PRNumber)
	}
	if ws.Dirty {
		src.WorkingTree = "dirty"
	} else if ws.HeadRevision != "" {
		src.WorkingTree = "clean"
	}
	return src
}

// mapScope reduces the sourcectx scope vocabulary to the node model's four
// values. Anything that is neither main, a PR, nor a no-git workspace is a
// "branch" (feature/protected/tag/dirty/ci all resolve operationally to a
// branch-scoped source for the object graph).
func mapScope(ws sourcectx.WorkspaceState) string {
	switch ws.Scope() {
	case "local-nogit":
		return nodes.ScopeLocalNoGit
	case "pr":
		return nodes.ScopePR
	case "branch-main":
		return nodes.ScopeMain
	default:
		return nodes.ScopeBranch
	}
}

// itoa is a tiny dependency-free int→string for the PR number.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
