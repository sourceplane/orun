# orun-cache-efficiency — what orun caches, what it costs, where it breaks, and where to improve

**Status:** analysis, October 2026, against orun `main` at fbb77d6 (v2.79.0 plus
the sync-efficiency status) and sourceplane/orun-cloud `main` at 948a0c3.
**Companions:** `specs/orun-runner-efficiency` (work per provisioned vCPU) and
`specs/orun-sync-efficiency` (the CLI ↔ Orun Cloud traffic). This one is about
the third cost: what a run has to download, build or compute again because a
cache was missing, cold, private, stale or wrong.

## 0. Summary

orun keeps more than a dozen caches in three scopes: the machine (`~/.orun`), the checkout
(`.orun/`) and the process. They were added one at a time for one purpose each
and they mostly work; what is missing is the layer that makes a set of caches a
system — a shared contract for keys, locks across processes, freshness, a size
budget and a way to inspect or prune them. The gaps show up as four bugs and
four large waste items:

| | Finding | Effect |
|---|---|---|
| F1 | A memoized ("hermetic") job's key omits its source tree and the composition lock, and a hit restores no outputs | a labelled job is skipped on stale inputs and leaves nothing behind for its dependents |
| F2 | The action-cache lock file survives a crash and is never reclaimed; the waiter gives up after 30 s; the tarball download itself is capped at 30 s | every later run of that action version on the machine waits 30 s and fails until someone deletes the lock; a slow download fails outright |
| F3 | The on-disk ref cache never expires and the tarball is fetched by the mutable ref, not the resolved SHA | `uses: x@main` and `x@v4` are frozen per machine forever; the directory named by one SHA can hold another commit's content |
| F4 | The tool-cache lock and the Terraform plugin-cache slots are process-local | two orun processes on one host (matrix mode, co-hosted lanes) race on `RUNNER_TOOL_CACHE` and share Terraform slot 0: the #734 and #736 failures return |
| F5 | The composition cache copies `dir` sources into `~/.orun/cache` on every new digest, promotes entries without a lock, and never prunes | one copy of the package per edit, forever; two processes can delete each other's live entry |
| F6 | Each job's `HOME` is a fresh temp directory, deleted at job end | every tool cache not explicitly shared (Go, pip/uv, cargo, gradle, Playwright, Cypress, yarn, bun…) is cold per job and discarded; the CI workflow carries its own workaround for Playwright and Next |
| F7 | The engine exports `pnpm_config_store_dir`, which pnpm 10 does not read | without the workflow's hand-set `npm_config_store_dir`, every lane downloaded every package (~35 s a lane, 177 lanes) |
| F8 | The tool cache is always `~/.orun/tool-cache`, never the host runner's `RUNNER_TOOL_CACHE`, and nothing lets a caller choose | every lane downloaded Node until the workflow symlinked the host's cache in |
| F9 | The turbo cache is one directory per run under `.orun/runs/<exec>/cache` and is left behind | the next run starts cold; the checkout accumulates one cache per run |
| F10 | Setup actions run per job: pnpm is installed into the per-job prefix and thrown away; each job hardlinks the action tree (setup-node: 8.3 MB, 153 files) | a lane of n jobs installs pnpm n times |
| F11 | The presenter's "cache hits" count is keyed on a message setup-helm prints on misses too, and misses setup-node's hit message | the run summary's cache figure is noise |
| F12 | No cache has a size budget, an age, or a prune command; the object GC does not cover them | `~/.orun` and `.orun` only grow |

The proposals (§5) are seven milestones: CE0 fixes the bugs with small changes
(the lock, the store variable, the hit signal, the run directory); CE1 makes
action fetching fresh and tamper-evident (fetch by SHA, a TTL on refs); CE2
takes the locks across processes; CE3 gives jobs the host's tool cache and a
shared cache home with declared caches; CE4 makes the turbo cache and the
setup actions per checkout and per lane instead of per run and per job; CE5
fixes and prunes the composition cache and adds `orun cache`; CE6 makes
memoization correct; CE7 (optional) emulates `actions/cache` natively.

## 1. Scope and method

