-- Core control-plane routing table for catalog/run shard assignments.
-- The router checks this table for dedicated tenant backends before falling
-- back to deterministic hash routing over the bounded shard set.
CREATE TABLE tenant_routes (
  route_key    TEXT PRIMARY KEY, -- tenant ID, account ID, or namespace ID
  route_scope  TEXT NOT NULL,    -- 'tenant' | 'account' | 'namespace'
  backend_type TEXT NOT NULL,    -- 'd1_shard' | 'd1_dedicated' | 'postgres'
  backend_ref  TEXT NOT NULL,    -- shard name, D1 binding/database ID, or Hyperdrive binding
  shard_index  INTEGER,          -- for d1_shard: the 0-based index into the shard array
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL
);
