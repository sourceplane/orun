#!/usr/bin/env python3
"""Sync-cost model for an orun run against orun-cloud (specs/orun-sync-efficiency).

Counts the HTTP requests, bytes and server-side operations one run costs,
from the client loops as they are coded (cmd/orun/command_run.go
performRemoteJobClaim/waitForJobRunnable, cmd/orun/run_lane.go
awaitRemoteUpstreams, cmd/orun/command_status.go) and the server costs as they
are coded (apps/state-worker: run-coordinator.ts, coordination-native.ts,
handlers/logs.ts, projection-sweep.ts; apps/api-edge: rate-limit.ts).

Validated against the measured runs in README.md §3 (the 4-job product on a
request-logging fake backend): the model reproduces A (23 requests), B (137)
and C within a few requests. Then it scales the same loops to the shapes a
platform runs and applies the proposals of §5 one by one.

    python3 model.py            # the tables in README.md §3 and §6
    python3 model.py --json     # the same, machine readable
"""
import json
import math
import sys

# Constants as coded (file:line in README.md §2).
HEARTBEAT = 20          # heartbeatIntervalSeconds the server grants
LEASE = 60              # leaseSeconds
LONG_POLL = 15          # eventLongPollSeconds (cmd/orun/run_lane.go)
EVENT_BYTES = 330       # measured: a wire event with its envelope and payload
SNAPSHOT_EVERY = 64     # run-coordinator.ts
LOG_WAIT_TICK = 0.25    # LOG_POLL_INTERVAL_MS: one storage.list per tick while a long-poll holds


