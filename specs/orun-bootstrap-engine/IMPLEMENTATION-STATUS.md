# orun-bootstrap-engine — Implementation status

As-built. Kept distinct from the design and the plan: this file records what
shipped and every place it departed from the spec.

| Milestone | Status | Landed |
|---|---|---|
| **BE-O1** | ✅ Shipped | the action mechanism + `orun.http/probe@v1` |
| **BE-O1b** | ✅ Shipped | `Pen.Land`, `orun.pr/land@v1`, `orun pr land` |
| **BE-O2** | ✅ Shipped | `Derive`, `--status`, `--phase`/`--until`/`--resume`, input recovery |
| **BE-O3** | ✅ Shipped (lease deferred) | `when`, `requires.{phases,probe}`, `retry` |
| **BE-O4** | ✅ Shipped | `hooks.{pre,post,await}`, `pending`, the park, `.orun/run.state` |
| **BE-O5a** | ✅ Shipped | `task/ensure`, `task/rollup` — the task plane |
| **BE-O5b** | ✅ Shipped | `doctor/check`, `integrations/reconcile`, `secrets/exists` |
| **BE-O5c** | ✅ Shipped | `repo/ensure`, `run/watch` — **the action set is complete (9)** |
| **BE-O6** | ✅ Shipped | the event stream, narration, `--progress` |
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

## BE-O2 — derivation

### What shipped

- **`Derive`** renders the blueprint exactly as `Run` does — same inputs, same
  order, same bytes — and compares against the tree. It writes nothing. Both go
  through one `buildPlan`, because if "is this phase done?" were answered by a
  different renderer than the one that placed it, the answer would mean nothing.
- **Four states, not two.** `done` / `pending` / `partial` / `drifted`.
  `partial` is an interrupted run and re-running completes it; `drifted` is a
  file someone edited or a blueprint that moved, and is **reported rather than
  silently overwritten**.
- **`--status`** (and `--status --json`) — the read a different session performs
  to answer "where did this product get to?".
- **`--phase` / `--until` / `--resume`**, mutually exclusive, with an unknown
  phase naming the ones that exist.
- **Input recovery.** A resumed run reads what the product was built with from
  its own lock, before prompting, so a fresh container with no values file asks
  nothing. A flag that **disagrees** with the record is refused naming both
  values — not applied. Half a tree rendered with one value and half with
  another is a silent, expensive failure, and changing an input after the fact
  is `upgrade`'s operation.
- **Partial runs merge the module record**, so `--phase 05-edge` does not write
  a lock naming five files and lose everything phases 01–04 placed.

### The rule this milestone exists to make real

**Phase state is derived; a stored file is a cache and must be safe to delete.**
`TestDeriveSurvivesDeletingTheLocalArtifacts` deletes the entire `.orun`
directory and asserts the answer is unchanged. That is the property the paced
bootstrap already depends on — `.orun/*` is gitignored in every product, so the
records the flows archive have never survived a container — and it is now
enforced rather than assumed.

### A bug the tests caught

The first implementation returned `nil` from phase selection both when no
selection was asked for **and** when `--resume` selected zero phases. A finished
product therefore re-placed its entire tree on `--resume`. Selection now reports
*whether a selection was asked for* separately from *what it selected*.

### Narrowing is about writing, not computing

The whole blueprint is always rendered even when one phase is selected: collision
detection and the output gate are only meaningful against the complete set — a
phase that collides with one it was not asked to place is still a collision.

### Verification

`go build ./...`, `go vet ./...`, `go test ./...` green. `status_test.go` covers
the empty tree, the full run, survival of a deleted `.orun`, each selector,
resume-is-a-no-op-when-done, drift, partial, two selectors at once refused, an
unknown phase, the merged module record, and all three input-recovery paths.

## BE-O3 — preconditions, conditions, retry

### What shipped

- **`when:`** — a CEL expression over `inputs`. CEL for the reason
  orun-workflows-v3 already settled: a condition must be side-effect-free and
  terminating, and *"whatever a template engine accepts"* is not a contract.
  Compiled at **parse time**, because a phase that silently never runs because
  its condition has a typo is the worse failure — nothing appears to be wrong.
  A condition that is not boolean is refused with what it must produce.
- **A fifth phase state, `skipped`.** Distinct from `pending`, and the
  distinction is load-bearing: pending is work outstanding, skipped is work not
  wanted, and calling both "pending" would report a finished product as
  unfinished forever.
- **`requires.phases`** — a placement check, answered by **deriving**, never by
  reading a record claiming a phase ran. A requirement may only point
  *backwards*; forward is unsatisfiable by construction and self-reference is a
  loop, so both are parse errors.
