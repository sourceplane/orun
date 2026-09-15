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
| **BE-O7** | ✅ Shipped (register/publish deferred) | `orun baseline list\|show\|check`, the registry client |
| **BE-O8** | ✅ Shipped | `Hook.Workflow` deleted; `internal/scaffold` no longer imports `internal/flow` |
| **BE-O9** | ✅ Shipped | the `with:` scope, `ActionInput.BaseDir`, `task/ensure` `contract:` |
| **BE-O10** | ✅ Shipped | narration renders against state; `PhaseUnknown` for a hook-only phase |
| **BE-O11** | ✅ Shipped | a whole-blueprint run satisfies its own `requires`; a probe gates the work, not the bytes |
| **BE-O12** | ✅ Shipped | drift satisfies `requires.phases`, and `--resume` no longer un-brands a product |
| **BE-O13** | ✅ Shipped | the platform sink — the engine's account of a build, delivered to the platform showing it |
| **BE-O14** | ✅ Shipped | the bootstrap driver — serve supervises a build, with no model attached |
| **BE-O7b** | ✅ Shipped | `baseline new --local` / `--via-platform`, `register`, `publish` — **the write verbs are complete** |

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

## BE-O7 — `orun baseline`

### The gap it closes

The binary had **no baseline verbs at all**: `grep -rli baseline cmd/orun/` hit
only `tasks.go` and `agent_serve.go`, and `internal/remotestate/` had
`catalog.go`, `tasks.go`, `epics.go`, `skills.go` and no `baselines.go`. Every
registry surface was console-or-API, so the "one command" story broke at step
one — an operator had to open a console to learn an id and a tag before the CLI
could do anything with either.

### What shipped

- **`internal/remotestate/baselines.go`** — the registry client. Each method
  maps 1:1 onto a route the platform already serves; **nothing here decides
  anything.** The paid gate, the admin grant and the bootstrap session live
  behind one door on the server, and a CLI that re-derived any of them would be
  a second answer to a question the platform already answers.
- **`orun baseline list`** — the workspace-scoped catalogue by default (the
  public set **plus** what the account registered), because a signed-in reader
  should see their own private baseline at the one moment it matters.
  `--public` reads what a stranger sees.
- **`orun baseline show <id[@tag]>`** — the row, plus whether this workspace can
  build it. A **stale pin resolves and says so**: the visitor clicking a link
  from a blog post wants to build the platform, not to litigate a version.
- **`orun baseline check <id>`** — readiness as an **exit code**, for a script
  or a CI gate, naming the unconnected providers. Writes nothing, starts
  nothing.

### A bug this found in the client

`Client.doJSONOnce` dereferenced `tokenSrc` unconditionally, so a client built
to read the **public** catalogue panicked. A nil token source is *anonymous*,
not a programming error — the public read needs no session, which is what
"public" means, and requiring a login to read it would make the signed-out
catalogue a lie. Both the resolve and the `Authorization` header are now
guarded, and a test asserts the public read sends no credential.

### Deferred: `new`, `register`, `publish`

`orun baseline new --via-platform` is a thin POST the client already supports
(`Bootstrap`), but it needs a **repo link id** the CLI has no verb to resolve
yet; `--local` needs the blueprint fetched at the registry's tag, which is
`orun new` over a `git` source and belongs with the cirrus-side work that
authors one. `register` and `publish` need the orun-cloud publish door
(**BE-K4**). All three move to **BE-O7b**, rather than shipping a verb that
half-works.

### Verification

`go build ./...`, `go vet ./...`, `go test ./...` green. `baselines_test.go`
covers the public read sending no credential, the workspace-scoped route being
used by default, readiness carried through, a stale pin resolving and being
reported while the build still uses what the registry publishes, the bootstrap
POST carrying the repo link, and `Retired()`.

## BE-O8 — the workflow hook is gone

### What was deleted

`Hook.Workflow`, `Hook.Connections`, `Hook.IsWorkflow`, `hookRunner.runWorkflow`,
`validateHookGrants`, `connectionPayloads`, `hookDigestMap`, `Provenance.Hooks`,
`ProvHook`, `pinHookDigests`, and `workflow_hook_test.go`. A hook is now exactly
one of `run:` or `uses:`.

