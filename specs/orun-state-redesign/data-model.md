# Data Model

Every persisted JSON schema for orun-state-redesign Phase 1. Fields use
`lowerCamelCase`. Times are RFC 3339 (`2026-05-29T00:00:00Z`). All schemas carry
`apiVersion: "orun.io/v1alpha1"` and an explicit `kind` so consumers can
discriminate without filename heuristics.

---

## 1. `.orun/version.json`

Written on first creation of the new layout. Allows readers to detect the active
storage version without scanning.

```json
{
  "apiVersion": "orun.io/v1alpha1",
  "kind": "StateStoreVersion",
  "layout": "revision-first",
  "version": 1,
  "createdAt": "2026-05-29T00:00:00Z"
}
```

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `layout` | string | yes | Always `"revision-first"` in Phase 1. |
| `version` | int | yes | Bump on incompatible layout changes. |

---

## 2. `TriggerOccurrence` — `trigger.json`

One per `PlanRevision`. Captures *why* the plan was compiled.

### 2.1 Schema

```go
type TriggerOccurrence struct {
    APIVersion      string         `json:"apiVersion"`
    Kind            string         `json:"kind"`            // "TriggerOccurrence"
    TriggerID       string         `json:"triggerId"`       // ULID, "trg_..."
    TriggerKey      string         `json:"triggerKey"`      // "trg-<scope>-<shortSha>"
    TriggerType     string         `json:"triggerType"`     // "declared" | "system"
    TriggerName     string         `json:"triggerName"`     // binding name or "system.manual"
    Mode            string         `json:"mode"`            // "manual" | "event-file" | "changed" | ...
    Provider        string         `json:"provider"`        // "orun" | "github" | "gitlab" | ...
    Event           string         `json:"event"`           // "manual" | "pull_request" | "push" | ...
    Action          string         `json:"action,omitempty"`
    MatchedBindings []string       `json:"matchedBindings,omitempty"`
    Source          TriggerSource  `json:"source"`
    PlanScope       PlanScope      `json:"planScope"`
    CreatedAt       time.Time      `json:"createdAt"`
}

type TriggerSource struct {
    Repo         string `json:"repo"`
    Ref          string `json:"ref"`
    SourceScope  string `json:"sourceScope"`   // "pr-139" | "branch-main" | "tag-v1-2-0" | "manual" | "local-dirty"
    HeadRevision string `json:"headRevision"`
    BaseRevision string `json:"baseRevision,omitempty"`
    WorkingTree  string `json:"workingTree"`   // "clean" | "dirty"
}

type PlanScope struct {
    Mode               string   `json:"mode"`              // "full" | "changed"
    Base               string   `json:"base,omitempty"`
    Head               string   `json:"head,omitempty"`
    ActivationMode     string   `json:"activationMode,omitempty"` // "all-environments" | "binding-scoped"
    ActiveEnvironments []string `json:"activeEnvironments,omitempty"`
    ChangedComponents  []string `json:"changedComponents,omitempty"`
}
```

### 2.2 Validation rules

- `triggerType` MUST be one of `"declared"` or `"system"`.
- For `triggerType=="system"`, `triggerName` MUST be one of
  `system.manual`, `system.manual-changed`, `system.replay`, `system.api`, or
  `system.migrated`. (`system.ci-unmatched` is defined but not emitted in
  Phase 1.)
- For `triggerType=="declared"`, `triggerName` MUST equal a binding name from
  `intent.yaml` `automation.triggerBindings`.
- `triggerKey` MUST match `^trg-[a-z0-9-]+-([a-f0-9]{7}|local-dirty|no-git)$`.
- `source.workingTree` MUST be `"clean"` or `"dirty"`. When `"dirty"`,
  `source.headRevision` MAY be empty.
- `planScope.mode=="changed"` REQUIRES `planScope.base` and `planScope.head`.

### 2.3 Examples

Declared CI trigger:

