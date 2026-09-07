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
  -- groups is a JSON array of group ids; nodes registered with the key
  -- join those groups. See docs/ref/access-control.md.
  groups text,
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
  -- suspended_at is set while an administrator has suspended the node:
  -- it stays registered but gets no peers, no peer sees it and its
  -- client is told it is not authorized. NULL for an active node.
  suspended_at datetime,
  -- posture is the device identity the client reported over c2n as
  -- JSON (types.PostureIdentity), NULL until the server asked.
  posture text,
  -- global_exit_node marks an exit node every client is told to prefer:
  -- it gets suggest-exit-node and every node auto-exit-node.
  global_exit_node numeric DEFAULT false,

  created_at datetime,
  updated_at datetime,
  deleted_at datetime,

  CONSTRAINT fk_nodes_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT fk_nodes_auth_key FOREIGN KEY(auth_key_id) REFERENCES pre_auth_keys(id)
);

-- node_attributes are the custom posture attributes set through the API,
-- Tailscale style: a "custom:" key with a string, number or bool value
-- (JSON in value) that may expire; see docs/ref/device-trust.md.
CREATE TABLE node_attributes(
  id integer PRIMARY KEY AUTOINCREMENT,
  node_id integer NOT NULL,
  key text NOT NULL,
  value text NOT NULL,
  expires_at datetime,
  comment text,
  created_at datetime,
  updated_at datetime,

  CONSTRAINT fk_node_attributes_node FOREIGN KEY(node_id) REFERENCES nodes(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_node_attributes_node_key ON node_attributes(node_id, key);

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

-- groups are named sets of nodes for access rules; see
-- docs/ref/access-control.md. A node belongs directly (group_nodes) or
-- through its owner (group_users). The builtin "all" group holds every
-- node and has no rows in either.
CREATE TABLE groups(
  id integer PRIMARY KEY AUTOINCREMENT,
  name text NOT NULL,
  description text,
  builtin text,
  -- requestable lets members ask to join the group for a while; see
  -- access_requests.
  requestable numeric DEFAULT false,
  created_at datetime,
  updated_at datetime
);
CREATE UNIQUE INDEX idx_groups_name ON groups(name);

CREATE TABLE group_nodes(
  id integer PRIMARY KEY AUTOINCREMENT,
  group_id integer NOT NULL,
  node_id integer NOT NULL,
  created_at datetime,
  -- expires_at ends a temporary membership; NULL is permanent.
  expires_at datetime,

  CONSTRAINT fk_group_nodes_group FOREIGN KEY(group_id) REFERENCES groups(id) ON DELETE CASCADE,
  CONSTRAINT fk_group_nodes_node FOREIGN KEY(node_id) REFERENCES nodes(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_group_nodes_group_node ON group_nodes(group_id, node_id);

CREATE TABLE group_users(
  id integer PRIMARY KEY AUTOINCREMENT,
  group_id integer NOT NULL,
  user_id integer NOT NULL,
  created_at datetime,
  expires_at datetime,

  CONSTRAINT fk_group_users_group FOREIGN KEY(group_id) REFERENCES groups(id) ON DELETE CASCADE,
  CONSTRAINT fk_group_users_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_group_users_group_user ON group_users(group_id, user_id);

-- access_rules let source groups reach destination groups on a protocol
-- and ports. Rules only allow. The policy compiles them next to the
-- policy file's grants.
CREATE TABLE access_rules(
  id integer PRIMARY KEY AUTOINCREMENT,
  name text NOT NULL,
  description text,
  enabled numeric DEFAULT true,
  protocol text NOT NULL,
  ports text,
  bidirectional numeric DEFAULT false,
  -- expires_at is when the rule stops applying; NULL never does. An
  -- expired rule is kept so it can be extended or deleted.
  expires_at datetime,
  created_at datetime,
  updated_at datetime
);

CREATE TABLE access_rule_groups(
  id integer PRIMARY KEY AUTOINCREMENT,
  rule_id integer NOT NULL,
  group_id integer NOT NULL,
  side text NOT NULL,

  CONSTRAINT fk_access_rule_groups_rule FOREIGN KEY(rule_id) REFERENCES access_rules(id) ON DELETE CASCADE,
  CONSTRAINT fk_access_rule_groups_group FOREIGN KEY(group_id) REFERENCES groups(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_access_rule_groups_rule_group_side ON access_rule_groups(rule_id, group_id, side);

-- postures are reusable conditions on a source node: expressions in the
-- policy file's posture language (JSON list in expressions) and an
-- optional weekly schedule (JSON); see docs/ref/device-trust.md. An
-- access rule that names postures lets a source through when any one of
-- them holds.
CREATE TABLE postures(
  id integer PRIMARY KEY AUTOINCREMENT,
  name text NOT NULL,
  description text,
  expressions text NOT NULL,
  schedule text,
  created_at datetime,
  updated_at datetime
);
CREATE UNIQUE INDEX idx_postures_name ON postures(name);

CREATE TABLE access_rule_postures(
  id integer PRIMARY KEY AUTOINCREMENT,
  rule_id integer NOT NULL,
  posture_id integer NOT NULL,

  CONSTRAINT fk_access_rule_postures_rule FOREIGN KEY(rule_id) REFERENCES access_rules(id) ON DELETE CASCADE,
  CONSTRAINT fk_access_rule_postures_posture FOREIGN KEY(posture_id) REFERENCES postures(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_access_rule_postures_rule_posture ON access_rule_postures(rule_id, posture_id);

-- access_requests are asks from a user to join a requestable group for a
-- while, for one machine (node_id) or every machine they own (node_id
-- NULL); see docs/ref/temporary-access.md. Approval adds the membership
-- with expires_at; the request keeps the outcome for the record.
CREATE TABLE access_requests(
  id integer PRIMARY KEY AUTOINCREMENT,
  user_id integer NOT NULL,
  node_id integer,
  group_id integer NOT NULL,
  reason text,
  duration_seconds integer NOT NULL,
  status text NOT NULL,
  decided_by text,
  note text,
  created_at datetime,
  decided_at datetime,
  expires_at datetime,

  CONSTRAINT fk_access_requests_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT fk_access_requests_node FOREIGN KEY(node_id) REFERENCES nodes(id) ON DELETE CASCADE,
  CONSTRAINT fk_access_requests_group FOREIGN KEY(group_id) REFERENCES groups(id) ON DELETE CASCADE
);
CREATE INDEX idx_access_requests_status ON access_requests(status, id);

-- networks are sets of prefixes reached through routing nodes, the way
-- NetBird's networks work; see docs/ref/networks.md. The routers
-- advertise the prefixes and the network approves them; the groups get
-- the routes.
CREATE TABLE networks(
  id integer PRIMARY KEY AUTOINCREMENT,
  name text NOT NULL,
  description text,
  enabled numeric DEFAULT true,
  created_at datetime,
  updated_at datetime
);
CREATE UNIQUE INDEX idx_networks_name ON networks(name);

CREATE TABLE network_prefixes(
  id integer PRIMARY KEY AUTOINCREMENT,
  network_id integer NOT NULL,
  prefix text NOT NULL,

  CONSTRAINT fk_network_prefixes_network FOREIGN KEY(network_id) REFERENCES networks(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_network_prefixes_network_prefix ON network_prefixes(network_id, prefix);

CREATE TABLE network_routers(
  id integer PRIMARY KEY AUTOINCREMENT,
  network_id integer NOT NULL,
  node_id integer NOT NULL,

  CONSTRAINT fk_network_routers_network FOREIGN KEY(network_id) REFERENCES networks(id) ON DELETE CASCADE,
  CONSTRAINT fk_network_routers_node FOREIGN KEY(node_id) REFERENCES nodes(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_network_routers_network_node ON network_routers(network_id, node_id);

CREATE TABLE network_groups(
  id integer PRIMARY KEY AUTOINCREMENT,
  network_id integer NOT NULL,
  group_id integer NOT NULL,

  CONSTRAINT fk_network_groups_network FOREIGN KEY(network_id) REFERENCES networks(id) ON DELETE CASCADE,
  CONSTRAINT fk_network_groups_group FOREIGN KEY(group_id) REFERENCES groups(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_network_groups_network_group ON network_groups(network_id, group_id);

-- network_route_approvals records the route approvals networks made, so
-- that withdrawing a network leaves approvals made by hand alone.
CREATE TABLE network_route_approvals(
  id integer PRIMARY KEY AUTOINCREMENT,
  node_id integer NOT NULL,
  prefix text NOT NULL,

  CONSTRAINT fk_network_route_approvals_node FOREIGN KEY(node_id) REFERENCES nodes(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_network_route_approvals_node_prefix ON network_route_approvals(node_id, prefix);

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

-- webhooks are the endpoints the server posts events to; see
-- docs/ref/webhooks.md. subscriptions is a JSON array of event type
-- names and secret signs every delivery.
CREATE TABLE webhooks(
  id integer PRIMARY KEY AUTOINCREMENT,
  url text NOT NULL,
  description text,
  provider_type text,
  secret text NOT NULL,
  subscriptions text NOT NULL,
  created_by integer,
  created_at datetime,
  updated_at datetime,
  last_delivery_at datetime,
  last_delivery_status text
);

-- webhook_deliveries keeps the newest attempts per webhook (the dispatcher
-- trims the rest) so an operator can see what was sent and how it went.
-- status is the HTTP status, or the error text when no response came.
CREATE TABLE webhook_deliveries(
  id integer PRIMARY KEY AUTOINCREMENT,
  webhook_id integer NOT NULL,
  event_type text NOT NULL,
  status text NOT NULL,
  ok boolean NOT NULL,
  attempts integer NOT NULL,
  duration_ms integer NOT NULL,
  created_at datetime NOT NULL,

  CONSTRAINT fk_webhook_deliveries_webhook FOREIGN KEY(webhook_id) REFERENCES webhooks(id) ON DELETE CASCADE
);
CREATE INDEX idx_webhook_deliveries_webhook ON webhook_deliveries(webhook_id, id);