### Why it could go

The workflow hook existed because it was **the only way a hook could reach a
capability an argv could not express** — a real gap, and it cost a credential
grant, a content-digest pin and an entire engine reachable from the scaffold
path. Typed actions are that way now: in process, parameter-checked at parse
time, with addressable outputs and a closed set. Keeping both would have meant
two answers to one question, and the workflow answer was the expensive one.

**This is why the ordering was a safety property, not a preference.** BE-O8
could only land after BE-O5 completed the action set — invert them and a
baseline has no way to express a bootstrap in between. The plan said so in its
header from the start, and the sequence held.

### What did NOT go

`orun workflow` and `internal/flow` are untouched and very much alive:
`kind: Workflow` remains a first-class document and a `workflow:` plan step
still runs one. What ended is **instantiation** reaching for it. The command's
help text advertised "or blueprint hook" and now does not — a stale sentence
about a retired capability is exactly the drift this cluster exists to stop.

### Two tests hold the line

- `TestScaffoldDoesNotImportTheFlowEngine` fails if any file in
  `internal/scaffold` imports `internal/flow` again. A dependency that is easy
  to re-add by reflex is the kind worth writing down.
- `TestAWorkflowHookIsNowRefusedWithAPointer` asserts that a blueprint still
  carrying `workflow:` fails **with a message naming what a hook may be** —
  rather than parsing with a silently ignored field and a hook that does
  nothing, which is the worse outcome by far.

### Verification

`go build ./...`, `go vet ./...`, `go test ./...` green. `orun workflow --help`
verified by hand. Net across the epic so far: **+3,631 / −619** lines in the
binary.


## BE-O9 — a hook can say what it means

**Not in the original plan.** BE-O9 is a split, recorded rather than absorbed,
and it was not found by re-reading the design — it was found by starting to
write cirrus BE1 against what actually shipped. Two things the design takes for
granted turned out not to exist, and neither was visible from the orun side
because no orun test had ever written the expressions a real baseline writes.

### Gap 1 — a hook's parameters could see almost nothing

`design.md` §2 sketches a phase whose hooks say `{{ .inputs.epicslug }}`,
`{{ .phase.name }}` and `{{ .phase.hooks.task.outputs.key }}`. What BE-O1
shipped put exactly one key in scope:

```go
scope := map[string]any{"hooks": hookScope(hr.outputs)}
```

Templates render under `missingkey=error`, so every one of those expressions
was a **run-time failure**, not an empty string. That is the good failure mode
— but it meant the design's own worked example could not be authored, and the
only way for a baseline to name its phase in a branch, a PR title or a
milestone was to type the phase name again on every line that needed it.

The scope now carries three things:

- `.inputs.<name>` — **secret-free**. `nonSecretFields()` already renders a
  secret field as the literal `<secret>`, so a blueprint that reaches for a
  credential gets the redaction. A hook that needs one gets it brokered at
  resolve time; an input rendered into a parameter would travel onward through
  an argv, a PR body or a task brief.
- `.phase.name` / `.phase.title` — with `title` falling back to the name, so a
  phase that never titled itself does not put a bare colon in a PR title.
- `.hooks.<id>.outputs.<key>`, also reachable as `.phase.hooks.…` — both
  spellings work, so the design's text is literally true and the short form a
  hook already used keeps working.

What is deliberately **not** in scope: the placed file set, the provenance
lock, anything about an earlier phase. Reaching for one of those fails at the
line that reached, which is the behaviour BE-O1 chose and this keeps.

### Gap 2 — `orun.task/ensure@v1` could not contract a task

The action's own doc comment, shipped in BE-O5a, states the requirement:

> The plane parks a merged PR at `in_review` when its task never declared
> gates — gates unknown to us are not gates passed. A bootstrap creates its
> tasks seconds before their PRs, so it has to be able to say "merge alone
> finishes this" at birth, or every landing it makes sits un-folded forever.

