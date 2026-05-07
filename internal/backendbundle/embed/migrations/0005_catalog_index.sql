CREATE TABLE catalog_uploads (
  upload_id       TEXT PRIMARY KEY,
  namespace_id    TEXT NOT NULL,
  repo_id         TEXT NOT NULL,
  repo_full_name  TEXT NOT NULL,
  commit_sha      TEXT NOT NULL,
  branch          TEXT,
  workflow_run_id TEXT,
  workflow_ref    TEXT,
  pr_number       INTEGER,
  envelope_ref    TEXT NOT NULL,
  component_count INTEGER NOT NULL,
  created_at      TEXT NOT NULL,
  FOREIGN KEY (namespace_id) REFERENCES namespaces(namespace_id)
);

CREATE TABLE catalog_components (
  component_id         TEXT PRIMARY KEY,
  namespace_id         TEXT NOT NULL,
  repo_id              TEXT NOT NULL,
  repo_full_name       TEXT NOT NULL,
  name                 TEXT NOT NULL,
  title                TEXT,
  description          TEXT,
  type                 TEXT NOT NULL,
  owner                TEXT,
  system               TEXT,
  lifecycle            TEXT,
  repo_path            TEXT NOT NULL,
  tags_json            TEXT NOT NULL,
  environments_json    TEXT NOT NULL,
  latest_plan_id       TEXT,
  latest_plan_checksum TEXT,
  latest_commit_sha    TEXT NOT NULL,
  latest_status        TEXT NOT NULL DEFAULT 'unknown',
  current_state_ref    TEXT NOT NULL,
  first_seen_at        TEXT NOT NULL,
  last_seen_at         TEXT NOT NULL,
  FOREIGN KEY (namespace_id) REFERENCES namespaces(namespace_id)
);

CREATE TABLE catalog_component_relations (
  relation_id         TEXT PRIMARY KEY,
  source_component_id TEXT NOT NULL,
  relation_type       TEXT NOT NULL,
  target_kind         TEXT NOT NULL,
  target_ref          TEXT NOT NULL,
  environment         TEXT,
  job_id              TEXT,
  last_seen_at        TEXT NOT NULL,
  FOREIGN KEY (source_component_id) REFERENCES catalog_components(component_id)
);

CREATE TABLE catalog_component_events (
  event_id        TEXT PRIMARY KEY,
  component_id    TEXT NOT NULL,
  namespace_id    TEXT NOT NULL,
  upload_id       TEXT NOT NULL,
  event_type      TEXT NOT NULL,
  commit_sha      TEXT NOT NULL,
  pr_number       INTEGER,
  summary         TEXT,
  payload_ref     TEXT,
  created_at      TEXT NOT NULL,
  FOREIGN KEY (component_id) REFERENCES catalog_components(component_id),
  FOREIGN KEY (upload_id) REFERENCES catalog_uploads(upload_id)
);

CREATE INDEX idx_catalog_components_namespace ON catalog_components(namespace_id, last_seen_at DESC);
CREATE INDEX idx_catalog_components_repo ON catalog_components(repo_id, name);
CREATE INDEX idx_catalog_components_owner ON catalog_components(owner);
CREATE INDEX idx_catalog_components_type ON catalog_components(type);
CREATE INDEX idx_catalog_events_component ON catalog_component_events(component_id, created_at DESC);
CREATE INDEX idx_catalog_relations_target ON catalog_component_relations(target_kind, target_ref);