Read: `internal/gha` (`engine.go`, `fetch.go`, `isolate.go`), `internal/runner`
(`workspace.go`, `lane_setup.go`, `presenter.go`, `runner.go`),
`internal/composition` (`registry.go`, `archive.go`), `internal/loader`,
`internal/objplan` (`memo.go`, `plan.go`), `internal/objindex`, `internal/objgc`,
`internal/statebackend` (`coordbackend.go`, `coordclient.go`, `memo.go`),
`internal/remotestate/auth.go`, `internal/agent/ground/token.go`,
`internal/integrationscli/cache.go`, `internal/platformmcp/entitlement.go`,
`internal/configsurface/client.go`; the docs that describe them
(`website/docs/execute/runners.md`, `architecture/execution-runtime.md`,
`concepts/lanes.md`); orun-cloud's `.github/workflows/ci.yml`, the edge's
`actor-cache.ts` and `org-ref-cache.ts`, the state worker's `authz.ts`,
`handlers/logs.ts`, `task-lease-gate.ts`; and the two setup actions whose
output the presenter parses (`actions/setup-node`, `azure/setup-helm`).

Measured, on this sandbox (Linux, orun built from `main`): the composition
cache across a plan and an edited plan (§3 M1); the lock behaviour with a
throw-away test against `withLock` (M2); what a job leaves under
`.orun/runs/<exec>` with a throw-away test against `stageJobWorkspace` (M3);
the size of the action trees each job materializes (M4). The action fetch path
itself could not be exercised here (the sandbox has no GitHub API access), so
F2 and F3 rest on the code and the lock test. The production numbers for the
CI workflow are the ones its own comments record.

## 2. The caches today

### 2.1 The GitHub Actions engine (`internal/gha`)

One `Engine` per `orun run` process (`cmd/orun/command_run.go` →
`executor.Get`), so its in-memory maps span every job of the process.

**Action code.** `fetchRemoteAction` keys an action by
`<owner>/<repo>@<resolvedSHA>#<path>`: in memory (`actionCache`, singleflight
per key), then on disk at `~/.orun/actions/<owner>/<repo>/<sha>/` with a
`.ready` marker, filled through `<dir>.tmp` and a rename. The fill is guarded
by `withLock(<dir>.lock, 30 s)`: an `O_EXCL` file, polled every 100 ms,
removed by the process that created it, with no staleness rule (`fetch.go`
`withLock`). The tarball comes from `GET /repos/{repo}/tarball/{ref}` — the
reference as written (`v4`, `main`), not the resolved SHA — through the
engine's `http.Client{Timeout: 30 s}`, which bounds the whole download.
Entries are never evicted; one directory per version ever used.

**Refs.** `resolveRemoteRef` turns `repo@ref` into a SHA in three tiers: the
process map (`refCache`, singleflight), the file
`~/.orun/actions/<owner>/<repo>/refs/<slug>` (honoured only while the SHA's
`.ready` exists), then the API (`/repos/{repo}/commits/{ref}`). The file has no
age: once written, a tag or branch is pinned on that machine until the
directory is deleted by hand (`readRefFromDisk`, `writeRefToDisk`).

**Per-job copies.** `materializeAction` (`isolate.go`) mirrors the cached tree
into the job's temp directory (`$TMPDIR/orun-gha-<job>-*/actions/<repo>-<sha12>`),
hardlinking regular files and recreating directories and symlinks, once per
job per action; `FinalizeJob` removes the temp directory. The cache is
therefore read-only during execution, as the docs say.

**The tool cache.** `RUNNER_TOOL_CACHE` is `~/.orun/tool-cache` (or
`Options.ToolCacheDir`, which nothing sets) — persistent across runs, shared
by every job on the machine, mounted at `/github/toolcache` for docker
actions. Actions whose name contains "setup" run one at a time per tool-cache
directory *within the process* (`lockToolCache`, a `sync.Map` of mutexes),
the fix for the half-written Node tree of #734.

**The job's HOME and prefixes.** `newJobState` creates `$TMPDIR/orun-gha-<job>-*/home`
and makes it `HOME`, with `npm_config_prefix` and `PNPM_HOME` under it, so
global installs do not race between jobs (GHA gets this from fresh VMs). The
directory goes with the job.