The code did not do it. `TaskCreateRequest` carries no contract, nothing
attached one, and the action had no parameter to name a document with. A
bootstrap switched from `track.sh` to this action would have created exactly
the un-folded landings the comment warns about — and `orun task create --help`
says the same thing in the user's own words: *"one created without a contract
parks at in_review after its merge."* Three places agreed on the rule and the
implementation quietly did not.

`contract:` now names a TaskContract document. It is:

- **read before anything is created**, so a malformed document costs no minted
  key — the discipline `orun task create` already kept, for the same reason;
- **attached on the found path as well as the created one**, because a
  bootstrap re-runs: the run that made this task may have predated its
  contract, or died between the create and the attach. Attaching the same bytes
  is a no-op by content hash, so the re-run heals instead of leaving a parked
  landing behind;
- **optional**. An uncontracted task stays an honest state; the action does not
  invent a document.

### Gap 3 (consequence) — an action could not name a baseline file

A contract lives beside the blueprint and is deliberately **not** copied into
the product — it is the factory's machinery, not the product's. But an action
received only `Dir`, the product tree, so a relative path could only ever
resolve to the wrong place.

`ActionInput.BaseDir` carries the blueprint's own directory, and
`actions.PathParam` resolves a path parameter against it (absolute paths as
given; an empty `BaseDir` falls back to `Dir` for a caller driving an action
directly). A test swaps the two directories and asserts the swap **fails**
rather than creating an uncontracted task.

### Gap 4 — a templated LIST passed through unrendered

Found the same way, one layer down. `resolveWith` rendered string parameters
and let everything else through:

```go
s, ok := value.(string)
if !ok || !strings.Contains(s, "{{") { out[name] = value; continue }
```

Every parameter a baseline actually needs to template is a **list**: the URLs
a phase probes, the secret keys it requires. Those are exactly the values that
depend on what the operator answered — a product's health endpoint is its own
repo name and its own workers subdomain. So the one shape that had to render
was the one shape that did not, and the symptom would have been an HTTP
request to the literal text `{{ .inputs.repoName }}` reported as a failed
probe: a wrong answer dressed as a real one.

`renderValue` now recurses into `[]any`, element by element, and names the
element on failure (`urls[1]`, not `urls`) — "urls is wrong" sends a reader to
a block of six, `urls[1]` sends them to a line. Anything that is not a string
or a list still passes through untouched: only text can carry an expression.

### Gap 5 — a `run:` hook could not name the baseline

design.md §2 writes an argv hook as:

```yaml
- id: rebrand
  run: [node, "{{ .baseline.dir }}/tooling/rebrand/rebrand.mjs", --values, .rebrand/values.json]
```

`runArgv` exec'd `h.Run` verbatim — no rendering at all. And an argv hook runs
in the PRODUCT tree, where a baseline's machinery is deliberately absent: the
rebrand tool and the bootstrap helpers are the factory's, not the product's.
So the hook could only name files the product carries, which is precisely the
set that excludes every tool a bootstrap runs. cirrus's own blueprint already
carried `run: ["node", "tooling/rebrand/rebrand.mjs", …]` and a comment
explaining that "hook argv is not templated" — a latent failure that nothing
had hit only because nothing ran those hooks.

Each argv element now renders through the same engine, with the same scope
plus `.baseline.dir`. This is **not** a widening of what a hook may reach: an
argv is a list, not a command line, and nothing between the elements
interprets anything — a rendered element is exactly one argument however it
renders, which a test pins with a value containing `; rm -rf /`.

### One implementation, two callers

The seal-then-attach step now lives once, in `taskfile.Attach`, behind a
one-method `Attacher` interface. `orun task create` and the action both call
it. The alternative was two copies of the same eight lines in packages that
cannot see each other — fine today, drift the first time attach gains an
argument.

### Verification

`go build ./...`, `go vet ./...`, `go test ./...` green. Fifteen new tests.
Ten on the scope — a secret input reaches no parameter; an unknown key is an
error rather than an empty string; an expression inside a list renders and a
failure inside one names the element; the baseline is nameable from an argv
and a rendered argv element stays exactly one argument. Five on the contract —
attached on create, attached on re-find, the baseline-vs-product path swap, no
minted key for a malformed document, and nothing attached when none is
declared.


