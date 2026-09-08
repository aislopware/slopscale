-- PostgreSQL schema of Headscale. It mirrors schema.sql (the SQLite source of
-- truth) with PostgreSQL types, and matches column for column what the
-- previous GORM AutoMigrate produced, so an existing deployment and a fresh
-- one end up identical. Validated by TestPostgresSchemaMatchesGolden.

CREATE TABLE migrations(id varchar(255) PRIMARY KEY);

CREATE TABLE users(
  id bigserial PRIMARY KEY,
  created_at timestamptz,
  updated_at timestamptz,
  deleted_at timestamptz,
  name text,
  display_name text,
  email text,
  provider_identifier text,
  provider text,
  profile_pic_url text,
  role text,
  approved_at timestamptz
);
CREATE INDEX idx_users_deleted_at ON users(deleted_at);
CREATE UNIQUE INDEX idx_provider_identifier ON users(provider_identifier) WHERE provider_identifier IS NOT NULL;
CREATE UNIQUE INDEX idx_name_provider_identifier ON users(name, provider_identifier);
CREATE UNIQUE INDEX idx_name_no_provider_identifier ON users(name) WHERE provider_identifier IS NULL;

CREATE TABLE pre_auth_keys(
  id bigserial PRIMARY KEY,
  key text,
  prefix text,
  hash bytea,
  user_id bigint,
  description text,
  reusable boolean,
  ephemeral boolean DEFAULT false,
  used boolean DEFAULT false,
  tags text,
  created_at timestamptz,
  expiration timestamptz,
  revoked timestamptz,
  preauthorized boolean DEFAULT true,
  groups text,
  CONSTRAINT fk_pre_auth_keys_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE SET NULL
);
CREATE UNIQUE INDEX idx_pre_auth_keys_prefix ON pre_auth_keys(prefix) WHERE prefix IS NOT NULL AND prefix != '';

CREATE TABLE api_keys(
  id bigserial PRIMARY KEY,
  prefix text,
  hash bytea,
  user_id bigint,
  created_at timestamptz,
  expiration timestamptz,
  last_seen timestamptz,
  scopes text,
  description text
);
CREATE UNIQUE INDEX idx_api_keys_prefix ON api_keys(prefix);

CREATE TABLE oauth_clients(
  id bigserial PRIMARY KEY,
  client_id text,
  secret_hash bytea,
  scopes text,
  tags text,
  description text,
  user_id bigint,
  created_at timestamptz,
  revoked timestamptz
);
CREATE UNIQUE INDEX idx_oauth_clients_client_id ON oauth_clients(client_id);

CREATE TABLE oauth_access_tokens(
  id bigserial PRIMARY KEY,
  prefix text,
  hash bytea,
  client_id text,
  scopes text,
  tags text,
  expiration timestamptz,
  created_at timestamptz
);
CREATE UNIQUE INDEX idx_oauth_access_tokens_prefix ON oauth_access_tokens(prefix);

CREATE TABLE nodes(
  id bigserial PRIMARY KEY,
  machine_key text,
  node_key text,
  disco_key text,
  endpoints text,
  host_info text,
  ipv4 text,
  ipv6 text,
  hostname text,
  given_name varchar(63),
  user_id bigint,
  register_method text,
  tags text,
  auth_key_id bigint,
  expiry timestamptz,
  last_seen timestamptz,
  approved_routes text,
  created_at timestamptz,
  updated_at timestamptz,
  deleted_at timestamptz,
  approved_at timestamptz,
  global_exit_node boolean DEFAULT false,
  suspended_at timestamptz,
  posture text,
  ephemeral boolean DEFAULT false,
  CONSTRAINT fk_nodes_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT fk_nodes_auth_key FOREIGN KEY(auth_key_id) REFERENCES pre_auth_keys(id)
);