```json
{
  "apiVersion": "orun.io/v1alpha1",
  "kind": "TriggerOccurrence",
  "triggerId": "trg_01JABC...",
  "triggerKey": "trg-pr139-def456a",
  "triggerType": "declared",
  "triggerName": "github-pull-request",
  "mode": "event-file",
  "provider": "github",
  "event": "pull_request",
  "action": "synchronize",
  "matchedBindings": ["github-pull-request"],
  "source": {
    "repo": "sourceplane/orun",
    "ref": "refs/pull/139/head",
    "sourceScope": "pr-139",
    "headRevision": "def456a1b2c3...",
    "baseRevision": "abc1239f8e7d...",
    "workingTree": "clean"
  },
  "planScope": {
    "mode": "changed",
    "base": "abc1239f8e7d...",
    "head": "def456a1b2c3...",
    "activeEnvironments": ["development"],
    "changedComponents": ["api-edge-worker"]
  },
  "createdAt": "2026-05-29T00:00:00Z"
}
```

System manual (ad-hoc `orun plan`):

```json
{
  "apiVersion": "orun.io/v1alpha1",
  "kind": "TriggerOccurrence",
  "triggerId": "trg_01JLOCAL...",
  "triggerKey": "trg-manual-def456a",
  "triggerType": "system",
  "triggerName": "system.manual",
  "mode": "manual",
  "provider": "orun",
  "event": "manual",
  "matchedBindings": ["system.manual"],
  "source": {
    "repo": "sourceplane/orun",
    "ref": "refs/heads/main",
    "sourceScope": "branch-main",
    "headRevision": "def456a1b2c3...",
    "workingTree": "clean"
  },
  "planScope": {
    "mode": "full",
    "activationMode": "all-environments"
  },
  "createdAt": "2026-05-29T00:00:00Z"
}
```

---

## 3. `PlanRevision` — `revision.json`

```go
type PlanRevision struct {
    APIVersion    string        `json:"apiVersion"`
    Kind          string        `json:"kind"`          // "PlanRevision"
    RevisionID    string        `json:"revisionId"`    // ULID, "rev_..."
    RevisionKey   string        `json:"revisionKey"`   // "rev-<scope>-<shortSha>-p<planHash8>[-xN]"
    TriggerID     string        `json:"triggerId"`
    TriggerKey    string        `json:"triggerKey"`
    PlanHash      string        `json:"planHash"`      // "sha256:..."
    PlanShortHash string        `json:"planShortHash"` // first 8 hex chars
    Source        TriggerSource `json:"source"`
    Summary       RevSummary    `json:"summary"`
    CreatedAt     time.Time     `json:"createdAt"`
}

type RevSummary struct {
    JobCount           int      `json:"jobCount"`
    Scope              string   `json:"scope"`              // "full" | "changed"
    ActiveEnvironments []string `json:"activeEnvironments"`
    ChangedComponents  []string `json:"changedComponents,omitempty"`
}
```

### 3.1 Validation

- `revisionKey` MUST match `^rev-[a-z0-9-]+-p[a-f0-9]{8}(-x\d+)?$`.
- `planHash` MUST be the full SHA-256 of the canonical `plan.json` bytes.
- `planShortHash` MUST equal the first 8 hex chars of `planHash`.

---

## 4. `RevisionManifest` — `manifest.json`

The human and tool entrypoint for a revision directory. Aggregates the most
useful fields from `revision.json` + `trigger.json` + latest execution, plus a
catalogue of objects under the revision.

```json
{
  "apiVersion": "orun.io/v1alpha1",
  "kind": "RevisionManifest",
  "revision": {
    "id": "rev_01JABC...",
    "key": "rev-pr139-def456a-p8f31c09",
    "planHash": "sha256:8f31c09d4e2...",
    "createdAt": "2026-05-29T00:00:00Z"
  },
  "trigger": {
    "id": "trg_01JABC...",
    "key": "trg-pr139-def456a",
    "type": "declared",
    "name": "github-pull-request",
    "provider": "github",
    "event": "pull_request",
    "action": "synchronize",
    "scope": "changed"
  },
  "source": {
    "repo": "sourceplane/orun",
    "sourceScope": "pr-139",
    "headRevision": "def456a1b2c3...",
    "baseRevision": "abc1239f8e7d..."
  },
  "summary": {
    "jobCount": 12,
    "activeEnvironments": ["development"],
    "latestExecutionKey": "run-001",
    "latestExecutionStatus": "completed"
  },
  "objects": {
    "plan": "plan.json",
    "trigger": "trigger.json",
    "revision": "revision.json"
  }
}
```