## BE-O10 — the engine's own words, and what it will not claim

**Not in the original plan.** Found the same way BE-O9 was: by writing a real
baseline against what shipped rather than against the design. cirrus BE1 landed
the phases; starting BE2 — the narration contract — turned up the first half,
and BE1's own two hook-only phases turned up the second.

### Narration was never rendered

design.md §4 rule 1: *"It is a template over state, never prose about facts.
`{{ .phase.title }} is live on {{ .envs | join }}` cannot assert what the state
does not hold."*

`renderNarration` took a `scope map[string]any`. Every call site passed `nil`:

```go
Narration: renderNarration(decl.NarrateLine(EventStarted), title, EventStarted, nil),
```

So the rule was true of the **type** and false of the **values**. An authored
line containing any expression failed to render and fell back to the generated
line — and the fallback is what makes it bad rather than merely broken: a line
that never rendered looks exactly like a phase that authored nothing. A
baseline author would write the prose, review it in a pull request, watch the
build, and never learn that what they wrote was not what was shown.

Three fixes, in the order a mistake should be caught:

1. **Parse time** — a narration template that cannot compile is a blueprint
   error naming the line (`narrate.done`). Finding out at run time means
   finding out in front of the operator it was written for.
2. **Run time** — a render that fails now appends `(narration unavailable: …)`
   to the generated line instead of silently substituting it. A build must not
   die because a caption did not, but nor should the caption disappear.
3. **The scope itself** — `.phase.{name,title}`, `.inputs.<name>` (secret-free,
   as everywhere), `.meta.<key>` and `.hooks.<id>.outputs.<key>`.

`.meta` is the half that matters: it is **the engine's facts** — files placed,
`expectedMinutes`, `elapsed`, the next phase — not the author's. design §4 ends
with *"the YAML supplies the prose, the engine supplies the numbers, and
neither can lie about the other"*, and until now a line had no way to reach the
numbers. The scope deliberately mirrors a hook's `with:` scope: a baseline
author should not have to learn two vocabularies for one document.

**A hook's `narrate:` was the one unvalidated, unrendered line in the
blueprint.** `emitHookNarrations` emitted `h.Narrate` verbatim, so a hook
caption could assert a state — the exact thing `validateNarration` refuses on a
phase's lines — and could carry a template that would never render. It now
goes through both.

### A hook-only phase claimed to be done

```go
// A phase that places nothing — every module consume-mode — is done by
// definition; there is nothing that could be missing.
if len(files) == 0 { st.State = PhaseDone; return st }
```

True for the case the comment names. False for a phase whose whole content is
hooks, which is what cirrus BE1 produced two of: `04-workers-restore` re-adds
service bindings, `08-docs` records the deployment. Neither writes a file the
product tree keeps, and both do real work. Reporting them done meant `--resume`
skipping them and `requires.phases` passing on a phase that never ran.

`PhaseUnknown` separates the two situations honestly — no files **and** no
hooks is done; no files **with** hooks is something placement cannot answer,
because the record is in the task plane and the deployment.

Where it lands:

- **`--resume` re-runs an unknown phase.** That is the safe direction: a
  bootstrap's hooks are idempotent by construction (find-or-create, additive
  apply), so re-running one that was already done costs a few API calls, while
  skipping one that was not leaves a bootstrap silently incomplete.
- **`requires.phases` fails closed** and the message explains why, rather than
  reporting a bare `unknown` the reader has to decode.
- **`--status` shows `?`**, which is what the operator should see.

This answers cirrus's open question 7 and lets `04-workers-restore` and
`08-docs` be depended on.

### Verification

`go build ./...`, `go vet ./...`, `go test ./...` green. Seven new tests: four
on narration (every scope key renders; a failed render says so; an uncompilable
template is a parse error; a hook line is held to the state-word rule) and
three on derivation (hooks + no files is unknown; neither is still done;
`--resume` does not skip an unknown phase).


## BE-O11 — a whole-blueprint run, and what a probe is for

**Found by cirrus BE5a's Tier 1, on its first execution.** Not by reading the
code — by a baseline trying to do the most ordinary thing there is.