**Shared stores.** Content-addressed stores are shared on purpose:
`~/.orun/actions/shared/npm` (`npm_config_cache`), `…/shared/pnpm-store`
(`pnpm_config_store_dir`, `PNPM_CONFIG_STORE_DIR`), and the Terraform plugin
cache (`TF_PLUGIN_CACHE_DIR`), which is unsafe under concurrent `init` and so
gets one slot per live job *in the process* (`acquireTerraformSlot`,
`…/shared/terraform-plugins[-N]`), the fix for #736.

**GitHub's own cache service.** The engine sets `ACTIONS_RUNTIME_URL` to
`https://orun.invalid/runtime` and a random token: `actions/cache` and
setup-node's `cache:` input reach nothing, warn, and continue. Nothing
emulates them.

**Docker.** Pulled and built images are remembered per process
(`pulledImages`, `builtImages`); the daemon's own cache does the rest.

### 2.2 The workspace (`internal/runner`)

**Staging.** With isolation on (the default when concurrency > 1),
`stageJobWorkspace` materializes `<source>/.orun/runs/<exec>/<job>/work`
by clonefile/FICLONE or byte copy, skipping `.git`, build outputs and
(unless the lane installed them) `node_modules`; component mode copies only
the job's directory and links the rest. `Cleanup` removes `<exec>/<job>` and
then tries `os.Remove(<exec>)`, which succeeds only when the exec directory
is empty.

**Lane-scoped steps.** A `scope: lane` step runs once per process; later jobs
with the same `(run, shell, dir, env)` key wait and reuse its result
(`laneSetup`). `use:` setup actions are not shared: each job runs its own.

**The turbo cache.** Every staged job gets
`TURBO_CACHE_DIR=<source>/.orun/runs/<exec>/cache/turbo`: one cache per run,
so the second job of a lane hits what the first built (`stagedWorkspace.Env`).

**Git.** `GIT_DIR` and `GIT_WORK_TREE` point the staged tree at the source
repository instead of copying `.git`.

### 2.3 Compositions (`internal/composition`)

Every declared source is resolved into `~/.orun/cache/compositions/<sha256hex>/`:
a `dir` source is hashed file by file (`hashDirectory`, every plan) and then
*copied* into the cache (`ensureCachedDirectory`); an `archive` is hashed and
extracted; an `oci` source is pulled by digest, and since SE6 a lockfile-pinned
digest is served from the cache without resolving the tag. A hit is "the entry
holds `stack.yaml` or `orun.yaml`". A fill is `MkdirTemp` → copy →
`RemoveAll(cacheDir)` → `Rename`, with no lock between processes. Nothing
prunes the directory.

### 2.4 The plan (`internal/loader`, `internal/objplan`, `internal/objindex`)

- `.orun/component-tree.yaml`: the discovered component entries, served while
  the discovery roots, the file list and each file's size and mtime match
  (`loadDiscoveredComponentEntriesFromCache`). Sound; the saving is the YAML
  parse of each manifest.
- `.orun/objectmodel/cache/resolve/<sourceHex>-rv<N>[-in<digest>].json`: the
  input-addressed resolve memo (`ResolveMemo`), `(sourceId, resolverVersion,
  workspace-inputs digest)` → `catalogId`, honoured only if the catalog object
  still exists; written atomically; one file per source id ever planned. The
  object GC prunes executions, not this directory.
- `.orun/objectmodel/index/executions/all.json`: the executions index,
  rebuilt by a walk on a miss or a corrupt file.
- The planner's template cache and the catalog resolver's known-keys cache are
  per process.

### 2.5 Coordination (`internal/statebackend`)

**Memoized jobs.** A job labelled hermetic (`isHermetic`, the `HermeticLabel`)
sends `jobInputHash` with its claim: the sha256 of the canonical
`{steps, envKeys, inputDigests, compositionLockDigest}` (`memo.go`), where
`jobInputHashFor` fills only `steps` and `envKeys` — the comment says input
digests and the lock digest "are not yet threaded through the plan". A server
hit (`claimed: false, cached: true`) makes the runner adopt success and skip
execution; a hermetic success pushes a `job-result` object
`{jobInputHash, outputs: [], exit: 0}` so the next run with the same key hits.
`orun run` prints `✓ memoized <job> — cache hit, skipped execution`.

**Resume.** `ResumeJobs` seeds a run with the jobs that succeeded in a prior
execution of the same id (read from the object graph) and counts them as
"cached" in the summary.

