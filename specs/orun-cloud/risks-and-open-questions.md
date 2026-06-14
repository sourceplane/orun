# orun-cloud — Risks & Open Questions

Status: Draft. Platform-side decisions (naming, free tier, secret custody,
BYO-KMS, conformance commitment) live in
`orun-cloud/specs/epics/saas-orun-platform/risks-and-open-questions.md`
(D1–D6); this doc carries only the client-side items.

## Decisions needed (human)

### C1 — Default backend URL in released binaries
Once the platform has a public URL, does `orun auth login` default to it
(zero-config onboarding, strongest funnel) or stay URL-required (neutral OSS
posture)? Recommendation: default to the public URL with `--backend-url`
prominent in `--help` and docs — the OSS self-host story stays first-class via
config. Needed before the first release that ships OC1.

### C2 — GitHub device-flow removal vs migration window
OC1 replaces driving GitHub's device flow with the platform's. Existing
sessions from the reference backend become invalid. Decide: hard cut on a
minor release (recommended — the feature is pre-GA) or a one-release dual
path. Affects OC1 scope only.

### C3 — `catalog push` in the default `run --remote-state` path
Design keeps catalog publishing explicit/opt-in. Counter-argument: drift
badges (platform OP7) are only as fresh as pushes. Recommendation: keep
explicit for humans, recommend `catalog refresh --push` in the CI golden-path
workflow so CI keeps heads fresh. Revisit after OP7 ships with real usage.

## Engineering risks (chosen mitigations)

### R1 — Contract drift
Mirror of platform R4. Mitigation: vendored contract + CI diff (OC0), version
header fails loud, conformance suite (OC6) is the executable contract. The
client never branches on which server it talks to.

### R2 — Heartbeat liveness on busy runners
A step that saturates CPU could starve the heartbeat goroutine and lose the
lease, double-running a job's tail. Mitigation: heartbeats run on a dedicated
goroutine with a monotonic deadline check; on `lease_lost` the runner kills
the step process group before exiting the job (the platform may have re-queued
it). Tested in OC3's kill/recovery gate.

### R3 — Log buffering memory on backend outage
Unbounded buffering of chunks during an outage could OOM a CI box. Mitigation:
bounded in-memory buffer with spill-to-file (design §7 row 5), hard cap with
oldest-chunk drop + a "log gap" marker chunk so the server-side log is honest
about loss.

### R4 — Secret redaction completeness
Substring scrubbing can't catch transformed values (base64, split lines).
Mitigation: redactor handles exact + line-split occurrences; documented
residual risk (mirrors platform R6); secrets never touch disk locally, which
bounds the blast radius to live log streams the user's own step produced.

### R5 — `~/.orun/session.json` theft
A copied session file is a working credential until revoke. Mitigation now:
0600 enforcement, short access tokens, rotating refresh with reuse detection
(theft + parallel use revokes the family), console session list + revoke.
Follow-up (not this epic): OS-keychain storage behind a build tag.

### R6 — Plan-digest-first InitRun ordering
Run create requires the plan object to exist; a crash between object PUT and
run create leaves an orphan blob. Harmless (GC-eligible, platform R7), but the
client retries create-after-ensure as one idempotent composite so the common
case self-heals.

## Deferred

- OS-keychain session storage (R5 follow-up).
- SSE log streaming when the platform adds it (client stays cursor-based until
  then; additive).
- Artifact upload through the object plane (`artifact-manifest` kind is
  reserved in the contract; runner integration is a future spec).
- Hosted-runner handshake — out of scope until the platform epic exists.