### A run could not satisfy its own requirements

Preconditions are checked before the first byte is written:

```go
for _, ph := range phases {
    if err := checkRequires(ctx, plan, opts, ph, opts.Actions); err != nil {
```

So a run placing every phase had `02-foundation` ask whether `01-scaffold` was
on disk **during the very run that was about to write it**, and always hear no:

```
✕ phase "02-foundation" requires 01-scaffold (pending) — run 01-scaffold first
```

A bootstrap never noticed: it runs phase by phase, and the tree accumulates
between runs. But any blueprint declaring `requires.phases` could only ever be
run one phase at a time — and a dry instantiation, which is the check that
tells a baseline what its product looks like before an hour of real cloud, was
impossible.

`checkRequires` now takes the phases this run places **earlier**, accumulated
in phase order, and treats them as satisfied. The question a requirement is
really asking is *"will this be there when my hooks run"*, and for a phase
ordered earlier in the same run the answer is yes.

What this must not soften is the case the requirement was written for, and two
tests that predate BE-O11 hold it: a phase run alone still refuses when its
predecessor is genuinely absent, and still passes once it is on disk.

### A probe gates the work, not the bytes

`requires.probe` ran on every placement. But placement writes files — it does
not deploy, and it does not read a secret. What a probe protects is the phase's
**hooks**: the landing, the convergence, the thing that fails if the database
binding an earlier phase published has since been deleted.

With `--run-hooks` off — the default — there is nothing to protect, and probing
anyway means a placement that cannot happen without a workspace and a live
provider. That is not hypothetical: it is the other half of what stopped a
baseline from ever running its own dry instantiation.

Probes now run only when hooks do. `requires.phases` still applies either way:
it asks the tree, which is always there to ask. This answers the standing open
question about reaching the substitute-runner seam from the CLI — **no flag was
needed**, because the rule was already implicit in what the two halves mean.

### Verified against the thing that failed

cirrus's real `repo-blueprint.yaml`, offline, no credential, no workspace:

```
  │ components: admin-worker, admin-worker-tests, api-edge, api-edge-tests (+38 more)
  │ plan: fb41a4ea0904
  repo gate: validate + plan --dry-run passed
--- files placed: 1085
```

and `rebrand --verify` on the result: *"no baseline-identity leftovers"*. That
is cirrus BE5b's Tier 1 in full, working.

Four new tests: a whole-blueprint run satisfies its own requires; `--until`
does too, inside its selection; a placement with no hooks does not probe; a run
that executes hooks still does. `go build/vet/test ./...` green.

## BE-O12 — what drift means to a product that has been branded

### The two failures

Found the way BE-O9, BE-O10 and BE-O11 were: by running cirrus's real
blueprint through the sequence a bootstrap actually performs. BE-O11 made a
whole-blueprint run possible, which made it possible to ask the next question —
*what happens on the second phase of a phase-by-phase bootstrap?* — and the
answer was that it did not happen at all.

**1. A requirement could not be satisfied by a branded predecessor.**

```
✕ phase "02-foundation" requires 01-scaffold (drifted) — run 01-scaffold first
```

cirrus's `01-scaffold` places the tree and then **rewrites the baseline's
identity out of every file it just wrote** — that is the whole point of the
phase, and BE1 moved that hook chain into it. So from the moment phase 01 ends,
`01-scaffold` derives `drifted`, permanently. `checkRequires` accepted only
`done`, so every later phase was refused, and the bootstrap died at step two.

**2. `--resume` reverted the product's identity.**

```
before resume:  "name": "acme-cloud"
after  resume:  "name": "cirrus"
```

`selectPhases` re-placed any phase not `done`, drift included. On a branded
product that is every placed phase, so a resume overwrote the branded tree with
the baseline's own rendering — silently, and in the file a product is
identified by.

### Why it was wrong, which is not the same as being an oversight

Both sites had a stated reason, and they contradicted each other. `status.go`:

> **PhaseDrifted**: every file is present but at least one differs. Someone
> edited the product, or the blueprint moved. Re-running would overwrite, so
> this is reported and never silently resolved.