**The run view and tokens.** Covered by `specs/orun-sync-efficiency` (SE1):
one view per run with `viewFreshFor`, the session token reused for 30 s.

### 2.6 Client-side tokens and metadata

- The OIDC-exchanged token is kept until shortly before expiry; the session
  token source re-reads its file on every call because `orun agent serve`
  rotates it (`remotestate/auth.go`).
- The repository token for grounded sessions rests at
  `$XDG_RUNTIME_DIR/orun/repo-token.json` (0600, spent 5 min before expiry);
  `git credential erase` is the one way to drop it (`agent/ground/token.go`).
- The integration registry per org: `<orunDir>/integrations/registry-<org>.json`
  plus an etag sidecar, a 24 h soft TTL for offline help, server-validated
  on every invocation (`integrationscli/cache.go`).
- MCP entitlement verdicts: 60 s, at most 64 workspaces, the map cleared at
  the cap (`platformmcp/entitlement.go`).
- Project and environment slug→id maps per CLI invocation
  (`configsurface/client.go`); the `RepoLink` in `~/.orun/config.yaml`; the
  catalog's pushed head ref (SE6).

### 2.7 Orun Cloud, and the CI workflow that drives orun

The edge caches a resolved actor for 30 s (`ACTOR_CACHE_TTL_SECONDS`) and an
org reference for 3600 s (immutable ids) or 60 s (slugs). The state worker
remembers an authorization grant for 30 s (`authz.ts`, up to 10 000 keys,
the whole map cleared at the cap), a plan's declared steps (64 entries,
first-in first-out) and a project's lease setting for 30 s.

sourceplane/orun-cloud's `ci.yml` is the production consumer of §2.1–2.2 and
documents, in its own comments, what it had to add around orun:

- **The dependency cache.** A `deps-cache` job fills orun's pnpm store once
  per lockfile hash and the lanes restore it (with a prefix fallback),
  because "with nothing restored, [a lane] downloads every package again:
  ~35s a lane, 177 lanes in a full run".
- **The store variable.** Each lane sets `npm_config_store_dir` by hand:
  "orun v2.61 exports `pnpm_config_store_dir` for it, which pnpm 10.12 does
  not read (only `npm_config_store_dir`), so each lane fell back to a store
  under its own per-job HOME and downloaded every package again."
- **The tool cache.** Each lane runs
  `ln -s "$RUNNER_TOOL_CACHE/node" ~/.orun/tool-cache/node`: "orun points
  each lane's setup-node at its own tool cache, which starts empty, so every
  lane downloaded node."
- **Browser and build caches.** Playwright's browsers and a lane's `.next`
  live under `~/.orun/actions/shared/` "because each lane's HOME is
  redirected per job"; the composition step copies `.next/cache` in from
  `ORUN_NEXT_CACHE_DIR` and back out.
- **The turbo remote cache.** `rharkor/caching-for-turbo` on dev lanes, so a
  `build` one lane ran is not rebuilt by the next; "test and typecheck are
  never cached".

Everything in that list is a cost orun could carry itself.

## 3. Measurements

