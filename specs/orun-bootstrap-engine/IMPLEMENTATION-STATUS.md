# orun-bootstrap-engine — Implementation status

As-built. Kept distinct from the design and the plan: this file records what
shipped and every place it departed from the spec.

| Milestone | Status | Landed |
|---|---|---|
| **BE-O1** | ✅ Shipped | the action mechanism + `orun.http/probe@v1` |
| BE-O2 | 🗓️ Planned | |
| BE-O3 | 🗓️ Planned | |
| BE-O4 | 🗓️ Planned | |
| BE-O5 | 🗓️ Planned | |
| BE-O6 | 🗓️ Planned | |
| BE-O7 | 🗓️ Planned | |
| BE-O8 | 🗓️ Planned | |

## BE-O1 — the action mechanism

### What shipped

- **`internal/actions`** — the closed, versioned registry. `Spec`/`Param` declare
  an action's contract; `Validate` checks a hook's `with:` against it;
  `Resolve` applies defaults and re-checks types after templates render; `Run`
  executes. Ids follow `<namespace>/<verb>@v<major>`, enforced by a pattern, and
  a duplicate or malformed id **panics at startup** rather than shipping a
  registry that lies about itself.
- **`Hook.Uses` + XOR validation.** A hook is now exactly one of `run` / `uses` /
  `workflow`. Two is ambiguous, zero does nothing, and both are far more likely
  to be an unfinished edit than an intention.
- **Parse-time validation with line numbers.** `ParseBlueprint` re-decodes the
  document into a `yaml.Node` (`actionhooks.go`) purely to locate the offending
  key, because *"action orun.http/probe@v1 has no parameter urlz"* is markedly
  less useful than the same sentence with the line it is on.
- **Hook outputs, scoped to the phase.** An action's outputs are readable by a
  later hook in the same phase as `{{ .hooks.<id>.outputs.<key> }}`, rendered
  through the existing constrained funcmap. **Not** across phases — a resumed
  run executes one phase in a container that never saw the others, so a
  cross-phase reference would be a promise the engine cannot keep.
- **The `ActionRunner` seam.** The engine calls an interface;
  `ScaffoldOptions.Actions` injects it, defaulting to the real registry. A test
  substitutes a recording runner and asserts the exact sequence of actions a
  phase performs, with parameters, with no network — which is also what the
  baselines' phase-simulation CI tier needs.
- **`orun.http/probe@v1`** — the first action: GET each URL, require a status.
  Does not follow redirects, reports **every** failing URL rather than the
  first, and refuses an empty list.

### Departure from the plan: which actions shipped first

**The plan said BE-O1 would ship `orun.pr/land@v1` and `orun.run/watch@v1`**,
on the stated grounds that *"neither needs anything new: the pen exists, and
`orun run --retry` exists."*

Reading the code showed that premise was half right. `internal/provenance` has
the pen's *primitives* — `BranchName`, `TaskTrailer`, `RenderManifest`,
`Verify` — but the **landing logic** (open, wait for checks, merge, return to a
pulled main) lives inside the cobra command in `cmd/orun/pr.go`, not in a
package an action can call. Same for the convergence watch. Making either action
real means first extracting that logic out of the command layer, which is a
genuine piece of work and not a detail of this milestone.

So BE-O1 shipped the **mechanism** plus `orun.http/probe@v1`, which needed
nothing extracted and is a real action a baseline's verify step uses.

Deliberately **not** done: registering `pr/land` and `run/watch` *specs* without
working implementations. A registry that advertises what it cannot run is
exactly the failure this epic exists to end — the platform offering a baseline
it cannot build. They arrive with their extraction, tracked as **BE-O1b**.

### BE-O1b — extract the command-layer logic (new, from the above)

Move PR landing and run watching out of `cmd/orun/{pr,run}.go` into packages an
action can call, then register `orun.pr/land@v1` and `orun.run/watch@v1`. The
CLI verbs become thin wrappers over the same code, so there is one
implementation and the command and the action cannot diverge.

### Verification

`go build ./...`, `go vet ./...` and `go test ./...` all green. New tests:
`internal/actions/{actions,http_probe}_test.go` and
`internal/scaffold/action_hook_test.go` — covering the registry's shape, unknown
and misspelled parameters, type mismatches, templated values deferred to
runtime, defaults, the line-numbered parse failure, cross-phase output
isolation, and the three-way hook exclusivity.