`plan.go`, on the same state:

> Drift is not skipped: re-placing restores the phase to what the blueprint
> says, which is what a resume is for. It is reported by `--status` so nobody
> is surprised by it here.

The second rests on an assumption the first does not make: that a product's
files are *meant* to equal the blueprint's rendering, so any difference is
damage. A baseline that brands what it places breaks that assumption **by
design**. What the blueprint says is the unbranded baseline; restoring the
product to it is not a repair.

And "reported by `--status` so nobody is surprised" does not survive contact
with the phased bootstrap either: a resume is what an automated build runs
between phases, not what a person types after reading a status table.

### What changed

**`requires.phases` accepts `drifted`.** The question a requirement asks is
*will the predecessor's files be there when my hooks run*, and for a drifted
phase every one of them is. `pending`, `partial` and `unknown` still fail, and
each for a reason the tree can back: nothing placed, a placement interrupted,
or — per BE-O10 — a phase the tree cannot answer for at all.

**`--resume` treats `drifted` as placed and skips it, out loud:**

```
resume: leaving 02-foundation, 04-workers as placed — every file is present but
differs from the blueprint (branded, or edited). Re-place one deliberately with
--phase <name>.
```

Re-placing a phase whose blueprint genuinely moved is still available, as the
deliberate act it should be. `partial` is untouched: files really are missing,
and re-running is what completes it.

### Verification

Four tests, each shown to fail on the old rule before the change:

| Test | On the old code |
|---|---|
| a requirement is satisfied by a drifted predecessor | `requires infrastructure (drifted) — run infrastructure first` |
| a partial predecessor still fails the requirement | passes — accepting drift must not become accepting anything |
| resume leaves a drifted phase alone | `resume re-placed a drifted phase`; `b.txt` read back `"two"`, not the branded content |
| resume still places a partial phase | passes — the guard against over-correcting |

And against the thing that failed: cirrus's real `repo-blueprint.yaml`, offline,
placing `01-scaffold`, running `rebrand.mjs` over it exactly as that phase's
hooks do, then `02-foundation`, `03-infrastructure` and `04-workers` — all
three now place, where the first refused before.

### One thing this does NOT fix, in the baseline rather than here

`05-edge` still refuses, and correctly:

```
✕ phase "05-edge" requires 04-workers-restore (unknown) — …
```

`04-workers-restore` places no files, so BE-O10's `PhaseUnknown` fails closed —
which is the honest answer and is not softened here. The defect is that
cirrus's blueprint gates on a phase the tree can never answer for; that is
cirrus's to fix, and the engine should keep refusing it until a hook-only phase
can declare its own evidence (a `doneWhen`, or its `requires.probe`).


## BE-O7b (part) — `baseline new --local`

### What shipped

`orun baseline new <id[@tag]> --local --out <dir>` builds a registered
baseline from its own source, on this machine. Three facts, composed: the
registry says WHERE the baseline lives and WHICH COMMIT is published, its
manifest says WHICH DOCUMENT in that tree is the build, and `orun new` places
that document's phases. An operator held all three before this and joined them
by hand.

### What unblocked it, and what it is still missing

BE-O7 deferred all three verbs rather than "shipping a verb that half-works",
and named `--local`'s blocker: the blueprint had to be "fetched at the
registry's tag, which is `orun new` over a `git` source and belongs with the
cirrus-side work that authors one". Both halves arrived:

- cirrus BE4 made `repo-blueprint.yaml` the one artifact, and BE4c declares it.
- orun-cloud **BE-K1e** accepts `spec.bootstrap.blueprint` in the manifest;
  **BE-K1f** serves `manifestPath` on the row the CLI already resolves through.

`--via-platform` still needs a repo-link id this CLI cannot resolve, and
`register`/`publish` still need orun-cloud BE-K4. So `--local` is REQUIRED
rather than defaulted: the two shapes have different failure modes and
different places to watch them, and a flag that silently picks one surprises
somebody at the worst moment.

### Why the manifest, and not `blueprint.yaml`