| | What | Result |
|---|---|---|
| M1 | Composition cache, `dir` source (the examples' 408 KB package): plan, append a comment to one job file, plan again | 17 → 18 entries under `~/.orun/cache/compositions`; one full copy per edit; 1.3 MB after a day of work on this sandbox; nothing ever removed |
| M2 | `withLock` against a lock file left by a "crashed" process (mtime one hour old), 2 s timeout for the demonstration | blocks for the whole timeout, then `timed out waiting for action cache lock`; in production the timeout is 30 s and the outcome is the same on every later run |
| M3 | `stageJobWorkspace` + a file written to `TURBO_CACHE_DIR` + `Cleanup`, then a second run | `.orun/runs/<exec1>` survives holding `cache/`; the second run's `TURBO_CACHE_DIR` is `.orun/runs/<exec2>/cache/turbo`, absent; after both runs one exec directory remains |
| M4 | The action trees a job materializes (hardlinks, once per job per action) | `actions/setup-node`: 8.3 MB, 153 files; `azure/setup-helm`: 260 KB, 29 files; every one of the 38 jobs in the examples plan carries at least one setup action |
| M5 | The CI workflow's own figures (its comments) | a cold `pnpm install` ~35 s per lane, 177 lanes a full run; every lane downloaded Node before the symlink; each spot lane pays ~65 s to boot, so a cache miss of 35 s is half a boot |
| M6 | setup-helm's output on a hit and on a miss (`src/run.ts`) | `Helm tool version 'X' has been cached at …` is printed after `downloadHelm` returns, on both paths; setup-node prints `Found in cache @ …` on a hit and `Adding to the cache …` on a miss — neither is matched by the presenter |

## 4. Findings, ranked by effect

**F1 — memoization is keyed on too little and restores nothing.**
`jobInputHashFor` hashes the job's steps and env keys; the source tree the
steps run against and the composition lock are not in the key (the contract's
C5 lists them). A job labelled hermetic that builds its component is skipped
on the next run after the component changed, with the previous result adopted.
And because the pushed `job-result` carries `outputs: []`, a hit restores
nothing: the job is reported succeeded, its dependents start, and whatever it
would have produced is absent. The gate is opt-in (the label) and so the
blast radius is whoever turns it on; the feature is unsafe to turn on today.
*Severity: high when used; a correctness bug, not a performance one.*

**F2 — the action-cache lock is not crash-safe and the waits are too short.**
`withLock` creates `<dir>.lock` with `O_EXCL` and removes it on return; a
process killed mid-download (SIGTERM on a spot reclaim, an OOM, a Ctrl-C)
leaves the file behind, and every later `fetchRemoteAction` of that version
on that machine polls for 30 s and fails with "timed out waiting for action
cache lock" (M2). Independently, a second process waiting on a download in
progress also gives up at 30 s, and the download itself runs under the
engine's 30 s HTTP timeout, so a large action on a slow link (setup-node's
tarball is 8 MB uncompressed; a self-hosted runner behind a proxy) fails even
alone. *Severity: high on persistent self-hosted runners and developer
machines; invisible on ephemeral runners, which never reuse `~/.orun`.*

**F3 — refs never expire and the tarball is not fetched by the SHA.**
`refs/<slug>` is written once and honoured forever: `uses: org/action@main`
never moves again on that machine, and `@v4` never sees `v4.1`. GitHub
resolves on every run; orun's three-tier cache was built to stop API-rate
problems under concurrency (the docs say so) and overshot into never. Then
`downloadAndExtractTarball` asks for `/tarball/<ref>` rather than
`/tarball/<sha>`: between the resolve and the download the tag can move, and
the directory named by the resolved SHA then holds a different commit's code
— and keeps serving it under that name, with `.ready` written, for good. The
hardening GHA gets from pinning by SHA is lost here. *Severity: medium
(freshness) to high (integrity) on persistent machines.*

**F4 — the cross-job locks stop at the process boundary.** `toolCacheLocks`
and the Terraform slots live in process memory, while the directories they
guard are per machine. `orun run --job` in matrix mode runs one process per
job; a persistent self-hosted runner can host several lanes at once; both
share `~/.orun/tool-cache` and `~/.orun/actions/shared/terraform-plugins`
(every process takes slot 0). The failures #734 and #736 fixed are back in
exactly those deployments: a half-extracted Node tree, `text file busy`, a
corrupted plugin cache. The nimbus fleet (one ephemeral instance per lane)
does not hit this; a shared runner does. *Severity: medium, deployment-dependent.*

**F5 — the composition cache copies what it already has, races on fill, and
never prunes.** A `dir` source is on disk beside the intent; orun hashes it
(every plan, every file) and then copies it into `~/.orun/cache` so that the
package loader can read it from a digest-named directory. Every edit is a new
digest and a new copy (M1). The fill is unlocked: two processes resolving the
same new digest each build a temp directory, and each runs
`RemoveAll(cacheDir)` before its `Rename` — the second deletes the first's
live entry while the first process may be reading it, and the loader's "the
manifest exists" hit test cannot tell. Nothing removes old entries.
*Severity: medium; growth is bounded only by edits times package size.*

**F6 — a per-job HOME discards every tool cache orun does not know about.**
The per-job HOME is the right fix for global installs racing. Its side effect
is that every cache a tool keeps under `$HOME` or `$XDG_CACHE_HOME` — Go's
build and module caches, pip and uv, cargo's registry, gradle and maven,
Playwright's browsers, Cypress, puppeteer, yarn, bun, deno — is empty at job
start and deleted at job end. orun shares three (npm, pnpm store, Terraform
plugins). The CI workflow re-homes Playwright and Next under the shared
directory by hand and copies `.next/cache` in and out inside a composition
step. *Severity: medium; every non-Node toolchain pays full price per job.*