CREATE TABLE node_attributes(
  id bigserial PRIMARY KEY,
  node_id bigint NOT NULL,
  key text NOT NULL,
  value text NOT NULL,
  expires_at timestamptz,
  comment text,
  created_at timestamptz,
  updated_at timestamptz,
  CONSTRAINT fk_node_attributes_node FOREIGN KEY(node_id) REFERENCES nodes(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_node_attributes_node_key ON node_attributes(node_id, key);

CREATE TABLE node_shares(
  id bigserial PRIMARY KEY,
  node_id bigint NOT NULL,
  user_id bigint NOT NULL,
  created_by bigint,
  created_at timestamptz,
  CONSTRAINT fk_node_shares_node FOREIGN KEY(node_id) REFERENCES nodes(id) ON DELETE CASCADE,
  CONSTRAINT fk_node_shares_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_node_shares_node_user ON node_shares(node_id, user_id);

CREATE TABLE groups(
  id bigserial PRIMARY KEY,
  name text NOT NULL,
  description text,
  builtin text,
  requestable boolean DEFAULT false,
  created_at timestamptz,
  updated_at timestamptz
);
CREATE UNIQUE INDEX idx_groups_name ON groups(name);

CREATE TABLE group_nodes(
  id bigserial PRIMARY KEY,
  group_id bigint NOT NULL,
  node_id bigint NOT NULL,
  created_at timestamptz,
  expires_at timestamptz,
  CONSTRAINT fk_group_nodes_group FOREIGN KEY(group_id) REFERENCES groups(id) ON DELETE CASCADE,
  CONSTRAINT fk_group_nodes_node FOREIGN KEY(node_id) REFERENCES nodes(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_group_nodes_group_node ON group_nodes(group_id, node_id);

CREATE TABLE group_users(
  id bigserial PRIMARY KEY,
  group_id bigint NOT NULL,
  user_id bigint NOT NULL,
  created_at timestamptz,
  expires_at timestamptz,
  CONSTRAINT fk_group_users_group FOREIGN KEY(group_id) REFERENCES groups(id) ON DELETE CASCADE,
  CONSTRAINT fk_group_users_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_group_users_group_user ON group_users(group_id, user_id);

CREATE TABLE access_rules(
  id bigserial PRIMARY KEY,
  name text NOT NULL,
  description text,
  enabled boolean DEFAULT true,
  protocol text NOT NULL,
  ports text,
  bidirectional boolean DEFAULT false,
  expires_at timestamptz,
  created_at timestamptz,
  updated_at timestamptz,
  builtin text
);

CREATE TABLE access_rule_groups(
  id bigserial PRIMARY KEY,
  rule_id bigint NOT NULL,
  group_id bigint NOT NULL,
  side text NOT NULL,
  CONSTRAINT fk_access_rule_groups_rule FOREIGN KEY(rule_id) REFERENCES access_rules(id) ON DELETE CASCADE,
  CONSTRAINT fk_access_rule_groups_group FOREIGN KEY(group_id) REFERENCES groups(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_access_rule_groups_rule_group_side ON access_rule_groups(rule_id, group_id, side);

CREATE TABLE postures(
  id bigserial PRIMARY KEY,
  name text NOT NULL,
  description text,
  expressions text NOT NULL,
  schedule text,
  created_at timestamptz,
  updated_at timestamptz
);
CREATE UNIQUE INDEX idx_postures_name ON postures(name);

CREATE TABLE access_rule_postures(
  id bigserial PRIMARY KEY,
  rule_id bigint NOT NULL,
  posture_id bigint NOT NULL,
  CONSTRAINT fk_access_rule_postures_rule FOREIGN KEY(rule_id) REFERENCES access_rules(id) ON DELETE CASCADE,
  CONSTRAINT fk_access_rule_postures_posture FOREIGN KEY(posture_id) REFERENCES postures(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_access_rule_postures_rule_posture ON access_rule_postures(rule_id, posture_id);

-- networks are sets of prefixes reached through routing nodes, the way
-- NetBird's networks work; see docs/ref/networks.md. The routers
-- advertise the prefixes and the network approves them; the groups get
-- the routes.
CREATE TABLE access_requests(
  id bigserial PRIMARY KEY,
  user_id bigint NOT NULL,
  node_id bigint,
  group_id bigint NOT NULL,
  reason text,
  duration_seconds bigint NOT NULL,
  status text NOT NULL,
  decided_by text,
  note text,
  created_at timestamptz,
  decided_at timestamptz,
  expires_at timestamptz,
  CONSTRAINT fk_access_requests_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT fk_access_requests_node FOREIGN KEY(node_id) REFERENCES nodes(id) ON DELETE CASCADE,
  CONSTRAINT fk_access_requests_group FOREIGN KEY(group_id) REFERENCES groups(id) ON DELETE CASCADE
);
CREATE INDEX idx_access_requests_status ON access_requests(status, id);

CREATE TABLE networks(
  id bigserial PRIMARY KEY,
  name text NOT NULL,
  description text,
  enabled boolean DEFAULT true,
  protocol text,
  ports text,
  created_at timestamptz,
  updated_at timestamptz
);
CREATE UNIQUE INDEX idx_networks_name ON networks(name);

CREATE TABLE network_prefixes(
  id bigserial PRIMARY KEY,
  network_id bigint NOT NULL,
  prefix text NOT NULL,

  CONSTRAINT fk_network_prefixes_network FOREIGN KEY(network_id) REFERENCES networks(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_network_prefixes_network_prefix ON network_prefixes(network_id, prefix);

CREATE TABLE network_routers(
  id bigserial PRIMARY KEY,
  network_id bigint NOT NULL,
  node_id bigint NOT NULL,

  CONSTRAINT fk_network_routers_network FOREIGN KEY(network_id) REFERENCES networks(id) ON DELETE CASCADE,
  CONSTRAINT fk_network_routers_node FOREIGN KEY(node_id) REFERENCES nodes(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_network_routers_network_node ON network_routers(network_id, node_id);

CREATE TABLE network_groups(
  id bigserial PRIMARY KEY,
  network_id bigint NOT NULL,
  group_id bigint NOT NULL,

  CONSTRAINT fk_network_groups_network FOREIGN KEY(network_id) REFERENCES networks(id) ON DELETE CASCADE,
  CONSTRAINT fk_network_groups_group FOREIGN KEY(group_id) REFERENCES groups(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_network_groups_network_group ON network_groups(network_id, group_id);

CREATE TABLE group_dns_rules(
  id bigserial PRIMARY KEY,
  name text NOT NULL,
  description text,
  enabled boolean DEFAULT true,
  domains text NOT NULL,
  nameservers text NOT NULL,
  created_at timestamptz,
  updated_at timestamptz
);
CREATE UNIQUE INDEX idx_group_dns_rules_name ON group_dns_rules(name);

CREATE TABLE group_dns_rule_groups(
  id bigserial PRIMARY KEY,
  rule_id bigint NOT NULL,
  group_id bigint NOT NULL,

  CONSTRAINT fk_group_dns_rule_groups_rule FOREIGN KEY(rule_id) REFERENCES group_dns_rules(id) ON DELETE CASCADE,
  CONSTRAINT fk_group_dns_rule_groups_group FOREIGN KEY(group_id) REFERENCES groups(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_group_dns_rule_groups_rule_group ON group_dns_rule_groups(rule_id, group_id);

CREATE TABLE network_route_approvals(
  id bigserial PRIMARY KEY,
  node_id bigint NOT NULL,
  prefix text NOT NULL,
  CONSTRAINT fk_network_route_approvals_node FOREIGN KEY(node_id) REFERENCES nodes(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_network_route_approvals_node_prefix ON network_route_approvals(node_id, prefix);

CREATE TABLE policies(
  id bigserial PRIMARY KEY,
  created_at timestamptz,
  updated_at timestamptz,
  deleted_at timestamptz,
  data text
);
CREATE INDEX idx_policies_deleted_at ON policies(deleted_at);

CREATE TABLE database_versions(
  id bigserial PRIMARY KEY,
  version text NOT NULL,
  updated_at timestamptz
);
CREATE TABLE settings(
  key text PRIMARY KEY,
  value text,
  updated_at timestamptz
);

CREATE TABLE sessions(
  id bigserial PRIMARY KEY,
  token_hash bytea NOT NULL,
  user_id bigint NOT NULL,
  created_at timestamptz,
  expires_at timestamptz,
  last_seen_at timestamptz,
  CONSTRAINT fk_sessions_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_sessions_token_hash ON sessions(token_hash);

CREATE TABLE audit_events(
  id bigserial PRIMARY KEY,
  created_at timestamptz NOT NULL,
  actor_kind text NOT NULL,
  actor_user_id bigint,
  actor_name text,
  action text NOT NULL,
  target_kind text,
  target_id text,
  target_name text,
  outcome bigint NOT NULL,
  detail text,
  remote_addr text
);
CREATE INDEX idx_audit_events_created_at ON audit_events(created_at);
CREATE INDEX idx_audit_events_actor_user_id ON audit_events(actor_user_id);

CREATE TABLE webhooks(
  id bigserial PRIMARY KEY,
  url text NOT NULL,
  description text,
  provider_type text,
  secret text NOT NULL,
  subscriptions text NOT NULL,
  created_by bigint,
  created_at timestamptz,
  updated_at timestamptz,
  last_delivery_at timestamptz,
  last_delivery_status text
);

CREATE TABLE webhook_deliveries(
  id bigserial PRIMARY KEY,
  webhook_id bigint NOT NULL,
  event_type text NOT NULL,
  status text NOT NULL,
  ok boolean NOT NULL,
  attempts bigint NOT NULL,
  duration_ms bigint NOT NULL,
  created_at timestamptz NOT NULL,
  CONSTRAINT fk_webhook_deliveries_webhook FOREIGN KEY(webhook_id) REFERENCES webhooks(id) ON DELETE CASCADE
);
CREATE INDEX idx_webhook_deliveries_webhook ON webhook_deliveries(webhook_id, id);

CREATE TABLE log_streams(
  id bigserial PRIMARY KEY,
  name text NOT NULL,
  destination text NOT NULL,
  url text NOT NULL,
  token text,
  enabled boolean NOT NULL,
  created_by bigint,
  created_at timestamptz,
  updated_at timestamptz,
  last_delivery_at timestamptz,
  last_delivery_status text,
  delivered bigint NOT NULL DEFAULT 0,
  dropped bigint NOT NULL DEFAULT 0
);

CREATE TABLE ssh_recordings(
  id bigserial PRIMARY KEY,
  started_at timestamptz NOT NULL,
  ended_at timestamptz,
  src_node text,
  src_node_id text,
  src_user text,
  dst_node_id bigint,
  dst_node text,
  ssh_user text,
  local_user text,
  command text,
  size bigint NOT NULL DEFAULT 0,
  path text NOT NULL,
  complete boolean NOT NULL DEFAULT false
);
CREATE INDEX idx_ssh_recordings_started ON ssh_recordings(started_at);
