CREATE TABLE accounts (
  account_id   TEXT PRIMARY KEY,
  github_login TEXT NOT NULL UNIQUE,
  created_at   TEXT NOT NULL
);

CREATE TABLE account_repos (
  account_id    TEXT NOT NULL,
  namespace_id  TEXT NOT NULL,
  linked_by     TEXT NOT NULL,
  linked_at     TEXT NOT NULL,
  PRIMARY KEY (account_id, namespace_id),
  FOREIGN KEY (account_id) REFERENCES accounts(account_id),
  FOREIGN KEY (namespace_id) REFERENCES namespaces(namespace_id)
);

CREATE INDEX idx_account_repos_namespace ON account_repos(namespace_id);