- **`requires.probe`** — a reality check, answered by running actions. It exists
  because placement cannot know whether what an earlier phase *deployed* is
  still there. A baseline carries this knowledge as prose today — *"this lane
  fails because phase 03 is incomplete; re-run phase 03"* — and prose cannot
  gate anything. A probe **must** be an action: it answers a question, it does
  not run a command.
- **`retry`** — governs a phase's **hooks**, not its placement. Placement is
  deterministic, so a second attempt renders exactly what the first did; hooks
  reach the network, which is where a transient failure actually lives. The
  whole hook list is retried rather than resuming mid-list, because resuming
  would require knowing which hooks are safe to skip — a claim only the hook
  could make. With no declared policy a hook runs **once**: a silent default
  retry would hide a real failure behind a delay.

### The narrower CEL environment, on purpose

A phase condition sees `inputs` and nothing else — not another phase's result.
A phase must be answerable alone, and a condition depending on what a previous
phase produced could not be evaluated by a resumed run in a container that never
ran it. That is BE-O2's derivation rule constraining BE-O3's surface.

### Deferred: the lease

The plan listed a per-product lease here. It is **not** in this milestone, and
on reflection it does not belong in the binary at all — `risks-and-open-questions.md`
open question 5 already leaned this way. A lease held in a container dies with
the container, which is the opposite of what a lease is for. It belongs in
orun-cloud, keyed (workspace, product repo); the binary asks for it and reports
the holder. Tracked as orun-cloud **BE-K4**, where the runner that would hold it
lives.

### Verification

`go build ./...`, `go vet ./...`, `go test ./...` green. `gates_test.go` covers
condition true/false/uncompilable/non-boolean, resume not placing an excluded
phase, a skipped phase not blocking `Done()`, requires unmet and then met,
forward and unknown requirements refused, a probe that shells out refused, retry
succeeding within budget, retry exhausting and saying how many attempts, and no
policy meaning exactly one attempt.

## BE-O4 — the wait

### What shipped

- **`hooks.{pre, post, await}`** — three slots: before placement, after
  placement, and the wait. **A bare list still parses and still means `post`**,
  which is what a phase's hooks have always meant, so no existing blueprint
  changes.
- **`pending`** — an action reporting that it is neither done nor failed. A
  convergence is still running; a provider connection has not been made.
  Collapsing that into success or failure is how an unattended bootstrap either
  reports a product live that is not, or tears down one that is merely
  mid-deploy. It travels as a typed `*actions.PendingError` so every existing
  caller keeps its two-value signature.
- **The park.** A pending stops the run with a `ParkedError` naming the phase,
  the hook, the reason and a suggested retry — and **the placed tree stays on
  disk**, which is what makes resuming from there meaningful. Exit code 75
  (EX_TEMPFAIL), deliberately not 1: a caller that reads "waiting" as "broken"
  will tear down a working product.
- **`.orun/run.state`** — what the run is waiting on, so a person can ask
  without re-running anything.

This is what `blueprint.go` promised two milestones ago: *"Approval gates +
resumable pausing are a planned follow-on."*

### Two things the tests changed

**Pending parks from any slot, not just `await`.** The first implementation
honoured `pending` only in `await`; a pending from a `pre` hook was retried
three times and then reported as a failure. `await` is where a wait *belongs* —
it is not the only place one is *honoured*.

**Pending is never retried.** Retrying a wait turns "still running" into an
error after N attempts, when the honest answer is that it is still running.

### The cache rule, enforced

`TestDeletingRunStateChangesNothing` parks a run, deletes `.orun/run.state`,
and resumes successfully — the resume re-runs the await and learns the same
answer from the world. The cache is a convenience; it is never the truth. If it
ever becomes load-bearing, that test fails.

### Verification

`go build ./...`, `go vet ./...`, `go test ./...` green. `park_test.go` covers
slot ordering, the legacy list form, a park rather than a failure, the tree
surviving a park, the run-state record, deleting it changing nothing, it being
cleared once the wait is over, await not being retried, and a real failure in an
await still being a failure.

## BE-O5a — the task-plane actions

BE-O5 named seven actions. Seven in one change is not a reviewable diff, so it
is split: **BE-O5a** is the task plane, **BE-O5b** the rest. This half retires
the largest of a baseline's shell scripts (`track.sh`, 210 lines).

### What shipped

- **`orun.task/ensure@v1`** — find-or-create an epic, milestone or task **by
  identity**. A bootstrap is re-runnable by construction, so every write it
  makes must be idempotent: an epic by slug, a milestone by **name within its
  epic**, a task by **title within its epic**. `create` is the wrong verb, which
  is why a baseline hand-rolls list-then-create three times today.
