# orun-sync-efficiency — implementation status

| Milestone | Status | What shipped |
|---|---|---|
| **SE0** | ✅ #754 | A lapsed long-poll is not an event: `eventWait.wait` reports a wake only when the head advanced and a lapse as time spent, so `awaitRemoteUpstreams` backs off 2, 4, 8, 15 s with one state read per long-poll and `waitForJobRunnable` never sleeps a second interval; `orun status`/`logs`/`tui` build the coordination backend so `status --watch` long-polls (`ORUN_COORDINATION=legacy` keeps the plain one). Measured on the 4-job lanes scenario: 24 wait requests instead of 75 |
| **SE1** | 🟡 in review | The per-run view in `CoordBackend` (`runView`): the folded state, the dependency graph (seeded by `InitRun` from the plan the process holds, else the plan object fetched once) and the presenter's timestamps, advanced by `FoldFrom` with the events each long-poll or delta read returns; `LoadRunState` and `RunnableJobs` serve from it (synced by a delta read unless within `viewFreshFor` of the last sync); `WaitForRunEvents` folds what it returns and its head read starts from the view's seq. The plan blob no longer carries `CreatedAt`, so its digest is stable across processes. `CoordClient` on the remotestate client's keep-alive transport with a 45 s timeout (`CoordinationHTTPClient`); `SessionTokenSource` reuses the token it read for 30 s |
| **SE2** | ⬜ | orun-cloud: `GET …/runs/{id}/state`; `/frontier` from the DO's frontier; long-poll tick on `storage.get("seq")`; authorization context cached per (actor, org, action) |
| **SE3** | ⬜ | orun-cloud: lease renewals out of the event log (or filtered from waits); projection by difference; the minute sweep skips unchanged runs |
| **SE4** | ⬜ | orun-cloud: log append seq from a counter; bounded parallel chunk reads; `/logs/{job}` on a per-run rate family |
| **SE5** | ⬜ | orun: asynchronous log pipeline with per-job queues, 429 retry, chunk coalescing; `logs --follow` without polling |
| **SE6** | ⬜ | orun: catalog head advance skipped when unchanged; batched task lookups; docs pushed by hash; pinned OCI sources resolved from the lockfile |
