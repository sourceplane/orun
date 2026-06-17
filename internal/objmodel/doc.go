// Package objmodel is the unified read seam over the orun object model
// (specs/orun-object-model remote-and-consumers.md §4). It defines ModelReader —
// the single interface the TUI cockpit, the CLI porcelain, and the hosted Orun
// Cloud console all consume — and a Reader that satisfies it by composing the
// shipped per-layer readers (objread, objcatalog, objindex) over one
// ObjectStore + RefStore pair.
//
// The load-bearing property is substitutability: a ModelReader is backed by a
// local store or a remote store interchangeably, because both implement the same
// objectstore.ObjectStore / refstore.RefStore interfaces. "The console and the
// TUI share one read path" is therefore literally true — they hold the same
// Reader type, differing only in whether its stores point at .orun/ on disk or
// at a hosted object bucket + ref KV. Selection (source / head / branch / PR) is
// ref resolution, not a bespoke API.
package objmodel