- **`orun.task/rollup@v1`** — an epic's total / done / blocked, and whether it is
  complete. **Outstanding work is never an error**: a landing the observation
  drain has not folded yet is a cron that has not run, not a failed build.
- A task is created **clubbed and briefed in the same call**. The plane resolves
  the epic and milestone *before* minting the key, so a bad ref is a 422 rather
  than a half-made task — and a bootstrap creates tasks seconds before their
  PRs, so it must be able to say what they belong to at birth.

### Two seams, for two different reasons

- **`cloudClient`** resolves a workspace from the `org` parameter then `ORUN_ORG`
  — and deliberately *not* from the repo link or `intent.yaml` the way the CLI
  does. An action is not a person at a terminal: it runs inside a bootstrap that
  already knows which workspace it builds into, and the blueprint says so.
  Anything cleverer would let an action act on a workspace its blueprint never
  named. It never prompts, because it runs unattended.
- **`taskPlane`** is the narrow interface the ensure logic speaks, so the
  find-or-create rules — the part that is easy to get subtly wrong — are tested
  against a fake with no auth handshake and no backend.

### Verification

`go build ./...`, `go vet ./...`, `go test ./...` green. `task_ensure_test.go`
covers epic created and epic adopted (through the real `ExistingEpicOf` 409
path, not around it), milestone idempotent by name and created when new, a
milestone with no epic refused, task idempotent by title, a task born clubbed
and briefed, an unknown kind refused, and rollup reporting progress, completion,
and an empty epic not counting as complete.

## BE-O5b — the workspace actions

Three of the five. `repo/ensure` and `run/watch` are both GitHub-API work of a
different shape and move to **BE-O5c**, keeping each diff reviewable.

### What shipped

- **`orun.doctor/check@v1`** — require the named providers to be connected,
  optionally waiting. A missing connection reports **pending**, not failure:
  connecting a provider is a consent a person clicks in a console, and a
  bootstrap that treats "the human has not clicked yet" as a broken build tears
  itself down for being early. This is ~40 lines of polling shell in every
  baseline today.
- **`orun.secrets/exists@v1`** — require keys to exist. Missing is **pending**
  for the same reason: the phase that publishes them may simply not have run.
- **`orun.integrations/reconcile@v1`** — create the declared brokered secrets
  that do not exist. This is the verb `create-secrets.sh` was reaching for:
  it walks a declared table, skipping what exists and creating what does not.

### Brokered, which is the whole point

A brokered secret carries **no value** — it is a pointer at a connection and a
scope template, and the value is minted just-in-time at resolve. So this action
creates every credential a bootstrap needs while being *incapable* of holding,
logging or reading one. `TestReconcileCreatesBrokeredPointersWithNoValue` pins
that: the create request's `Value` must be empty.

**Keys that exist are kept.** Re-running a phase must not rotate a credential
the rest of the product is already using.

### Two orderings that matter

- **A failing READ is a failure, never a wait.** Mistaking "the command did not
  work" for "not connected yet" polls forever against a broken credential.
  `TestDoctorFailsLoudlyWhenTheReadItselfFails` asserts it does not even poll
  twice.
- **A write refusal after successful reads names its likely cause.**
  Resource-hiding masks authorization as not-found, so a bare `not_found` after
  two working reads sends people hunting for a missing scope when the answer is
  that the credential is below the admin floor.

### Verification

`go build ./...`, `go vet ./...`, `go test ./...` green. `doctor_test.go`
covers all-connected, a missing consent as pending, an inactive connection not
counting, waiting until a consent arrives, a failing read erroring immediately,
secrets present/missing, reconcile creating only what is missing, brokered
pointers carrying no value, not-connected waiting, and the write-refusal
explanation.

## BE-O5c — the GitHub-side actions, and the set is complete

### What shipped

- **`internal/forge`** — the small GitHub surface beyond the pen: does this
  repository exist, and is this workflow run finished. Deliberately **not** in
  `internal/provenance`: that package is the pen, the gesture that binds a PR to
  a task, and creating a repository or watching a convergence is neither.
- **`orun.repo/ensure@v1`** — find-or-create, for the same reason every other
  bootstrap write is: phase 01 is re-run like any other phase, and the second
  run must find the repository the first made rather than fail on a collision.
  An **organization and a user take different endpoints**, and that is not
  cosmetic — GitHub reads the owner of `POST /user/repos` off the token's own
  identity. A missing credential is refused rather than attempted, because an
  anonymous 404 is indistinguishable from "absent".
- **`orun.run/watch@v1`** — watch a convergence to green, resuming through
  transient failures.

### `run/watch` is the one that gets BETTER by moving

