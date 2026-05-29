# `orun github` log-pull UX review

Walkthrough date: 2026-05-29
Scope: end-to-end exercise of the four `orun github` subcommands (`runs`, `status`, `pull`, `logs`) against a real workflow run on PR #142 (branch `happy-patch-113`, run `26606158847`).

This doc captures both **bugs found and fixed during the walkthrough** and **friction the user will hit that is not yet a bug**. It is meant to be paired with `website/docs/cli/orun-github.md` (now updated) and the internal `docs/github-artifacts.md`.

---

## 1. What works well

| Capability | Verdict | Evidence |
|---|---|---|
| `orun github runs` — Level 1 listing | ✅ Fast, shows shard counts inline | `26606158847 [2 shards]` rendered in <1s |
| `orun github runs --details` — Level 2 | ✅ Parses shard name into `role/component/env/job` cleanly | Correctly attributed `api-edge-worker.dev.verify-deploy` |
| `orun github pull` end state | ✅ Produces `.orun/executions/<exec-id>/{metadata,plan,state,github,shards}.json` + `logs/` ready for offline `orun status` / `orun logs` | Tested on `/tmp/orun-pull-test` |
| Hydrated `orun status` / `orun logs` | ✅ Renders identically to a local run — same TUI, same colors, same progress bar | One-shot, no extra flags needed |
| Artifact-name based no-download status | ✅ The naming convention (`orun.v1.<exec-id>.<role>.<suffix>.<status>`) carries enough info that `runs` / `status` can answer “did this pass?” with zero shard downloads | Killer feature; should be marketed harder |

The artifact-name convention is the **standout design decision**. It turns GitHub Actions artifact metadata into a queryable index without paying network for shard download — every other "let me peek at CI" tool I've used downloads the full log to answer the same question.

---

## 2. Bugs found and fixed in this walkthrough

### 2a. `orun github pull --orun-dir` silently lost the user's path (FIXED)

**Symptom**
```
$ orun github pull --run-id 26606158847 --orun-dir /tmp/orun-pull-test
✕ hydrate execution: failed to write metadata:
  open /tmp/.orun/executions/gh-26606158847-1-896da6b09c29/metadata.json:
  no such file or directory
```

**Root cause** — `cmd/orun/command_github.go:353`
```go
orunDir := githubPullOrunDir          // user passed "/tmp/orun-pull-test"
if orunDir == "." {                   // only the default got normalized
    orunDir = filepath.Join(storeDir(), state.OrunDir)
}
// ...
runbundle.Hydrate(..., orunDir)       // passed "/tmp/orun-pull-test"
```
…and inside `Hydrate`:
```go
store := state.NewStore(filepath.Dir(orunDir))   // /tmp
// state.NewStore re-appends ".orun" → /tmp/.orun/
```
So with `--orun-dir /tmp/orun-pull-test`, the exec dir gets created at `/tmp/orun-pull-test/executions/<id>/` but the state-store writes metadata to `/tmp/.orun/executions/<id>/metadata.json`. That directory doesn’t exist → ENOENT.

**Fix shipped** — `cmd/orun/command_github.go`
```go
orunDir := githubPullOrunDir
if orunDir == "" { orunDir = "." }
// Normalize: --orun-dir is a working directory; .orun/ lives inside it.
// Accept either the working dir or a path that already ends in .orun (back-compat).
if filepath.Base(orunDir) != state.OrunDir {
    orunDir = filepath.Join(orunDir, state.OrunDir)
}
```
And the flag description was updated from “Target `.orun` directory” to “Target working directory (a `.orun/` subdirectory is created/used inside it)”.

### 2b. `orun github status` ignored every resolution flag (FIXED)

**Symptom**
```
$ orun github status --latest
✕ unknown flag: --latest
$ orun github status --sha <sha>
✕ unknown flag: --sha
$ orun github status --exec-id ...
✕ unknown flag: --exec-id
```

**Root cause** — `registerGithubCommand` never registered any flags on `githubStatusCmd`, yet `runGithubStatus` reads `githubLogsRunID/ExecID/SHA/Branch/Failed`. So the command could only ever resolve via "latest run on current branch" (the implicit fallback), and even then *only* if you happened to be on a branch with at least one matching run. Any explicit targeting failed at cobra’s flag-parser before reaching the run function.

**Fix shipped** — added the six standard resolution flags (`--run-id`, `--exec-id`, `--sha`, `--branch`, `--latest`, `--failed`) to `githubStatusCmd`, reusing the same `githubLogs*` vars the handler already reads from. Status, logs, and pull now share a single, consistent flag vocabulary.

---

## 3. Open friction — not bugs, but cost real time

### 3a. `--sha` requires a full 40-char SHA, silently

```
$ orun github status --sha 061fa5d
✕ resolve workflow run: no runs found for SHA 061fa5d
$ orun github status --sha 061fa5d0d07cefbfe2ca3482aa64f1916208e576
✓ Run 26606158847 ...
```
GitHub's REST API filters runs by exact SHA. Every other git-adjacent CLI (`gh`, `git`, `git-branchless`) accepts short SHAs and disambiguates. Users will type `--sha $(git rev-parse --short HEAD)` instinctively and hit this.