**F7 — the pnpm store variable is the wrong one.** The engine exports
`pnpm_config_store_dir` and `PNPM_CONFIG_STORE_DIR`; pnpm 10 reads
`npm_config_store_dir`. Without the workflow's manual env every lane installed
into a store under its per-job HOME (F6) — the 35 s per lane the workflow
measured. One line. *Severity: high until fixed, trivial to fix.*

**F8 — the tool cache cannot be the host's.** On a GitHub runner
`RUNNER_TOOL_CACHE` already holds Node, Python, Go and more; orun overwrites
the variable with `~/.orun/tool-cache` and ignores the host's. There is no
flag or environment variable to point orun at another directory
(`Options.ToolCacheDir` has no caller). The symlink in `ci.yml` is the
workaround. *Severity: medium; the same waste on every hosted runner.*

**F9 — the turbo cache is per run and left behind.** M3: the cache a run
builds is under its own exec directory, never found by the next run, and never
deleted (the exec directory survives because it is not empty). Locally, every
`orun run` rebuilds every turbo task, and the checkout grows by one cache per
run. In CI the remote cache papers over it for dev lanes. *Severity: medium
locally, low in CI.*

**F10 — setup actions run per job.** The lane-scoped step sharing (RE4)
stops at `run:` steps: `pnpm/action-setup` installs pnpm into the per-job
prefix and the prefix is deleted with the job; `setup-node` re-resolves and
re-links per job (M4: 153 hardlinks per job, cheap, but one action resolve
per process on a cold machine goes to the API twice). A lane of eight jobs
installs pnpm eight times. *Severity: low to medium (seconds per job).*

**F11 — the cache-hit figure is noise.** M6: the presenter counts a step as a
cache hit when its output matches `… has been cached at …` or
`Restoring … from cache`; setup-helm prints the first on both paths,
setup-node prints neither on a hit. The summary line `N cache hits` is
therefore wrong in both directions. *Severity: low (display).*

**F12 — nothing is bounded and nothing can be pruned.** `~/.orun/actions/<repo>/<sha>`
per version, `~/.orun/tool-cache` per tool version, the shared stores, the
composition cache (F5), the resolve memo (one file per source id), the
executions index and `.orun/runs/<exec>` (F9) grow without a size budget, an
age, or a command that lists or removes them; `objgc` prunes executions in
the object graph and nothing else. *Severity: low per week, certain over a
year on a developer machine.*

Smaller notes, not ranked: the state worker's authorization grant cache
clears the whole map at its cap (a burst re-authorizes everyone at once; an
LRU would evict one); the plan-step cache in the log handler is first-in
first-out; `.orun/runs/` is the only `.orun` path orun adds to `.gitignore`
although `.orun/component-tree.yaml` and `.orun/objectmodel` are derived too.

## 5. Proposals

**P1 — Make the lock crash-safe and the waits long enough (F2).** A lock
file carries the holder's pid and start time; a waiter that finds a lock
older than the download bound (or whose pid is gone) removes it and takes
over. The waiter's timeout equals the download bound. The download bound is
per phase, not per request: a connect timeout, then a body read deadline that
resets on progress (a `net/http` client with `ResponseHeaderTimeout` and a
reader that extends a deadline), so a slow but moving download completes.

**P2 — Fetch by SHA, expire refs (F3).** `downloadAndExtractTarball` asks
for `/tarball/<resolvedSHA>`, so the directory named by a SHA always holds
that commit. The on-disk ref entry records when it was resolved; a SHA ref is
immutable, a tag is re-resolved after 24 h, a branch after 1 h, and a resolve
that fails (offline, rate-limited) keeps serving the stale entry with one
note. `orun run --refresh-actions` forces the resolve. Within a process the
singleflight already keeps the API cost at one call per ref per run, which is
what the docs promised.

**P3 — Locks across processes (F4).** The tool-cache lock becomes a
`flock` on `<tool-cache>/.orun.lock` (the cliauth package already has the
`flockExclusive` helper), held around a setup action by every orun process on
the host. Terraform slots become lock files `terraform-plugins-<N>.lock`
taken with `flock` at job start: the lowest free slot on the host, not in the
process.