`summary.latestExecutionKey` and `summary.latestExecutionStatus` are updated by
`executionstate.writer` whenever an execution under this revision changes
terminal state.

---

## 5. `ExecutionRun` — `executions/<execKey>/execution.json`

```go
type ExecutionRun struct {
    APIVersion   string        `json:"apiVersion"`
    Kind         string        `json:"kind"`         // "ExecutionRun"
    ExecutionID  string        `json:"executionId"`  // ULID, "exec_..."
    ExecutionKey string        `json:"executionKey"` // "run-001" | sanitized --exec-id
    OriginalKey  string        `json:"originalKey,omitempty"`
    RevisionID   string        `json:"revisionId"`
    RevisionKey  string        `json:"revisionKey"`
    TriggerID    string        `json:"triggerId"`
    TriggerKey   string        `json:"triggerKey"`
    Reason       string        `json:"reason"`       // "direct-run" | "rerun" | "retry" | "migration"
    Status       string        `json:"status"`       // "pending" | "running" | "completed" | "failed" | "cancelled"
    Attempt      int           `json:"attempt"`
    Runner       RunnerProfile `json:"runner"`
    Summary      ExecSummary   `json:"summary"`
    CreatedAt    time.Time     `json:"createdAt"`
    StartedAt    *time.Time    `json:"startedAt,omitempty"`
    FinishedAt   *time.Time    `json:"finishedAt,omitempty"`
}

type RunnerProfile struct {
    Mode     string `json:"mode"`     // "local" | "github-actions" | ...
    Backend  string `json:"backend"`  // "local" | "remote"
    Platform string `json:"platform"` // "darwin" | "linux" | ...
}

type ExecSummary struct {
    Total     int `json:"total"`
    Completed int `json:"completed"`
    Failed    int `json:"failed"`
    Running   int `json:"running"`
    Pending   int `json:"pending"`
}
```

### 5.1 `snapshot.latest.json`

A point-in-time snapshot used by `orun status` for the watch view. Same shape
as `execution.json` but `status` and `summary` reflect the most recent runner
tick. Overwritten atomically on each tick.

---

## 6. Refs

### 6.1 `refs/latest-revision.json`

```json
{
  "revisionKey": "rev-pr139-def456a-p8f31c09",
  "revisionId": "rev_01JABC...",
  "planHash": "sha256:8f31c09...",
  "createdAt": "2026-05-29T00:00:00Z"
}
```

### 6.2 `refs/latest-execution.json`

```json
{
  "revisionKey": "rev-pr139-def456a-p8f31c09",
  "executionKey": "run-001",
  "executionId": "exec_01JXYZ...",
  "status": "completed",
  "createdAt": "2026-05-29T00:00:00Z"
}
```

### 6.3 `refs/triggers/<triggerName>/{latest.json,<sourceScope>.json}`

```json
{
  "triggerName": "github-pull-request",
  "triggerKey": "trg-pr139-def456a",
  "revisionKey": "rev-pr139-def456a-p8f31c09",
  "latestExecutionKey": "run-001",
  "headRevision": "def456a1b2c3...",
  "createdAt": "2026-05-29T00:00:00Z"
}
```

### 6.4 `refs/named/<name>.json`

User-pinned aliases (e.g. `release-candidate`). Same shape as
`refs/latest-revision.json` plus a `name` field. Phase 1 does not ship CLI
sugar to manage these; the format is reserved.

---

## 7. Indexes

### 7.1 `indexes/revisions/<revisionKey>.json`

```json
{
  "revisionKey": "rev-pr139-def456a-p8f31c09",
  "revisionId": "rev_01JABC...",
  "triggerKey": "trg-pr139-def456a",
  "planHash": "sha256:8f31c09...",
  "createdAt": "2026-05-29T00:00:00Z",
  "path": "revisions/rev-pr139-def456a-p8f31c09"
}
```

