# orun-bootstrap-engine — Implementation status

As-built. Kept distinct from the design and the plan: this file records what
shipped and every place it departed from the spec.

| Milestone | Status | Landed |
|---|---|---|
| **BE-O1** | ✅ Shipped | the action mechanism + `orun.http/probe@v1` |
| **BE-O1b** | ✅ Shipped | `Pen.Land`, `orun.pr/land@v1`, `orun pr land` |
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

Reading the code showed that premise was half right — and **BE-O1's own account
of why was itself partly wrong; corrected here in BE-O1b.**

What BE-O1 said: *"the landing logic lives inside the cobra command in
`cmd/orun/pr.go`, not in a package an action can call."* That is **false for the
open half.** `provenance.Pen.Open()` is a fully callable package API, and
`cmd/orun/pr.go` was already a thin wrapper over it. Nothing needed extracting.

What was actually missing was only the **tail**: wait for checks, merge, return
to a pulled base. `Pen` had `Open` and nothing after it. So BE-O1b is smaller
than BE-O1 predicted — an addition to an existing type, not an extraction from
the command layer.

So BE-O1 shipped the **mechanism** plus `orun.http/probe@v1`, which needed
nothing extracted and is a real action a baseline's verify step uses.

Deliberately **not** done: registering `pr/land` and `run/watch` *specs* without
working implementations. A registry that advertises what it cannot run is
exactly the failure this epic exists to end — the platform offering a baseline
it cannot build. They arrive with their extraction, tracked as **BE-O1b**.

## BE-O1b — the landing tail, and `orun.pr/land@v1`

### What shipped

- **`provenance.Pen.Land`** — the gesture `Open` starts and nothing finished:
  poll the PR's checks until every run has a **conclusion**, merge **pinned to
  the commit those checks ran on**, and return the tree to a pulled base.
  Three behaviours carry the weight:
  - **No checks is a pass, not a wait.** A bootstrap's first landing creates the
    repository and its CI together, so at merge time there is nothing to wait
    for. Hanging there is the most common way an unattended run stalls.
  - **Queued is not passing.** Polling stops when every run has concluded, not
    when none has failed yet.
  - **A refusal carries GitHub's own message.** "405" is not something an
    operator can act on; "Pull Request is not mergeable" is.
  `neutral` and `skipped` count as passing — a skipped matrix leg is the normal
  shape of a conditional CI, and treating it as red would block every landing.
- **`orun.pr/land@v1`** — opens through the pen and lands. A draft is opened and
  deliberately **not** merged. No ambient credential is an **error**, not a
  quiet success: the pen's "branch pushed, open it here" is honest for a human
  at a terminal and useless to an unattended bootstrap, which cannot click it.
- **`orun pr land`** — the CLI verb over the same `Pen.Land`, so the command and
  the action cannot describe a landing differently.

### Still open

`orun.run/watch@v1` is **not** in this milestone. Watching a convergence is a
different subject from landing a PR — it reads workflow runs, not pull requests
— and folding it in here would have made one change do two things. It moves to
**BE-O5** with the rest of the action set.

### Verification

`go build ./...`, `go vet ./...`, `go test ./...` green.
`internal/provenance/land_test.go` drives a fake GitHub and a fake git: no
checks, queued-then-green, still-running at timeout, a failed check named,
neutral/skipped passing, GitHub's refusal surfaced, the return to a pulled base,
the merge pinned to the checked SHA, an unknown merge method, and an anonymous
landing refused.

### Verification

`go build ./...`, `go vet ./...` and `go test ./...` all green. New tests:
`internal/actions/{actions,http_probe}_test.go` and
`internal/scaffold/action_hook_test.go` — covering the registry's shape, unknown
and misspelled parameters, type mismatches, templated values deferred to
runtime, defaults, the line-numbered parse failure, cross-phase output
isolation, and the three-way hook exclusivity.
