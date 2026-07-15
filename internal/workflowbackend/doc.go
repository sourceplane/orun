// Package workflowbackend is the single, shared execution path for orun's
// `workflow:` vocabulary (specs/orun-workflows). It pins a torkflow workflow
// file and the torkflow engine by content digest, and invokes the engine as a
// subprocess over a JSON contract — the same process boundary torkflow already
// uses for its own providers (design §5).
//
// It is deliberately small and dependency-light: both surfaces that consume it —
// the `workflow:` plan step (Surface A) and the `workflow:` blueprint hook
// (Surface B) — share this one invocation, so there is no second engine-invocation
// implementation (invariant 2).
//
// The load-bearing law lives at the boundary of this package: callers pin only a
// workflow's reference + digest + declared inputs into durable state (plan.json /
// provenance.lock); the Result this package returns is the wire shape of a run,
// sealed into .orun/ by the caller and never promoted into a plan or lock
// (design §7).
package workflowbackend
