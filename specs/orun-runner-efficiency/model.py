"""Fleet model for specs/orun-runner-efficiency: the cumulative effect of RE2..RE7
on a Cirrus-shaped plan over a 200-vCPU pool.

Plan: 2 envs (stage, prod), 44 jobs each. Per env: 8 level-0 jobs (migrations /
infra, 30 s) and 36 level-1 workers each depending on one level-0 job. Worker
work: 24 x 20 s, 8 x 60 s, 4 x 180 s; the four heavy ones all hang off
migration 0 (the 'console and apps share a migration' shape). prod.X depends on
stage.X (promotion). 88 jobs; Cirrus' full merge plan is 89.

Time-stepped (1 s). A matrix entry = a lane = one runner.
Runner lifecycle: queued -> [gate] -> slot -> setup -> [lane prelude] -> jobs -> done.
In-lane: a job starts when its deps are done, up to `conc` in flight; each
running job demands `cpu` vCPU; when the lane's demand exceeds the runner's
vCPU every running job slows proportionally. A cross-lane dep costs POLL
seconds of poll latency unless waits are event-driven.

Setup: SETUP_NET seconds of network (checkout, downloads) plus SETUP_CPU
seconds of CPU-bound work calibrated to a 4-vCPU runner (toolchain install,
pnpm install), scaled by 4/vCPU. Together 70 s on 4 vCPU (the lane spec's
measurement).
"""
from dataclasses import dataclass

QUOTA = 200
SETUP_NET = 30
SETUP_CPU = 40        # on 4 vCPU; scales with 4/vcpu
INSTALL = 35          # s: a job's own `pnpm install` + cold build cache in an isolated tree
POLL = 8              # s: average loss to the 2->15 s poll backoff per cross-lane wait
GATE_LAT = 5          # s: a pool gate noticing an upstream finished
JOB_CPU = 1.5         # vCPU a running job demands (install/build/typecheck + deploy)

def setup_for(vcpu):
    return SETUP_NET + SETUP_CPU * min(2.0, 4.0 / vcpu)

@dataclass
class Job:
    id: str; env: str; w: float; deps: list
    lane: int = -1; rem: float = 0.0; done_at: float = None; started: bool = False

def make_plan():
    jobs = []
    for env in ("stage", "prod"):
        for m in range(8):
            jobs.append(Job(f"{env}.mig{m}", env, 30, []))
        ws = [180] * 4 + [60] * 8 + [20] * 24
        for i, w in enumerate(ws):
            mig = 0 if w == 180 else i % 8
            jobs.append(Job(f"{env}.w{i}", env, w, [f"{env}.mig{mig}"]))
    byid = {j.id: j for j in jobs}
    for j in jobs:
        if j.env == "prod":
            j.deps.append("stage." + j.id.split(".", 1)[1])
    return jobs, byid

def assign_lanes(jobs, lanes_per_env, weighted):
    """auto mode: a cluster is a migration and its workers; lanes_per_env of
    them per env (8 -> one cluster per lane). weighted: the heavy jobs are
    spread one per lane (the cluster cut by level; a cross-lane edge back to
    the migration)."""
    if lanes_per_env == 0:   # mode job: one runner per job, matrix in plan order
        order = sorted(jobs, key=lambda j: (j.env != "stage", ".mig" not in j.id))
        for i, j in enumerate(order):
            j.lane = i
        return len(jobs)
    lane = 0
    for env in ("stage", "prod"):
        ej = [j for j in jobs if j.env == env]
        migs = [j for j in ej if ".mig" in j.id]
        per_lane = 8 // lanes_per_env
        for k, m in enumerate(migs):
            m.lane = lane + k // per_lane
        for j in ej:
            if j in migs:
                continue
            mig = next(d for d in j.deps if ".mig" in d)
            j.lane = next(x for x in migs if x.id == mig).lane
        if weighted:
            for i, j in enumerate([j for j in ej if j.w == 180]):
                j.lane = lane + (i % lanes_per_env)
        lane += lanes_per_env
    return lane

