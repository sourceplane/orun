// Package scaffold is orun's unified scaffolding & instantiation engine.
//
// One engine, one language, two scales: scaffolding a single component and
// instantiating a whole repo are the same operation at different sizes —
// resolve typed inputs, resolve source(s), order a set of modules by
// dependency, place each module (render / copy / consume), gate the output,
// and record provenance. A single component is a Blueprint with one module; a
// repo is a Blueprint with many. Nothing in the grammar changes with scale —
// only the module count and which placement modes appear.
//
// The engine is a pure, sandboxed Go package: rendering is Go stdlib
// text/template with a constrained funcmap (no file/exec/net/time/rand). All
// ecosystem/SaaS/Cloudflare policy lives in the baseline's own blueprint.yaml
// and its declared hooks, never in this package (invariant 8, enforced by
// neutrality_test.go). See specs/orun-scaffolding/{README,design}.md.
package scaffold
