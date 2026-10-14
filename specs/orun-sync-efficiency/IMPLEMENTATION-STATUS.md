# orun-sync-efficiency — implementation status

| Milestone | Status | What shipped |
|---|---|---|
| **SE0** | ✅ #754 | A lapsed long-poll is not an event: `eventWait.wait` reports a wake only when the head advanced and a lapse as time spent, so `awaitRemoteUpstreams` backs off 2, 4, 8, 15 s with one state read per long-poll and `waitForJobRunnable` never sleeps a second interval; `orun status`/`logs`/`tui` build the coordination backend so `status --watch` long-polls (`ORUN_COORDINATION=legacy` keeps the plain one). Measured on the 4-job lanes scenario: 24 wait requests instead of 75 |
| **SE1** | ✅ #755 | The per-run view in `CoordBackend` (`runView`): the folded state, the dependency graph (seeded by `InitRun` from the plan the process holds, else the plan object fetched once) and the presenter's timestamps, advanced by `FoldFrom` with the events each long-poll or delta read returns; `LoadRunState` and `RunnableJobs` serve from it (synced by a delta read unless within `viewFreshFor` of the last sync); `WaitForRunEvents` folds what it returns and its head read starts from the view's seq. The plan blob no longer carries `CreatedAt`, so its digest is stable across processes. `CoordClient` on the remotestate client's keep-alive transport with a 45 s timeout (`CoordinationHTTPClient`); `SessionTokenSource` reuses the token it read for 30 s |
| **SE2** | ✅ #756 (client) | orun-cloud (`state-worker`, branch `claude/keen-archimedes-xeeemo`, awaiting push): `GET …/runs/{id}/state` answers the shard's `RunFoldState` (jobs, frontier, lastSeq, timings) after the run-read authorization; `/frontier` served by the DO's `/frontier` instead of its whole fold; a held `/log?wait=` ticks on the one `seq` key and lists events only once it moved; an allow from membership + policy remembered per isolate for 30 s per (actor, org, project, action), denials never; api-edge counts `/state` in the coordination rate family; contract §2 names the route. orun: `CoordClient.ReadState`; the view's first sync is the state read (plus the plan object once, when the process did not seed the plan), with the log as the fallback on a 404 (an older platform, or a run the shard does not back), remembered per run; vendored contract re-checksummed |
| **SE3** | ✅ #759 (client) | orun-cloud (same branch, awaiting push): the shard keeps a progress mark (the newest event that is not a lease renewal); `GET …/log?wait=&wake=progress` holds through renewals and lapses with them on the page and `head.progressSeq`; the projection asks the shard's head first and writes nothing for a run with no progress past the read model's seq, else the run row and, in one `jsonb_to_recordset` statement, the rows of the jobs the progress touched (`/delta?after=`); the minute sweep counts the runs it skipped. orun: `CoordClient.ReadLogWait`, `CoordBackend.WaitForRunProgress`; the runner's waits (`eventWait`) use the progress wait and treat a renewal-only lapse as no news with the cursor moved; `status --watch` keeps the plain wait; contract re-vendored |
| **SE4** | 🟡 cloud only | orun-cloud (same branch, awaiting push): the log append reads its next seq with one `MAX(seq)` (`StateRepository.nextLogChunkSeq`) instead of listing every chunk of the job; the job log read and the seal on completion fetch chunks from R2 eight at a time in order (`mapBounded`); `…/runs/{run}/logs/{job}` rides the per-run coordination rate family at the edge instead of the 60/min `state` family |
| **SE5** | ✅ #760 (client) | orun: `logpipe` queues a block and returns; a worker per job uploads in order beside the run (`MaxInFlight` 4 across jobs), a `Retry-After` on a 429 is honoured on the spot (`APIError.RetryAfterHint`), the spill and the drain at exit unchanged; `logs --follow` asks the platform to hold each tail read (`?wait=`, `TailJobLogWait`, `jobLogWaiter`) and polls only on a platform without it. orun-cloud (same branch, awaiting push): `GET …/logs/{job}?wait=` ticks on the job's `MAX(seq)` and row until a chunk lands, the job ends or the wait lapses. Chunk coalescing deferred: one append per step already carries the step's coordinate, and merging steps needs the `steps[]` contract change |
| **SE6** | ✅ #762 | orun: `catalog push` remembers the snapshot it advanced each head to (`catalogs/pushed/<scope>` local ref) and skips the advance for the same snapshot (`--force` re-advances); `work sync` lists an epic's tasks once (`ListTasksWhere{Epic}`, a miss still read on its own before a create) and its docs once (`ListEpicDocs`), skipping a doc whose `gitSha` and path the platform already holds; an OCI composition source the lockfile pins is loaded from the cache at its locked digest, or pulled by digest, without resolving the tag |

## Where the orun-cloud halves stand

The server halves of SE2, SE3, SE4 and SE5 are four signed-off commits on
the `claude/keen-archimedes-xeeemo` branch of sourceplane/orun-cloud
(`state-worker`: the state read, the DO frontier, the seq-keyed long-poll
tick and the 30 s authorization grant cache; the progress mark,
`wake=progress`, `/delta` and the projection by difference; the log
append's tail from `MAX(seq)`, parallel chunk reads and the log route on
the coordination family; the held log read). Their suites (state-worker,
contracts, db on pglite), typecheck and lint pass locally. They await a
push to that repository. Every orun change above degrades to the previous
behaviour against a platform without them (a 404 on the state read, a
long-poll that wakes on every event, a log read that answers at once), so
the CLI can ship first.

## Closing verification on main (after SE6, #762)

Against the request-logging stand-in backend from the spec (given the
state read, the progress wake and the held log read), the 4-job product,
45 s jobs:

| Scenario | Requests | Bytes down | Before the series |
|---|---|---|---|
| two lanes, with `status --watch` and `logs --follow` alongside | 50 (15 event long-polls, 6 tail reads) | 16.8 KB | 207 requests, lanes with `status --watch` alone |
| matrix, one process per job | 61 (26 event long-polls) | 19.5 KB | 137 requests, 101 KB |

The remaining long-polls are the waits lapsing on their 2, 4, 8, 15 s
schedule; the gain scales with the run, since no read grows with the event
log any more.