**Suggested fix:** in `github.ResolveRun`, if `len(sha) < 40 && len(sha) >= 7`, expand via `git rev-parse <sha>` (offline) or `client.RepoCommit(ctx, sha)` (one extra API call). Doc’d the limitation in `orun-github.md` in the meantime.

### 3b. `--job` is a substring match against the artifact name, not a logical job ID

```
$ orun github logs --job api-edge-worker.dev.verify-deploy
✕ no artifacts matching job "api-edge-worker.dev.verify-deploy" in run 26606158847
$ orun github logs --job job_896da6b                    # works
```
The user just got `api-edge-worker.dev.verify-deploy` from `orun github runs --details`. They will not guess `job_896da6b` (the internal UID). This is the **single biggest discoverability cliff** in the flow.

**Suggested fix:** match `--job` against `ParsedShard.Component + "." + Env + "." + JobName` *first*, then fall back to substring against the raw artifact name. The data is already loaded — `s.Parsed` has all of it. Roughly 10 lines around `command_github.go:518`.

### 3c. `--latest` on `pull` / `logs` is undocumented as "latest on current branch"

A naked `--latest` without `--branch` falls through to the default resolver, which uses the *current local git branch*. From a detached checkout, an unrelated PR's branch, or CI itself, this surprises you. Either:
- require `--branch` when `--latest` is set, OR
- print the resolved `(run=…, branch=…, sha=…)` triple to stderr before download.

### 3d. Output formatting is space-separated with no headers

```
$ orun github runs --branch happy-patch-113 --limit 3
26606158847 [2 shards]
26605783452 [1 shards]
```
Compared to `gh run list`, which gives status / title / branch / event / workflow / age in aligned columns, the `orun` output is hard to skim. The information is there for `--details`, but the default view loses run conclusion, event, age, and SHA. A two-line `STATUS  RUN ID  BRANCH  SHA  AGE  SHARDS` header + rows would change the feel substantially.

### 3e. Zero-shard runs are confusing, not informative

When run `26605783452` planned zero jobs (the dummy commit didn’t touch a `discovery.root`), `runs` shows it as `[1 shards]` (the plan shard) — but there is no signal that "the plan ran and decided to do nothing." A user staring at this list will assume CI is broken. Two cheap improvements:

1. `orun github runs --details` should surface `plan: 0 components × N envs → 0 jobs` from the plan manifest (it’s already downloaded).
2. `orun github status` should print a one-liner: `0 jobs planned (changed-only mode, no components touched)`.

### 3f. Progress signal during `pull`

```
Downloading 2 shard(s) for gh-26606158847-1-896da6b09c29...
✓ hydrated gh-26606158847-1-896da6b09c29  completed  1/1 shards
```
Fine for 2 shards, brutal for 50. Add a per-shard line (`[1/2] plan.a1b2c3 (12 KB)…`) or a progress bar identical to the `orun status` one.

### 3g. `--orun-dir` semantics are now consistent — but document the trap

The old default `.` resolved to `cwd/.orun/`, but `--orun-dir /tmp/foo` resolved to `/tmp/.orun/` (the bug). The new behavior treats `--orun-dir` uniformly as the **parent working directory** and creates/uses `.orun/` inside it. To not break existing scripts that already passed `.../.orun`, the normalizer accepts that too. Documented in the CLI reference. Worth a CHANGELOG entry.

### 3h. `orun github logs` dumps raw text to stdout with no exec-id banner

When you tail a single job’s logs you lose the context of *which* run and *which* job. Add a stderr banner like the `orun logs` view does (`✓ api-edge-worker  1 env completed done  (19s)`).

---

## 4. What changed in this walkthrough

Code:
- `cmd/orun/command_github.go` — `runGithubPull` normalizes `--orun-dir` consistently; `registerGithubCommand` adds the six standard resolution flags to `githubStatusCmd`.

Docs (`website/docs/cli/orun-github.md`):
- Corrected `--orun-dir` description and added an explicit "writes to `<dir>/.orun/`" example.
- Added a SHA-length call-out (full 40 chars required).
- Expanded the resolution-order list to include `--branch` and `--latest`.
- Filled in `orun github status` with its newly-registered flag set and four examples.
- Added a `--job`-matching call-out (substring match, not structured) under the logs flag table.
- Replaced the misleading short-SHA example (`abc123`) in the `runs` example block.

No changes needed in: `docs/github-artifacts.md` (already accurate at the design-doc level), `website/docs/{architecture,concepts,getting-started}` (no `orun github` references that contradicted current behavior).

---

## 5. Suggested priority for follow-ups

| # | Item | Effort | Impact |
|---|---|---|---|
| 1 | 3b: structured `--job` matching | XS (~15 LoC) | High — current behavior is a discoverability cliff |
| 2 | 3e: zero-job-plan messaging | S | High — "CI is broken" false alarm |
| 3 | 3a: short SHA expansion | S | Medium — pure quality of life |
| 4 | 3d: aligned column output | S | Medium — first impression / demo polish |
| 5 | 3f: per-shard pull progress | S | Low until shard counts grow |
| 6 | 3c: explicit resolution echo | XS | Low but catches detached-HEAD foot-guns |
| 7 | 3h: logs context banner | XS | Low |

Items 1, 2, 5 of this list (the bugs + structured `--job` match + zero-job messaging) would together take the flow from "works once you know it" to "obvious on first contact."
