// Package runworktree is the live, mutable half of the runner's working-tree /
// seal model (specs/orun-object-model runner-integration.md §1–§3). It is the
// native home for an execution's in-flight state — the analogue of git's index:
// a single, atomically-rewritten working file the runner mutates as jobs and
// steps progress, plus streamed step logs. When the run reaches a terminal
// status it is sealed into the immutable object graph via internal/execseal (the
// "commit"), at which point the working tree is dropped.
//
// This replaces what internal/state + the executionstate bridge faked via
// mirroring: there is one live representation (the working tree) and one
// finished representation (the sealed ExecutionRun objects), in the new schema,
// so sealing is a pure copy-to-objects with no translation.
//
// Layout, under <root>/run/<execId>/ (root is .orun/objectmodel):
//
//	run.json            the authoritative live snapshot (atomic temp+rename)
//	run.lock            crash sentinel: pid, started, lastHeartbeat, currentJob
//	logs/<folder>/<step>.log   streamed step output (becomes content blobs at seal)
//
// A refs/executions/live/<execId> handle marks the run in-flight and is the
// enumeration point for crash recovery. The lockfile's heartbeat age is the
// staleness signal; a stale tree is sealed on the next invocation (as its
// already-terminal status, or failed if it crashed mid-run). Partial objects
// written before a crash are unreachable and swept by GC.
//
// The package takes an injectable clock (it never calls the wall clock
// directly) and never imports the legacy internal/state module.
package runworktree
