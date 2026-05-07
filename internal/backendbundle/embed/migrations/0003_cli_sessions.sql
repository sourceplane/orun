CREATE TABLE cli_sessions (
  session_id                 TEXT PRIMARY KEY,
  account_id                 TEXT NOT NULL,
  github_login               TEXT NOT NULL,
  refresh_token_hash         TEXT NOT NULL UNIQUE,
  allowed_namespace_ids_json TEXT NOT NULL,
  created_at                 TEXT NOT NULL,
  last_used_at               TEXT,
  expires_at                 TEXT NOT NULL,
  revoked_at                 TEXT,
  user_agent                 TEXT,
  device_label               TEXT,
  FOREIGN KEY (account_id) REFERENCES accounts(account_id)
);

CREATE INDEX idx_cli_sessions_account ON cli_sessions(account_id);
CREATE INDEX idx_cli_sessions_expires ON cli_sessions(expires_at);
CREATE INDEX idx_cli_sessions_hash ON cli_sessions(refresh_token_hash);
