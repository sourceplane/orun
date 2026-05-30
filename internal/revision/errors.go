package revision

import "errors"

// Package-local sentinels for the seven-branch resolver in resolver.go.
// These do NOT extend the internal/statestore sentinel set; they live here
// because they describe resolver-shaped failures that have no analogue at
// the byte-level persistence layer (state-store.md §4 stays leaf-clean).
//
// Callers MUST route through errors.Is — string matching is unsupported.

// ErrAmbiguousArg is returned by ResolveRevision when the caller-supplied
// argument matches none of the seven resolution branches in
// compatibility-and-migration.md §3 (i.e. branch 7, "otherwise"). The
// argument is included in the wrapping message so the CLI can echo it.
//
// M5 surfaces this verbatim from `orun run <arg>`; the implementer report
// for Task 0010 documents the intent (a typed sentinel rather than a free
// string lets the CLI offer a `Did you mean…` hint without re-parsing the
// error message).
var ErrAmbiguousArg = errors.New("revision: ambiguous or unknown run target")

// ErrComponentRunUnchanged is returned by ResolveRevision branch 6 when the
// argument matches a registered component name. Branch 6 is the seam M5
// rewires into the existing component-run path (cli-surface.md §1.4); in
// M3 the resolver does NOT invent a component lookup — it returns this
// sentinel paired with ResolveSourceComponent so the caller can dispatch
// to the legacy component-run code path unchanged.
//
// The wording "unchanged" reflects that branch 6 is intentionally a
// passthrough: the resolver has done its job by classifying the arg as a
// component reference; the actual component-run logic lives elsewhere.
var ErrComponentRunUnchanged = errors.New("revision: argument matches a component name; route through component-run path")
