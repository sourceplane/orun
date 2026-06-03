// Package objread is the native read layer over the orun-object-model graph
// (specs/orun-object-model runner-integration.md §4 rows 16-18). It reconstructs
// execution detail — header, jobs, attempts, steps, and logs — from the
// content-addressed objects + refs, and from the live working tree for an
// in-flight run, returning presentation-neutral views.
//
// It is the object-model replacement for the legacy state.* reads that the read
// commands, runbundle, the TUI services, and the cockpit view-model perform.
// Those consumers move onto these views (with the legacy path kept behind the
// flag) so internal/state can eventually be deleted. This package never imports
// internal/state and takes no clock (reads are timeless).
package objread