`blueprint.yaml` is what every registered row but one names. `stratus-coolify`
is that one — two rows over a single tree, each with its own manifest — so a
caller guessing the convention serves the Azure contract to a Coolify build.
That substitution has happened on the platform side already, with the agent
brief, which is why the row carries a path. A test builds both rows from one
tree and asserts they resolve to DIFFERENT documents; a convention-based
implementation passes every other test in the file and fails that one.

### Readiness is a gate

A bootstrap that starts without its providers does not fail at the door. It
fails thirty minutes in, having created a repo and half a product, and the
operator reads a Cloudflare error rather than "you never connected
Cloudflare". `baseline check` exists to ask in advance; this asks again,
because the answer can change in between, and refuses by name.

### One pipeline, not two

The verb sets the same options `orun new` sets and calls `runScaffoldNew`. A
second path to place a blueprint is a second path to place one wrongly, and
every gate the first carries — the two-parser output check, the phase barrier,
the requirement gate — is one this build needs exactly as much.

### Verification

`go build ./...`, `go vet ./...`, `go test ./...` green. Twelve cases over the
build-document read: the declared path, a nested one, and the refusals —
absent, empty, whitespace, absolute, escaping, escaping only AFTER cleaning,
unparseable, an unnamed `manifestPath`, a document not in the tree, a missing
manifest. Three mutants, three failures: guessing the convention, checking the
raw path for `..` instead of the cleaned one, and defaulting an empty key.

The network is one seam (`fetchBaselineSource`), so the join is tested end to
end without one; behind it is orun's own git source resolver, so a baseline
fetched here and a `kind: git` source fetched during placement come down the
same shallow, tag-or-branch, pinned path.

## BE-O7b — `baseline register` and `baseline publish`

### What shipped

`orun baseline register <id>` and `orun baseline publish <id> <tag>`, over the
doors orun-cloud BR3 and BE-K4c serve. With `baseline new --local` already in,
BE-O7b is complete but for `--via-platform`.

### The order publish exists to remove

A baseline's version lives in two repositories: the git tag in its own source,
and the pin in the platform's registry. They have to move in that order —
what a build first reads is fetched from the source repo AT THE REGISTRY'S TAG
— so a pin moved before the tag is pushed means every build of that baseline
404s until somebody notices. The catalogue file warns about this in a comment,
which is the only place the rule lived.

One verb cannot get the order wrong. Push the tag, then run this: if the tag is
not there, the registry does not move.

### Nothing is decided here, and the refusal is verbatim

The door proves the tag — that it IS a tag and not a branch, that the files a
build enters through resolve at it, that the build contract parses with the
platform's own parser — and this reports what it said. A client that formed its
own opinion would be a second answer to a question the platform already
answers, and the two would drift the first time the contract changed.

That decides how a refusal is printed: the door's message names the file and
the line and what to do about it, and a CLI that replaced it with "publish
failed" would throw away the only part worth reading.

### The one thing publish refuses on its own

`id@tag` is the READ vocabulary — `show`, `check` and `new` all take it, and
there the tag says WHICH VERSION TO LOOK AT. Here the tag is what CHANGES, so
`orun baseline publish cirrus@baseline-v5 baseline-v6` has two tags in it and
no reading of it is obviously right. Refused rather than guessed: the wrong
guess moves a registry pin.

### `register`'s two rules

**`--brief` and `--umbrella` are a pair.** A baseline with a brief is run
through its umbrella; one with NEITHER is blueprint-driven and its build
document declares every phase — the shape this epic made canonical and the one
`baseline new --local` builds. Half a shell layer is a row somebody edited
halfway, and a build of it fetches a file that is not there. Declaring neither
sends neither field, rather than two empty strings: the door distinguishes "not
declared" from "declared empty", and sending fields nobody set is a claim about
paths the caller never mentioned.

**`--visibility public` is refused**, naming the flag the caller typed. A
public baseline is a repository an agent clones into a stranger's workspace and
runs guardrails from, so it is the platform's to grant. The door refuses it —
that is the boundary — and this refuses it one round trip earlier.

Neither refusal replaces the door's. Both are the courtesy of not spending a
round trip to be told something this binary already knew.

### `--manifest`, and why it could not work until now

