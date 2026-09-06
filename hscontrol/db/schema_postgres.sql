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
  role text
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
  last_seen timestamptz
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
  CONSTRAINT fk_nodes_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT fk_nodes_auth_key FOREIGN KEY(auth_key_id) REFERENCES pre_auth_keys(id)
);

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