**P4 — Inherit the host's tool cache; a shared cache home; declared caches
(F6, F8).** When the environment orun inherits has `RUNNER_TOOL_CACHE` (a
GitHub runner) the engine uses it, falling back to `~/.orun/tool-cache`;
`ORUN_TOOL_CACHE` overrides either. The per-job HOME stays (global installs
must not race), but `XDG_CACHE_HOME` and the cache variables of the common
toolchains point at `~/.orun/actions/shared/<tool>`: `GOCACHE`, `GOMODCACHE`,
`PIP_CACHE_DIR`, `UV_CACHE_DIR`, `CARGO_HOME`'s registry, `GRADLE_USER_HOME`,
`PLAYWRIGHT_BROWSERS_PATH`, `CYPRESS_CACHE_FOLDER`, `PUPPETEER_CACHE_DIR`,
`YARN_CACHE_FOLDER`, `BUN_INSTALL_CACHE_DIR`, `DENO_DIR` — each one documented
as safe for concurrent use by its tool, which these are (content-addressed or
internally locked). A composition step may declare `caches:` (a name, a path
inside the job tree, the environment variable that names it), and orun keeps
each under the shared directory across jobs and runs — the generic form of
the `.next/cache` copy the workflow does by hand.

**P5 — The right pnpm variable (F7).** Export `npm_config_store_dir`
beside the two existing names. One line, and the workflow's workaround can go.

**P6 — The turbo cache per checkout; setup actions per lane (F9, F10).**
`TURBO_CACHE_DIR` becomes `<source>/.orun/cache/turbo`: turbo's cache is
content-addressed and safe under concurrent jobs, so one directory serves
every run and every lane on the checkout; `.orun/runs/<exec>` is removed at
run end unless `--keep-workspaces`. The lane-scoped sharing extends to the
leading `use:` setup actions: when every job of a lane runs the same setup
action with the same inputs, the first job runs it and its exported
environment (`PATH` additions, outputs) is applied to the others — the
pnpm install happens once per lane, as the install step already does.