`register` gained `--manifest` because orun-cloud's write door gained the field
in the same change (BE-K4c). Until then there was no key for it at all, so the
column's default was the only value an account-registered row could ever hold —
and `stratus-coolify`, two rows over one tree, is the case that makes it a path
rather than a convention.

### Still deferred

`baseline new --via-platform` needs a repo link id this CLI has no verb to
resolve. That is unchanged from BE-O7, and it is the one remaining bullet.

### Verification

`go build ./...`, `go vet ./...`, `go test ./...` green. Fifteen mutants,
fifteen caught. The publish client: the tag never reaching the door, a blind
retry of a write, and the wrong HTTP verb. The publish command: `id@tag`
accepted, blank arguments accepted, and the verb never registered. The register
client: a blind retry, `manifestPath` not serialized, and the account-scoped
door. The register command: half a shell layer accepted, `public` accepted,
empty shell paths sent anyway, `manifestPath` dropped, missing flags unchecked,
`private` sent explicitly rather than as the door's own default, and the verb
never registered.

Both writes are NOT RETRYABLE, and both tests prove it by counting requests. A
blind retry of a publish that timed out after the registry moved is a second
publish of a tag the caller has already been told about; a blind retry of a
register that timed out after the row was created reports the door's 409 — the
allocator posture, carrying the existing row — as a failure of the thing that
in fact succeeded.

## BE-O7b — `--via-platform`, and the verb BE-O7 could not resolve

### What unblocked it

BE-O7 deferred this for one reason, in its own words: it *"needs a repo link id
the CLI has no verb to resolve yet"*. That verb is a list and a name match.
`GET /v1/organizations/{org}/repo-links` has served the workspace's links,
across every project, since GS6 — id, full name, status and `agentAccess` —
and a person standing in a repository knows the repository. Joining those two
belongs in the binary rather than in somebody's clipboard.

### The only unacceptable behaviour here is guessing

This chooses where an hour of automated commits lands: branches created, pull
requests merged, terraform applied against a real cloud account. So:

- **An ambiguous match refuses**, naming both ids. A workspace can hold two
  links to one repository — GitHub resolves `Acme/Storefront` and
  `acme/storefront` to the same place, which is why orun-cloud's build lease is
  keyed lowercased — and "the first one" is not a reason.
- **Matching is case-insensitive**, for the same reason GitHub is. A
  case-sensitive match would send somebody to link a repository already linked.
- **`agentAccess: off` refuses**, naming the switch to flip. The door refuses
  it too; that is the boundary, and this costs no round trip.
- **The repository is printed before anything starts.** An hour of commits into
  the wrong repository is not recoverable by pressing ctrl-c afterwards.
- **A "no link for X" refusal names what IS linked**, because otherwise it
  sends somebody to the console to read a list this command already holds.

### Neither shape is the default

`--local` writes into a directory here; `--via-platform` writes an entire
product into somebody's linked repository. They are different operations with
different failure modes and different places to watch them, so exactly one must
be named — and naming both is a question rather than a preference. `--out`
stays required for `--local` alone: a platform build has no local output, and
demanding a directory nothing writes to would be a flag for its own sake.

### Nothing is decided here

The admin requirement, the paid gate, readiness, the repo grounding, the
one-build-per-repository lease and the time-boxed admin grant all live on the
server. Readiness is re-checked here only because the answer arrives before
anything is created, and the door's refusals are printed VERBATIM: they name a
plan, a missing input, or the person already building into this repository, and
every one of those is more useful than "bootstrap failed".

Inputs go through `collectScaffoldInputs` — the same function `--local` uses —
so the two shapes cannot disagree about what a `--values` file means.

### Verification

`go build ./...`, `go vet ./...`, `go test ./...` green. Seven mutants, seven
caught: picking the first of two links, a case-sensitive match, `agentAccess:
off` accepted, the nearby-links hint dropped, neither shape accepted, both
shapes accepted, and `agentAccess` not deserialized off the wire — which would
make a build resolve against a link that looks writable and is not.

With this, BE-O7b's three deferred verbs are all in: `new --local`,
`new --via-platform`, `register` and `publish`.
