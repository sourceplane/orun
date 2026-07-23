// Package agents embeds the shipped agent-type definitions so the bare
// binary can run them. A cloud sandbox runs only the released orun — no
// checkout, no agents/ directory — so without this, `orun agent serve
// --type implementer` finds nothing, boots type-less, and the deny-by-default
// tool policy bricks every tool call. An authored agents/<name>.md on disk
// still wins (agenttype.LoadNamed); the embedded copy is the fallback.
package agents

import "embed"

// FS carries the shipped agent-type definitions (agents/*.md).
//
//go:embed *.md
var FS embed.FS
