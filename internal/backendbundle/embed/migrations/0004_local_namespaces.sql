-- Add immutable numeric GitHub user ID to accounts.
-- NULL for existing rows until next login.
ALTER TABLE accounts ADD COLUMN github_user_id TEXT;

-- Distinguish canonical repo namespaces from user-scoped local namespaces.
-- Default 'repo' covers all existing rows.
ALTER TABLE namespaces ADD COLUMN namespace_kind TEXT NOT NULL DEFAULT 'repo';

-- Account-scoped repo cache populated during OAuth/device login.
-- Namespace resolution for CLI local sessions uses this table, not the
-- global namespaces table, so a user can only link repos that were
-- visible under their own GitHub credentials.
CREATE TABLE account_repo_cache (
  account_id     TEXT NOT NULL,
  repo_id        TEXT NOT NULL,
  repo_full_name TEXT NOT NULL,
  last_seen_at   TEXT NOT NULL,
  PRIMARY KEY (account_id, repo_id),
  FOREIGN KEY (account_id) REFERENCES accounts(account_id)
);

CREATE INDEX idx_account_repo_cache_name ON account_repo_cache(account_id, repo_full_name);