**P7 — Fix the composition cache and give caches a budget (F5, F12).** A
`dir` source is loaded in place: the hash still validates (and feeds the
lock), but nothing is copied. Archive and OCI fills take a lock (P1's) and
never `RemoveAll` an entry that passes the hit test. A new `orun cache`
command lists every cache with its size and age (`ls`), removes what a
policy names (`prune`: entries older than N days, or not referenced by any
lockfile or plan seen in N days), and keeps the whole of `~/.orun` under a
budget (`gc --max-size`, least recently used first, by the directory's
atime or a `.last-used` stamp orun writes on a hit). `.orun/runs` and the
resolve memo join the same policy.

**P8 — Make memoization correct (F1).** The key gains the job's source
digest (the git tree hash of `job.Path` when the checkout is clean, else a
content hash of the staged tree), the composition lock digest and the input
artifact digests — the fields `JobInputHashInput` already has. A job that
declares outputs is memoized only when its `job-result` carries them and the
runner can restore them from the object store; otherwise the claim is sent
without the memo key. A hit is shown in `status` and the summary with the
result's digest, as the CLI doc already describes.

**P9 (optional) — Emulate `actions/cache` (§2.1).** A native handler for
`actions/cache`, `actions/cache/restore` and `actions/cache/save` (and the
`cache:` input of the setup actions, which call the same client) keyed by
`key` and `restore-keys`, backed by `~/.orun/actions/shared/actions-cache/`
locally and, with `--remote-state`, by the platform's object store. Then a
composition written for GitHub works unchanged under orun on any runner,
and the deps-cache job in `ci.yml` becomes a cache key.

**On the measurement harness.** The figures above came from throw-away tests
against unexported functions; the milestones should land them as real tests
(a stale lock is reclaimed; a second process waits out a download; the ref
file carries an age; the turbo directory is found by the next run; the memo
key changes when a source file does).

## 6. Expected effect

- **CI (orun-cloud):** the three workarounds in `ci.yml` go (the symlink, the
  store variable, the Next copy) with no change in lane time; a lane that
  today misses the store (a new lockfile before `deps-cache` has filled it)
  stops paying 35 s. Nothing else changes on ephemeral runners, which is
  the point: the fixes are for the machines that keep `~/.orun`.
- **Persistent self-hosted runners and developer machines:** a crash no
  longer poisons an action for 30 s per run; refs move again; two orun
  processes on a host stop corrupting the tool cache; Go, Python, Rust and
  JVM jobs find their caches warm; `orun run` on a checkout reuses turbo's
  cache across runs (a rebuild-free second run of the examples' 38 jobs
  is the measurable target); a lane installs pnpm once; `~/.orun` stops
  growing without limit and can be inspected.
- **Correctness:** memoization can be turned on; an action directory holds
  the commit it is named after.

## 7. Milestones

| Milestone | Scope | Where | Done when |
|---|---|---|---|
| **CE0** | P1 (crash-safe lock, waits), P5 (store variable), the hit signal (F11: a step is a hit when the tool version's directory existed before it ran), `.orun/runs/<exec>` removed at run end | `internal/gha/fetch.go`, `engine.go`, `internal/runner/presenter.go`, `runner.go` | a stale lock is reclaimed in a test; a slow download completes; `ci.yml` can drop `npm_config_store_dir` |
| **CE1** | P2: tarball by SHA, ref age and TTL, `--refresh-actions` | `internal/gha/fetch.go`, `cmd/orun` | `uses: x@main` updates within an hour on a warm machine; the SHA directory's content matches its name |
| **CE2** | P3: `flock` for the tool cache and the Terraform slots | `internal/gha/engine.go`, `internal/cliauth` lock helpers | two processes on one host run setup-node concurrently without a half-written tree |
| **CE3** | P4: host tool cache inherited, `ORUN_TOOL_CACHE`, shared cache home and toolchain variables, `caches:` on composition steps | `internal/gha/engine.go`, `internal/model`, docs | `ci.yml` drops the symlink and the Next copy; a Go job's second run uses its build cache |
| **CE4** | P6: turbo per checkout, run directory cleanup, setup actions once per lane | `internal/runner/workspace.go`, `lane_setup.go`, `runner.go` | the second run of the examples plan rebuilds nothing turbo cached; a lane installs pnpm once |
| **CE5** | P7: `dir` sources in place, locked fills, `orun cache ls / prune / gc` | `internal/composition`, `cmd/orun/command_cache.go` | M1 shows no new copy per edit; `~/.orun` stays under a budget |
| **CE6** | P8: the full memo key, outputs in the result, hits surfaced | `internal/statebackend`, `internal/runner`, the platform's result object | a labelled job misses after its source changes and restores its outputs on a hit |
| **CE7** | P9: native `actions/cache` | `internal/gha` | a composition using `actions/cache` restores under orun without the GitHub service |

CE0 is a day and removes the sharpest edges; CE1–CE2 are each small and
testable; CE3–CE5 are where the time goes back to users; CE6 is a contract
change on both sides; CE7 is a feature in its own right.

## 8. Reproducing the measurements

```sh
# M1 — composition cache growth (a scratch copy of the examples)
cp -r examples /tmp/ex1 && cd /tmp/ex1 && git init -q && git add -A && git commit -qm init
ls ~/.orun/cache/compositions | wc -l
orun plan -o /tmp/plan1.json
printf '\n# edit\n' >> compositions/compositions/cloudflare-pages-terraform/jobs/cloudflare-pages-terraform-verify-reconcile.yaml
orun plan -o /tmp/plan2.json
ls ~/.orun/cache/compositions | wc -l      # one more entry, a full copy

# M2 — a stale lock (throw-away test in internal/gha; not committed)
#   write <dir>.lock with an old mtime, call withLock(<dir>.lock, 2*time.Second, …)
#   → "timed out waiting for action cache lock" after the whole timeout

# M3 — the per-run turbo cache (throw-away test in internal/runner)
#   stageJobWorkspace(src, "exec1", "job-a", …); write into ws.Env("exec1")["TURBO_CACHE_DIR"];
#   ws.Cleanup() → .orun/runs/exec1/cache survives; a second exec's TURBO_CACHE_DIR is elsewhere

# M4 — action tree sizes
git clone --depth 1 https://github.com/actions/setup-node && du -sh --exclude=.git setup-node
```
