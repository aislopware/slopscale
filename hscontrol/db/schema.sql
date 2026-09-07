-- This file is the representation of the SQLite schema of Headscale.
-- It is the "source of truth" and is used to validate any migrations
-- that are run against the database to ensure it ends in the expected state.

CREATE TABLE migrations(id text,PRIMARY KEY(id));

CREATE TABLE users(
  id integer PRIMARY KEY AUTOINCREMENT,
  name text,
  display_name text,
  email text,
  provider_identifier text,
  provider text,
  profile_pic_url text,
  role text,
  -- approved_at is NULL while a user created by OIDC login waits for an
  -- administrator (users approval); administrator-created users are
  -- approved on creation.
  approved_at datetime,

  created_at datetime,
  updated_at datetime,
  deleted_at datetime
);
CREATE INDEX idx_users_deleted_at ON users(deleted_at);


-- The following three UNIQUE indexes work together to enforce the user identity model:
--
-- 1. Users can be either local (provider_identifier is NULL) or from external providers (provider_identifier set)
-- 2. Each external provider identifier must be unique across the system
-- 3. Local usernames must be unique among local users
-- 4. The same username can exist across different providers with different identifiers
--
-- Examples:
-- - Can create local user "alice" (provider_identifier=NULL)
-- - Can create external user "alice" with GitHub (name="alice", provider_identifier="alice_github")
-- - Can create external user "alice" with Google (name="alice", provider_identifier="alice_google")
-- - Cannot create another local user "alice" (blocked by idx_name_no_provider_identifier)
-- - Cannot create another user with provider_identifier="alice_github" (blocked by idx_provider_identifier)
-- - Cannot create user "bob" with provider_identifier="alice_github" (blocked by idx_name_provider_identifier)
CREATE UNIQUE INDEX idx_provider_identifier ON users(provider_identifier) WHERE provider_identifier IS NOT NULL;
CREATE UNIQUE INDEX idx_name_provider_identifier ON users(name, provider_identifier);
CREATE UNIQUE INDEX idx_name_no_provider_identifier ON users(name) WHERE provider_identifier IS NULL;

CREATE TABLE pre_auth_keys(
  id integer PRIMARY KEY AUTOINCREMENT,
  key text,
  prefix text,
  hash blob,
  user_id integer,
  description text,
  reusable numeric,
  ephemeral numeric DEFAULT false,
  used numeric DEFAULT false,
  tags text,
  -- preauthorized keys register nodes as approved even while device
  -- approval is on.
  preauthorized numeric DEFAULT true,
  expiration datetime,
  revoked datetime,

  created_at datetime,

  CONSTRAINT fk_pre_auth_keys_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE SET NULL
);
CREATE UNIQUE INDEX idx_pre_auth_keys_prefix ON pre_auth_keys(prefix) WHERE prefix IS NOT NULL AND prefix != '';

CREATE TABLE api_keys(
  id integer PRIMARY KEY AUTOINCREMENT,
  prefix text,
  hash blob,
  user_id integer,
  expiration datetime,
  last_seen datetime,

  created_at datetime
);
CREATE UNIQUE INDEX idx_api_keys_prefix ON api_keys(prefix);

-- OAuth 2.0 client-credentials clients for the v2 API. client_id is public and
-- embedded in the secret (hskey-client-<client_id>-<secret>); only the bcrypt
-- hash of the secret is stored. Mirrors the api_keys security model.
CREATE TABLE oauth_clients(
  id integer PRIMARY KEY AUTOINCREMENT,
  client_id text,
  secret_hash blob,
  scopes text,
  tags text,
  description text,
  user_id integer,
  created_at datetime,
  revoked datetime
);
CREATE UNIQUE INDEX idx_oauth_clients_client_id ON oauth_clients(client_id);