def simulate(*, name, lanes_per_env, vcpu, conc, gate, shared_setup, weighted, events, prewarm=False, estimates=None):
    jobs, byid = make_plan()
    nlanes = assign_lanes(jobs, lanes_per_env, weighted)
    for j in jobs:
        j.rem = j.w + (0 if shared_setup else INSTALL)
    lane_jobs = {l: [j for j in jobs if j.lane == l] for l in range(nlanes)}
    setup = setup_for(vcpu) + (INSTALL if shared_setup and lanes_per_env else 0)
    slots = QUOTA // vcpu
    state = {l: "queued" for l in range(nlanes)}
    t_start, t_end, t_ready = {}, {}, {}
    running = set()
    t, vcpu_s, peak = 0, 0.0, 0

    def eligible(l):
        if gate == "none":
            return True
        if prewarm and estimates:
            def done(u): return estimates[u.id] - setup <= t
        else:
            def done(u): return u.done_at is not None and u.done_at + GATE_LAT <= t
        if gate == "all":   # every cross-lane upstream of every lane job
            return all(done(byid[d]) for j in lane_jobs[l] for d in j.deps if byid[d].lane != l)
        if gate == "first": # some lane job has no in-lane upstream and its cross-lane upstreams are done
            return any(all(byid[d].lane != l and done(byid[d]) for d in j.deps) or not j.deps
                       for j in lane_jobs[l] if all(byid[d].lane != l for d in j.deps))
        raise ValueError(gate)

    while len(t_end) < nlanes and t < 36000:
        for l in range(nlanes):
            if state[l] != "queued" or not eligible(l):
                continue
            if len(running) >= slots:
                break
            state[l] = "setup"; running.add(l); t_start[l] = t; t_ready[l] = t + setup
        for l in list(running):
            if t < t_ready[l]:
                continue
            ready = []
            for j in lane_jobs[l]:
                if j.done_at is not None:
                    continue
                ok = True
                for d in j.deps:
                    dj = byid[d]
                    if dj.done_at is None or (dj.lane != l and not events and dj.done_at + POLL > t):
                        ok = False; break
                if ok:
                    ready.append(j)
            inflight = [j for j in ready if j.started]
            for j in ready:
                if len(inflight) >= conc:
                    break
                if not j.started:
                    j.started = True; inflight.append(j)
            demand = sum(JOB_CPU for _ in inflight)
            speed = min(1.0, vcpu / demand) if demand else 1.0
            for j in inflight:
                j.rem -= speed
                if j.rem <= 0:
                    j.done_at = t + 1
            if all(j.done_at is not None for j in lane_jobs[l]):
                t_end[l] = t + 1; running.discard(l); state[l] = "done"
        vcpu_s += len(running) * vcpu
        peak = max(peak, len(running) * vcpu)
        t += 1
    makespan = max(t_end.values())
    useful = sum(j.w * JOB_CPU for j in jobs)
    return dict(name=name, lanes=nlanes, vcpu=vcpu, conc=conc, makespan=makespan, vcpu_s=vcpu_s,
                peak=peak, useful=useful, est={j.id: j.done_at for j in jobs},
                runs_per_hour=(QUOTA / peak) * (3600 / makespan))

def run(s):
    if s.get("prewarm"):
        first = simulate(**{**s, "prewarm": False})
        return simulate(**s, estimates=first["est"])
    return simulate(**s)

base4 = dict(lanes_per_env=8, vcpu=4, conc=4)
scen = [
    dict(name="A  today, mode job: 1 runner per job",           lanes_per_env=0, vcpu=4, conc=1, gate="none",  shared_setup=True,  weighted=False, events=False),
    dict(name="B  today, lanes: 16 lanes, conc 4",              **base4, gate="none",  shared_setup=False, weighted=False, events=False),
    dict(name="C1 B + RE2 gate: all upstreams done",            **base4, gate="all",   shared_setup=False, weighted=False, events=False),
    dict(name="C2 B + RE2 gate: first job runnable",            **base4, gate="first", shared_setup=False, weighted=False, events=False),
    dict(name="C3 C2 + pre-warm on estimatedSeconds",           **base4, gate="first", shared_setup=False, weighted=False, events=False, prewarm=True),
    dict(name="D  C3 + RE4 shared setup: 1 install per lane",   **base4, gate="first", shared_setup=True,  weighted=False, events=False, prewarm=True),
    dict(name="E  D + RE5 weighted, critical-path packing",     **base4, gate="first", shared_setup=True,  weighted=True,  events=False, prewarm=True),
    dict(name="F  E + RE7 event-driven waits",                  **base4, gate="first", shared_setup=True,  weighted=True,  events=True,  prewarm=True),
    dict(name="G  F on 8-vCPU runners, 4 lanes/env, conc 5",   lanes_per_env=4, vcpu=8,  conc=5,  gate="first", shared_setup=True, weighted=True, events=True, prewarm=True),
    dict(name="H  F on 16-vCPU runners, 2 lanes/env, conc 10", lanes_per_env=2, vcpu=16, conc=10, gate="first", shared_setup=True, weighted=True, events=True, prewarm=True),
    dict(name="I  F on 2-vCPU runners, 8 lanes/env, conc 2",   lanes_per_env=8, vcpu=2,  conc=2,  gate="first", shared_setup=True, weighted=True, events=True, prewarm=True),
    dict(name="J  F on 4-vCPU runners, 4 lanes/env, conc 3",   lanes_per_env=4, vcpu=4,  conc=3,  gate="first", shared_setup=True, weighted=True, events=True, prewarm=True),
]

if __name__ == "__main__":
    results = [run(s) for s in scen]
    base = results[1]
    hdr = f"{'scenario':50} {'lanes':>5} {'vCPU':>4} {'conc':>4} {'wall':>7} {'vCPU·h':>7} {'peak':>5} {'useful':>7} {'runs/h':>7}"
    print(hdr)
    for r in results:
        print(f"{r['name']:50} {r['lanes']:5d} {r['vcpu']:4d} {r['conc']:4d} {r['makespan']/60:6.1f}m {r['vcpu_s']/3600:7.2f} {r['peak']:5d} {100*r['useful']/r['vcpu_s']:6.1f}% {r['runs_per_hour']:7.1f}")
    print("\nrelative to B (lanes as shipped):")
    for r in results:
        print(f"  {r['name']:50} wall x{r['makespan']/base['makespan']:.2f}   vCPU·h x{r['vcpu_s']/base['vcpu_s']:.2f}   runs/h x{r['runs_per_hour']/base['runs_per_hour']:.2f}")