A baseline polls GitHub's Actions API to ask about an execution **orun owns**,
then calls `orun run --retry` to resume it. The binary knows which lanes are its
own and which are retriable; GitHub's `conclusion` field knows neither. This is
the single place in the epic where moving code into the binary improves it
rather than merely shortening it.

Three judgements it now makes:

- **A failure may be resumed, up to a budget.** The CI is resume-capable, so a
  convergence that trips on propagation or an evicted runner heals in place. A
  real regression fails every resume and surfaces after the budget — which is
  what a budget is *for*: "flake" is not a root cause, and an unbounded loop
  retries a genuine break forever.
- **The resume is best effort; the diagnosis is not.** GitHub refuses a re-run
  for a token without `actions: write`, for a run with no retriable jobs, and
  for one past retention. Losing the failed-lane list because the retry could
  not be issued is the part that costs someone an afternoon, so both are
  reported.
- **No run at all is a real answer.** A repository whose CI has not landed yet
  has nothing to converge; waiting for a run that will never exist would hang
  the first phase of every bootstrap.

With `waitSeconds: 0` it reports **pending** rather than blocking — which is
what makes a convergence watchable by a console that polls, instead of only by a
process willing to sit for forty minutes.

### The registry, complete

```
orun.doctor/check@v1            orun.repo/ensure@v1      orun.task/ensure@v1
orun.http/probe@v1              orun.run/watch@v1        orun.task/rollup@v1
orun.integrations/reconcile@v1  orun.secrets/exists@v1
orun.pr/land@v1
```

Nine actions — one more than the eight the design budgeted, the extra being
`secrets/exists`, which `requires.probe` needed and which the design named but
did not count. Every one is implemented and tested; none is registered that
cannot run.

### Verification

`go build ./...`, `go vet ./...`, `go test ./...` green. `forge_test.go` covers
finding an existing repo, creating an absent one via the org endpoint, the user
endpoint outside an org, an anonymous ensure refused, a 403 not being mistaken
for "absent", no runs returning nil, and skipped/neutral jobs not counting as
failures. `forge_actions_test.go` covers green, resume-then-green, the budget
exhausted naming the lanes, the diagnosis surviving a refused resume, pending
rather than blocking, no-run-at-all, and a malformed repo.

## BE-O6 — the engine reports itself

### Why

A bootstrap takes about an hour and a person needs to know what it is doing.
Today that job belongs to a model relaying a script's output, and orun-cloud's
build page records what that costs in its own header: the status strip is *"the
agent's latest line, verbatim"*, and **two of its six row states cannot be drawn
at all** because the agent brief instructs the agent to post each line *"in
plain words, without the prefix"* — stripping the very markers that carry them.

### Two registers, never confused

- **`detail`** — the machine's line. High volume, verbatim, for a log.
- **`narration`** — the line the **baseline** authored, one per transition, for
  a feed. Reviewed in a pull request, diffed like code, identical on every run.

### Three rules that keep narration honest

1. **It is a template over state**, rendered through the same constrained
   funcmap as every module — no filesystem, exec, network or clock.
2. **It may not assert a state.** `state` is the truth; narration is the
   caption. A line containing a bare state word outside an expression is a
   **parse error**: *"The edge answers /health"* describes the world, *"05-edge:
   done"* is a claim the engine alone gets to make. Whole-word matched, so
   "incompleteness" does not fire, and expression spans are exempt because a
   field reference is not an assertion.
3. **A missing line renders a generated one** — never silence, which reads as a
   stalled build. A template that fails to render falls back too: a broken
   caption must never fail a build that otherwise succeeded.

### The engine supplies the numbers

A `done` event carries `files`, `elapsed` and the declared `expectedMinutes`.
The YAML supplies the words, the engine supplies the facts, and **neither can
lie about the other** — which is the whole reason the transition line is
composed rather than authored.

### One stream, four renderings

`--progress auto | plain | verbose | json`. They differ only in what they SHOW,
never in what happened — which is what makes `--progress json` something a
baseline's end-to-end CI can assert the operator-visible sequence against. The
envelope carries `schema: bootstrap-event/v1`, because a stream four repos
depend on is an interface whether or not it is called one, and `seq` is
monotonic so a consumer that reconnects resumes from what it holds.

Every phase the blueprint declares but a run is not placing is reported once as
`skipped`, so a feed shows the whole shape rather than only the part that moved.

### Verification

`go build ./...`, `go vet ./...`, `go test ./...` green. `events_test.go` covers
authored narration being emitted, an unnarrated phase still speaking, an
excluded phase reported as skipped, the envelope and monotonic ordering, done
events carrying the engine's facts, three state-asserting narrations refused, a
state word inside an expression allowed, whole-word matching, a broken template
falling back, and the composed transition line.