### 7.2 `indexes/executions/<executionKey>.json`

```json
{
  "executionKey": "run-001",
  "executionId": "exec_01JXYZ...",
  "revisionKey": "rev-pr139-def456a-p8f31c09",
  "status": "completed",
  "createdAt": "2026-05-29T00:00:00Z",
  "path": "revisions/rev-pr139-def456a-p8f31c09/executions/run-001"
}
```

---

## 8. Job records (reserved layout)

Phase 1 reserves the directory shape. The bridge mirrors the runner's existing
`state.json` and `metadata.json` into `executions/<execKey>/`. The schemas below
are not produced by Phase 1 code; they are documented so future runner work has
a target.

```
jobs/j-<shortHash>/job-run.json
jobs/j-<shortHash>/attempts/<n>/attempt.json
jobs/j-<shortHash>/attempts/<n>/steps/s-<slug>.json
jobs/j-<shortHash>/attempts/<n>/logs/<step>.log
```

`job-run.json` records the original `jobId`, `component`, `environment`, and
status. See the source design `orun-state-redesign.md` §6 for sketches.

---

## 9. Event stream

`executions/<execKey>/events/000000001-<kind>.json` — append-only,
zero-padded, lexically sortable. Each event captures `kind`, `at`, and a
type-specific payload. Phase 1 emits at minimum:

- `execution-created`
- `bridge-mirror-failed` (warning)
- Future kinds (`job-started`, `step-completed`, …) are runner-owned and not
  required in Phase 1.

### 9.1 `bridge-mirror-failed` payload (Phase 1, pinned)

Emitted by `internal/executionstate.Bridge` whenever a mirror tick fails
to promote a runner artifact (`state.json` or `metadata.json`) from
`.orun/executions/<legacyExecID>/` into
`revisions/<revKey>/executions/<execKey>/`. The bridge does NOT abort the
run on failure — it logs the event and continues, leaving
`.orun/executions/` authoritative for that artifact.

```json
{
  "kind": "bridge-mirror-failed",
  "at": "2026-05-30T13:32:51.78312Z",
  "payload": {
    "executionKey": "exec-…",
    "revisionKey":  "rev-…",
    "legacyExecId": "exec_<legacy>",
    "artifact":     "state.json",
    "stage":        "link",
    "mode":         "hardlink",
    "error":        "link …: invalid cross-device link"
  }
}
```

| Field          | Type     | Description                                                             |
|----------------|----------|-------------------------------------------------------------------------|
| `executionKey` | string   | The new-layout execution key (matches the parent `executions/<execKey>/`). |
| `revisionKey`  | string   | The new-layout revision key.                                             |
| `legacyExecId` | string   | The legacy execution ID under `.orun/executions/`. Differs from `executionKey` when M5.b synthesises a fresh run. |
| `artifact`     | string   | Source filename: `state.json` \| `metadata.json`.                       |
| `stage`        | string   | Failure stage: `read-source` \| `read-dest` \| `translate-dest` \| `mkdir-dest` \| `remove-dest` \| `link` \| `copy`. |
| `mode`         | string   | Mirror mode in effect: `hardlink` \| `copy` \| `auto`.                  |
| `error`        | string   | Human-readable error message from the underlying syscall.               |

Adding fields is non-breaking; renaming or removing fields is. Schema
match is pinned by
`internal/executionstate.bridgeMirrorFailedPayload` and exercised by
`TestMirrorRunnerOutput_*` in `bridge_test.go`.

---

## 10. ID generation

All `triggerId`, `revisionId`, `executionId` values are monotonic ULIDs from
`github.com/oklog/ulid/v2` with a shared `ulid.MonotonicEntropy` source per
process. Type prefixes are applied as string concatenation (`trg_` / `rev_` /
`exec_`) after generation; the underlying ULID remains the unique identifier.

Folder keys (`trg-…`, `rev-…`, `run-…`) are **independent** of the IDs and are
generated from human-meaningful inputs (scope + sha + plan hash).
