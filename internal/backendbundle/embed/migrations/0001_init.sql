CREATE TABLE namespaces (
  namespace_id   TEXT PRIMARY KEY,
  namespace_slug TEXT NOT NULL,
  last_seen_at   TEXT NOT NULL
);

CREATE TABLE runs (
  run_id         TEXT NOT NULL,
  namespace_id   TEXT NOT NULL,
  status         TEXT NOT NULL DEFAULT 'pending',
  plan_checksum  TEXT,
  trigger_type   TEXT,
  actor          TEXT,
  dry_run        INTEGER NOT NULL DEFAULT 0,
  created_at     TEXT NOT NULL,
  updated_at     TEXT NOT NULL,
  finished_at    TEXT,
  job_total      INTEGER NOT NULL DEFAULT 0,
  job_done       INTEGER NOT NULL DEFAULT 0,
  job_failed     INTEGER NOT NULL DEFAULT 0,
  expires_at     TEXT NOT NULL,
  PRIMARY KEY (namespace_id, run_id)
);

CREATE TABLE jobs (
  job_id         TEXT NOT NULL,
  run_id         TEXT NOT NULL,
  namespace_id   TEXT NOT NULL,
  component      TEXT NOT NULL,
  status         TEXT NOT NULL DEFAULT 'pending',
  runner_id      TEXT,
  started_at     TEXT,
  finished_at    TEXT,
  log_ref        TEXT,
  PRIMARY KEY (namespace_id, run_id, job_id)
);

CREATE INDEX idx_runs_namespace_status ON runs(namespace_id, status);
CREATE INDEX idx_runs_expires ON runs(expires_at);
CREATE INDEX idx_jobs_run ON jobs(namespace_id, run_id);