-- Short-lived bearer access tokens minted by an oauth_client. Stored as a bcrypt
-- hash of the secret, looked up by prefix.
CREATE TABLE oauth_access_tokens(
  id integer PRIMARY KEY AUTOINCREMENT,
  prefix text,
  hash blob,
  client_id text,
  scopes text,
  tags text,
  expiration datetime,
  created_at datetime
);
CREATE UNIQUE INDEX idx_oauth_access_tokens_prefix ON oauth_access_tokens(prefix);

CREATE TABLE nodes(
  id integer PRIMARY KEY AUTOINCREMENT,
  machine_key text,
  node_key text,
  disco_key text,

  endpoints text,
  host_info text,
  ipv4 text,
  ipv6 text,
  hostname text,
  given_name varchar(63),
  -- user_id is NULL for tagged nodes (owned by tags, not a user).
  -- Only set for user-owned nodes (no tags).
  user_id integer,
  register_method text,
  tags text,
  auth_key_id integer,
  last_seen datetime,
  expiry datetime,
  approved_routes text,
  -- approved_at is NULL while a node registered with device approval on
  -- waits for an administrator; it gets no peers and no peer sees it.
  approved_at datetime,
  -- global_exit_node marks an exit node every client is told to prefer:
  -- it gets suggest-exit-node and every node auto-exit-node.
  global_exit_node numeric DEFAULT false,

  created_at datetime,
  updated_at datetime,
  deleted_at datetime,

  CONSTRAINT fk_nodes_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT fk_nodes_auth_key FOREIGN KEY(auth_key_id) REFERENCES pre_auth_keys(id)
);

-- node_shares records the users a node has been shared with. The policy
-- resolves autogroup:shared per node from it; see docs/ref/sharing.md.
CREATE TABLE node_shares(
  id integer PRIMARY KEY AUTOINCREMENT,
  node_id integer NOT NULL,
  user_id integer NOT NULL,
  created_by integer,
  created_at datetime,

  CONSTRAINT fk_node_shares_node FOREIGN KEY(node_id) REFERENCES nodes(id) ON DELETE CASCADE,
  CONSTRAINT fk_node_shares_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_node_shares_node_user ON node_shares(node_id, user_id);

CREATE TABLE policies(
  id integer PRIMARY KEY AUTOINCREMENT,
  data text,

  created_at datetime,
  updated_at datetime,
  deleted_at datetime
);
CREATE INDEX idx_policies_deleted_at ON policies(deleted_at);

CREATE TABLE database_versions(
  id integer PRIMARY KEY,
  version text NOT NULL,
  updated_at datetime
);

-- settings holds tailnet-wide switches keyed by name; see
-- hscontrol/types/settings.go for the keys and their defaults.
CREATE TABLE settings(
  key text PRIMARY KEY,
  value text,
  updated_at datetime
);

-- sessions are the admin console's browser sign-ins: a user who signed in
-- through the identity provider holds a random token in a cookie, and the
-- row maps its hash to the user until expires_at. Deleting the user
-- deletes its sessions.
CREATE TABLE sessions(
  id integer PRIMARY KEY AUTOINCREMENT,
  token_hash blob NOT NULL,
  user_id integer NOT NULL,
  created_at datetime,
  expires_at datetime,
  last_seen_at datetime,

  CONSTRAINT fk_sessions_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_sessions_token_hash ON sessions(token_hash);

-- audit_events is the append-only record of who changed what: every
-- writing API request and the sign-in events the server performs itself.
-- Actor and target are recorded by value, without foreign keys, so the
-- history survives the deletion of the user or object it names.
CREATE TABLE audit_events(
  id integer PRIMARY KEY AUTOINCREMENT,
  created_at datetime NOT NULL,
  actor_kind text NOT NULL,
  actor_user_id integer,
  actor_name text,
  action text NOT NULL,
  target_kind text,
  target_id text,
  target_name text,
  outcome integer NOT NULL,
  detail text,
  remote_addr text
);
CREATE INDEX idx_audit_events_created_at ON audit_events(created_at);
CREATE INDEX idx_audit_events_actor_user_id ON audit_events(actor_user_id);