class Shape:
    def __init__(self, name, jobs, levels, job_seconds, steps=4, plan_bytes=1747,
                 mode="matrix", lanes=None, ci=True, per_level=None):
        self.name, self.jobs = name, jobs
        self.T, self.steps, self.plan_bytes = job_seconds, steps, plan_bytes
        self.mode, self.ci = mode, ci
        # jobs per level (level k starts when level k-1 is done): given, or evenly spread
        if per_level is None:
            per_level = [jobs // levels + (1 if i < jobs % levels else 0) for i in range(levels)]
        self.per_level = per_level
        self.levels = len(per_level)
        self.lanes = lanes if lanes is not None else levels  # one lane per level (the docs recipe)

    def processes(self):
        return {"plan": 1, "matrix": self.jobs, "lanes": self.lanes}[self.mode]


def event_times(shape, level, fixes):
    """Times (from run start) at which the run's log grows while `level` waits:
    the heartbeats of every running job of the levels below (every HEARTBEAT s),
    and each job's claim and completion. With P2 only claims and completions."""
    out = []
    for k in range(level):
        start = k * shape.T
        for j in range(shape.per_level[k]):
            out.append(start + 0.1 * j)            # its claim
            out.append(start + shape.T + 0.1 * j)  # its completion
            if not fixes.get("quiet_heartbeats"):
                t = start + HEARTBEAT
                while t < start + shape.T:
                    out.append(t)
                    t += HEARTBEAT
    return sorted(set(out))


def simulate_wait(shape, level, fixes, kind):
    """One waiting job's loop, as coded.

    kind="matrix": waitForJobRunnable (cmd/orun/command_run.go). Each iteration
    long-polls for min(poll, 15) s, then reads the frontier and (deps > 0) the
    run state; poll doubles to 15 s after every iteration. (Before SE0 a lapse
    was reported as an event; for this loop that made no difference to the
    request count, so the F1 row equals today's for the matrix shape.)
    kind="lane": awaitRemoteUpstreams (cmd/orun/run_lane.go). Reads the run state,
    then long-polls for min(delay, 15) s; `continue` on a reported event (delay
    unchanged), else sleeps delay and doubles it to 15 s.

    Today eventWait.wait reports a TIMEOUT as an event (F1), so every long-poll
    return re-reads the state and a lane's delay never grows past 2 s. With
    fix_f1 a timeout is a timeout. With incremental_state the long-poll's own
    events are folded locally, so a return with events costs no extra read and
    a timeout costs nothing but the next long-poll.

    Returns (long_polls, state_reads, frontier_reads, event_wakes, timeouts)."""
    wait_until = level * shape.T
    events = event_times(shape, level, fixes)
    f1 = fixes.get("fix_f1") or fixes.get("incremental_state")
    t, step = 0.0, 2.0
    long_polls = state_reads = frontier_reads = event_wakes = timeouts = 0
    ev_i = 0
    if kind == "matrix":
        long_polls += 1  # newEventWait reads the head (a /log from=0)
    else:
        state_reads += 1  # the first LoadRunState
    while t < wait_until:
        lp = min(step, LONG_POLL)
        while ev_i < len(events) and events[ev_i] <= t:
            ev_i += 1
        next_ev = events[ev_i] if ev_i < len(events) else math.inf
        long_polls += 1
        if next_ev <= t + lp:
            t = next_ev + 0.05
            event_wakes += 1
            woke = True
        else:
            t += lp
            timeouts += 1
            woke = False
        reported = woke or not f1  # today a timeout is reported as an event (F1)
        if kind == "matrix":
            if fixes.get("incremental_state"):
                pass  # the long-poll's events are folded locally; nothing else to read
            else:
                # the long-poll is the sleep (SE0); a lapse still re-reads, on
                # the 2, 4, 8, 15 s schedule, until SE1 folds the events locally
                frontier_reads += 1
                state_reads += 1
            step = min(step * 2, 15.0)
        else:
            if reported:
                if not fixes.get("incremental_state"):
                    state_reads += 1
                continue  # delay unchanged, as coded
            if not fixes.get("incremental_state"):
                t += step  # sleepOrDone(delay) on a timeout
                state_reads += 1
            step = min(step * 2, 15.0)
    return long_polls, state_reads, frontier_reads, event_wakes, timeouts


def cost(shape, fixes=None):
    f = fixes or {}
    N, T, S = shape.jobs, shape.T, shape.steps
    procs = shape.processes()
    req = {}
    bytes_down = 0
    bytes_up = 0

    # ── setup per process ──
    req["oidc exchange"] = 2 * procs if shape.ci else 0
    req["objects/missing"] = procs
    # the plan blob is re-uploaded by every process unless its digest is stable (P4)
    req["plan PUT"] = 1 if f.get("stable_plan_digest") else procs
    bytes_up += req["plan PUT"] * shape.plan_bytes
    req["runs POST"] = procs

    # ── per job ──
    hb = int(T // HEARTBEAT)
    req["claim"] = N
    req["heartbeat"] = N * hb
    req["log append"] = N * S if not f.get("batched_logs") else N
    req["complete"] = N

    # ── event log ──
    events_per_job = 3 + (0 if f.get("quiet_heartbeats") else hb)
    log_events_final = 2 + N * events_per_job
    log_bytes_final = log_events_final * EVENT_BYTES

    # ── waits ──
    long_polls = state_reads = frontier_reads = 0
    reread_bytes = 0
    if shape.mode in ("matrix", "lanes"):
        kind = "matrix" if shape.mode == "matrix" else "lane"
        for level in range(1, shape.levels):
            lp, sr, fr, _, _ = simulate_wait(shape, level, f, kind)
            waiters = shape.per_level[level]  # a lane job waits in its own loop too
            long_polls += lp * waiters
            state_reads += sr * waiters
            frontier_reads += fr * waiters
            if f.get("incremental_state"):
                reread_bytes += sr * waiters * (N * 120)  # a fold snapshot, ~120 B per job
            else:
                log_now = EVENT_BYTES * (2 + sum(shape.per_level[:level]) * events_per_job)
                reread_bytes += sr * waiters * (log_now + shape.plan_bytes)
        if shape.mode == "matrix":
            req["claim"] += sum(shape.per_level[1:])  # a waiting job's first claim is rejected
    req["log long-poll"] = long_polls
    req["frontier"] = frontier_reads
    if f.get("incremental_state"):
        req["state read"] = state_reads
    else:
        req["log from=0"] = state_reads
        req["plan object GET"] = state_reads
    bytes_down += reread_bytes

    total = sum(req.values())

    # ── server side ──
    srv = {}
    verbs = req["claim"] + req["heartbeat"] + req["complete"]
    srv["edge rate-limit DO ops (2 per write)"] = 2 * (verbs + req["log append"] + req["runs POST"] + req["plan PUT"] + req["objects/missing"])
    srv["DO storage puts (events + seq)"] = 2 * verbs + (log_events_final // SNAPSHOT_EVERY)
    # a long-poll lists storage every 250 ms while it holds
    srv["DO storage lists (long-poll ticks)"] = int(long_polls * (LONG_POLL / LOG_WAIT_TICK) * 0.6)
    srv["DO fold serialisations (/frontier, projections)"] = req["frontier"] + 2 * N
    proj_jobs = (1 if f.get("projection_diff") else N)
    srv["Postgres statements: projections (claim+complete)"] = 2 * N * (2 + proj_jobs)
    minutes = math.ceil(shape.levels * T / 60)
    sweep_per_min = 0 if (f.get("quiet_heartbeats") or f.get("projection_diff")) else (1 + N)
    srv["Postgres statements: minute sweep"] = minutes * (sweep_per_min + 2)
    appends = req["log append"]
    chunk_rows_read = N * sum(range(S)) if not f.get("append_seq_counter") else 0
    srv["Postgres statements: log appends"] = appends * 5
    srv["Postgres rows read by appends (chunk listing)"] = chunk_rows_read
    srv["R2 ops: log chunks (put + seal list/get/put)"] = appends + N * (2 + S)
    srv["R2 ops: plan object GETs served"] = 0 if f.get("incremental_state") else state_reads

    return {"shape": shape.name, "mode": shape.mode, "processes": procs, "requests": req,
            "total_requests": total, "bytes_down": bytes_down, "bytes_up": bytes_up,
            "event_log_events": log_events_final, "event_log_bytes": log_bytes_final,
            "server": srv}


FIXES = [
    ("today", {}),
    ("F1 fixed: a timeout is not an event", {"fix_f1": True}),
    ("+P1 incremental state: fold the long-poll's events, one /state read", {"fix_f1": True, "incremental_state": True}),
    ("+P2 heartbeats are not log events", {"fix_f1": True, "incremental_state": True, "quiet_heartbeats": True}),
    ("+P3 stable plan digest (one upload per run)", {"fix_f1": True, "incremental_state": True, "quiet_heartbeats": True, "stable_plan_digest": True}),
    ("+P4-P6 projection diff, append counter, batched logs", {"fix_f1": True, "incremental_state": True, "quiet_heartbeats": True, "stable_plan_digest": True, "projection_diff": True, "append_seq_counter": True, "batched_logs": True}),
]

SHAPES = [
    Shape("A measured: 4 jobs, levels 1/2/1, 45 s, one process", 4, 3, 45, steps=1, mode="plan", ci=False, per_level=[1, 2, 1]),
    Shape("B measured: 4 jobs, levels 1/2/1, 45 s, matrix", 4, 3, 45, steps=1, mode="matrix", ci=False, per_level=[1, 2, 1]),
    Shape("C measured: 4 jobs, 2 lanes (1 cross-lane waiter), 45 s", 4, 2, 45, steps=1, mode="lanes", lanes=2, ci=False, per_level=[3, 1]),
    Shape("examples/: 38 jobs, 4 levels, 10 min, matrix", 38, 4, 600, steps=4, plan_bytes=40_000, mode="matrix"),
    Shape("examples/: 38 jobs, 4 lanes, 10 min", 38, 4, 600, steps=4, plan_bytes=40_000, mode="lanes"),
    Shape("fleet: 200 jobs, 5 levels, 20 min, matrix", 200, 5, 1200, steps=5, plan_bytes=200_000, mode="matrix"),
    Shape("fleet: 200 jobs, 5 lanes, 20 min", 200, 5, 1200, steps=5, plan_bytes=200_000, mode="lanes"),
]


def fmt_bytes(b):
    for unit in ("B", "KB", "MB", "GB"):
        if b < 1024 or unit == "GB":
            return f"{b:.0f} {unit}" if unit == "B" else f"{b:.1f} {unit}"
        b /= 1024


def main():
    if "--json" in sys.argv:
        out = [{"fix": name, **cost(s, fx)} for s in SHAPES for name, fx in FIXES]
        json.dump(out, sys.stdout, indent=1)
        return
    for s in SHAPES:
        print(f"\n== {s.name} ({s.mode}, {s.processes()} process(es))")
        print(f"{'fix':68} {'requests':>9} {'down':>10} {'DO puts':>8} {'DO lists':>9} {'PG stmts':>9} {'RL ops':>7}")
        for name, fx in FIXES:
            c = cost(s, fx)
            srv = c["server"]
            pg = sum(v for k, v in srv.items() if k.startswith("Postgres statements"))
            print(f"{name:68} {c['total_requests']:9} {fmt_bytes(c['bytes_down']):>10} "
                  f"{srv['DO storage puts (events + seq)']:8} {srv['DO storage lists (long-poll ticks)']:9} {pg:9} "
                  f"{srv['edge rate-limit DO ops (2 per write)']:7}")
        c = cost(s)
        print("  today, by request kind: " + ", ".join(f"{k} {v}" for k, v in c["requests"].items() if v))
        print(f"  event log at the end: {c['event_log_events']} events, {fmt_bytes(c['event_log_bytes'])}")


if __name__ == "__main__":
    main()
