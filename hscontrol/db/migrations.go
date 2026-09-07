package db

import (
	"encoding/json/v2"
	"fmt"
	"net/netip"
	"slices"

	"github.com/juanfont/headscale/hscontrol/policy"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/rs/zerolog/log"
)

// migrations is the schema history. New migrations go at the end and every
// step is explicit SQL: a migration must describe the exact change, never
// derive it from the current shape of a Go type, so replaying the history
// on an old database gives the same result years later.
//
// Migrations start from v0.25.0. If upgrading from v0.24.x or earlier, you
// must first upgrade to v0.25.1 before upgrading to this version.
//
// Raw SQL uses $1..$n placeholders, which both drivers accept. Statements
// that only make sense on one dialect check tx.ex.dialect.
//
//nolint:funlen,maintidx,gocognit // legacy: the migration list is a flat, immutable history
func migrations(cfg *types.Config) []migration {
	return []migration{
		// v0.25.0
		{
			// Remove routes that no longer belong to a node. The routes
			// table itself is dropped by 202502131714.
			id: "202501221827",
			run: func(tx *Tx) error {
				hasRoutes, err := tx.ex.hasTable("routes")
				if err != nil {
					return err
				}

				if !hasRoutes {
					return nil
				}

				hasNodes, err := tx.ex.hasTable("nodes")
				if err != nil {
					return err
				}

				if hasNodes {
					_, err = tx.ex.execRaw("DELETE FROM routes WHERE node_id NOT IN (SELECT id FROM nodes)")
					if err != nil {
						return fmt.Errorf("deleting routes without node: %w", err)
					}
				}

				_, err = tx.ex.execRaw("DELETE FROM routes WHERE node_id IS NULL")
				if err != nil {
					return fmt.Errorf("deleting routes with null node: %w", err)
				}

				return nil
			},
		},
		{
			// Historically an AutoMigrate of the pre-auth key and node
			// tables that added the foreign key from nodes to pre-auth
			// keys. SQLite recreates every table with its constraints in
			// 202507021200, and PostgreSQL gets the constraint added
			// explicitly here when it is missing.
			id: "202501311657",
			run: func(tx *Tx) error {
				if tx.ex.dialect != dialectPostgres {
					return nil
				}

				return tx.ex.addForeignKeyIfMissing(
					"nodes", "fk_nodes_auth_key",
					"FOREIGN KEY (auth_key_id) REFERENCES pre_auth_keys(id)",
				)
			},
		},
		{
			// Ensure there are no nodes referring to a deleted preauthkey.
			id: "202502070949",
			run: func(tx *Tx) error {
				hasKeys, err := tx.ex.hasTable("pre_auth_keys")
				if err != nil {
					return err
				}

				if !hasKeys {
					return nil
				}

				_, err = tx.ex.execRaw(`
UPDATE nodes
SET auth_key_id = NULL
WHERE auth_key_id IS NOT NULL
AND auth_key_id NOT IN (
    SELECT id FROM pre_auth_keys
);
`)
				if err != nil {
					return fmt.Errorf("setting auth_key to null on nodes with non-existing keys: %w", err)
				}

				return nil
			},
		},
		// v0.26.0
		{
			// Migrate all routes from the Route table to the new field
			// ApprovedRoutes in the Node table. Then drop the Route table.
			id:  "202502131714",
			run: migrateRoutesToApprovedRoutes,
		},
		{
			id: "202502171819",
			run: func(_ *Tx) error {
				// This migration originally removed the last_seen column
				// from the node table, but it was added back in
				// 202505091439.
				return nil
			},
		},
		{
			// Add back last_seen column to node table if it does not
			// exist. This is a workaround for the fact that the last_seen
			// column was removed in the 202502171819 migration, but only
			// for some beta testers.
			id: "202505091439",
			run: func(tx *Tx) error {
				return tx.ex.addColumnIfMissing("nodes", "last_seen", typeTimestamp)
			},
		},
		{
			// Fix the provider identifier for users that have a double
			// slash in the provider identifier.
			id:  "202505141324",
			run: cleanUserProviderIdentifiers,
		},
		// v0.27.0
		{
			// Schema migration to ensure all tables match the expected
			// schema. This migration recreates all tables to match the
			// exact structure in schema.sql, preserving all data during
			// the process. Only SQLite will be migrated for consistency.
			id:  "202507021200",
			run: recreateSQLiteTables,
		},
		// v0.27.1
		{
			// Drop all tables that are no longer in use and has existed.
			// They potentially still present from broken migrations in
			// the past.
			id: "202510311551",
			run: func(tx *Tx) error {
				for _, oldTable := range []string{
					"namespaces", "machines", "shared_machines",
					"kvs", "pre_auth_key_acl_tags", "routes",
				} {
					err := tx.ex.dropTableIfExists(oldTable)
					if err != nil {
						return err
					}
				}

				return nil
			},
		},
		{
			// Drop all indices that are no longer in use and has existed.
			// They potentially still present from broken migrations in
			// the past. They should all be cleaned up by the db engine,
			// but we are a bit conservative to ensure all our previous
			// mess is cleaned up.
			id: "202511101554-drop-old-idx",
			run: func(tx *Tx) error {
				for _, oldIdx := range []string{
					"idx_namespaces_deleted_at",
					"idx_routes_deleted_at",
					"idx_shared_machines_deleted_at",
				} {
					err := tx.ex.dropIndexIfExists(oldIdx)
					if err != nil {
						return err
					}
				}

				return nil
			},
		},

		// Migrations **above** this points were written for GORM's
		// AutoMigrate and have been translated into the explicit
		// statements they produced on the databases that existed at the
		// time.

		// From this point, the following rules must be followed:
		// - Write the exact migration steps needed; never derive them
		//   from the current Go types, which change over time.
		// - Never write migrations that requires foreign keys to be disabled.
		// - ALL errors in migrations must be handled properly.

		{
			// Add columns for prefix and hash for pre auth keys,
			// implementing them with the same security model as api keys.
			id: "202511011637-preauthkey-bcrypt",
			run: func(tx *Tx) error {
				err := tx.ex.addColumnIfMissing("pre_auth_keys", "prefix", typeText)
				if err != nil {
					return err
				}

				err = tx.ex.addColumnIfMissing("pre_auth_keys", "hash", typeBlob)
				if err != nil {
					return err
				}

				// Create partial unique index to allow multiple legacy
				// keys (NULL/empty prefix) while enforcing uniqueness for
				// new bcrypt-based keys.
				_, err = tx.ex.execRaw(
					"CREATE UNIQUE INDEX IF NOT EXISTS idx_pre_auth_keys_prefix " +
						"ON pre_auth_keys(prefix) WHERE prefix IS NOT NULL AND prefix != ''",
				)
				if err != nil {
					return fmt.Errorf("creating prefix index: %w", err)
				}

				return nil
			},
		},
		{
			// Reformat multi-line indexes to single-line for consistency.
			// This migration drops and recreates the three user identity
			// indexes to match the single-line format expected by schema
			// validation.
			id: "202511122344-remove-newline-index",
			run: func(tx *Tx) error {
				err := tx.ex.execAll("dropping index", []string{
					`DROP INDEX IF EXISTS idx_provider_identifier`,
					`DROP INDEX IF EXISTS idx_name_provider_identifier`,
					`DROP INDEX IF EXISTS idx_name_no_provider_identifier`,
				})
				if err != nil {
					return err
				}

				return tx.ex.execAll("creating index", []string{
					"CREATE UNIQUE INDEX idx_provider_identifier " +
						"ON users(provider_identifier) WHERE provider_identifier IS NOT NULL",
					`CREATE UNIQUE INDEX idx_name_provider_identifier ON users(name, provider_identifier)`,
					"CREATE UNIQUE INDEX idx_name_no_provider_identifier " +
						"ON users(name) WHERE provider_identifier IS NULL",
				})
			},
		},
		{
			// Rename forced_tags column to tags in nodes table.
			// This must run after migration 202505141324 which creates
			// tables with forced_tags.
			id: "202511131445-node-forced-tags-to-tags",
			run: func(tx *Tx) error {
				return tx.ex.renameColumn("nodes", "forced_tags", "tags")
			},
		},
		{
			// Migrate RequestTags from host_info JSON to tags column.
			// In 0.27.x, tags from --advertise-tags (ValidTags) were
			// stored only in host_info.RequestTags, not in the tags column
			// (formerly forced_tags). This migration validates RequestTags
			// against the policy's tagOwners and merges validated tags
			// into the tags column.
			// Fixes: https://github.com/juanfont/headscale/issues/3006
			id: "202601121700-migrate-hostinfo-request-tags",
			run: func(tx *Tx) error {
				return migrateHostinfoRequestTags(tx, cfg)
			},
		},
		{
			// Clear user_id on tagged nodes.
			// Tagged nodes are owned by their tags, not a user.
			// Previously user_id was kept as "created by" tracking,
			// but this prevents deleting users whose nodes have been
			// tagged, and the ON DELETE CASCADE FK would destroy the
			// tagged nodes if the user were deleted.
			//
			// A nil tags slice marshals to the JSON literal 'null', so
			// untagged nodes can carry tags='null'. That spelling must be
			// excluded alongside '[]' and '' or untagged nodes lose their
			// user. Nodes already detached by the earlier version of this
			// migration are repaired by the recovery migration below.
			// Fixes: https://github.com/juanfont/headscale/issues/3077
			// Fixes: https://github.com/juanfont/headscale/issues/3323
			id: "202602201200-clear-tagged-node-user-id",
			run: func(tx *Tx) error {
				_, err := tx.ex.execRaw(`
UPDATE nodes
SET user_id = NULL
WHERE tags IS NOT NULL AND tags != '[]' AND tags != '' AND tags != 'null';
`)
				if err != nil {
					return fmt.Errorf("clearing user_id on tagged nodes: %w", err)
				}

				return nil
			},
		},
		{
			// Clear zero-time node expiry values to NULL.
			// Versions before 0.28 persisted a pointer to a zero
			// time.Time as '0001-01-01 00:00:00+00:00' rather than
			// NULL, which 0.29 reports as an expired node. This
			// normalises the existing rows so the column once
			// again means "no expiry" when unset.
			id: "202605221435-clear-zero-time-node-expiry",
			run: func(tx *Tx) error {
				_, err := tx.ex.execRaw(`
UPDATE nodes
SET expiry = NULL
WHERE expiry IS NOT NULL AND expiry < '1900-01-01';
`)
				if err != nil {
					return fmt.Errorf("clearing zero-time node expiry: %w", err)
				}

				return nil
			},
		},
		{
			// Recover user_id on untagged nodes detached by the earlier
			// version of 202602201200-clear-tagged-node-user-id, which
			// treated tags='null' as tagged and cleared the user. This
			// repairs databases that already upgraded to 0.29.0; fresh
			// upgrades are protected by the fixed migration above and find
			// nothing to repair. Recovery is best-effort: the owner is
			// re-derived from the node's pre-auth key, so nodes registered
			// via CLI/OIDC (no pre-auth key) cannot be recovered and must
			// be reassigned manually.
			// Fixes: https://github.com/juanfont/headscale/issues/3323
			id: "202606181200-recover-null-tags-node-user-id",
			run: func(tx *Tx) error {
				_, err := tx.ex.execRaw(`
UPDATE nodes
SET user_id = (
	SELECT pak.user_id FROM pre_auth_keys pak WHERE pak.id = nodes.auth_key_id
)
WHERE user_id IS NULL
	AND auth_key_id IS NOT NULL
	AND (tags IS NULL OR tags = '' OR tags = '[]' OR tags = 'null');
`)
				if err != nil {
					return fmt.Errorf("recovering user_id on untagged nodes: %w", err)
				}

				return nil
			},
		},
		{
			// Add an optional owning user to API keys so the v2 API can
			// create user-owned (untagged) auth keys, mirroring Tailscale's
			// "key owned by the creating identity".
			id: "202606191500-api-key-user-id",
			run: func(tx *Tx) error {
				return tx.ex.addColumnIfMissing("api_keys", "user_id", typeInteger)
			},
		},
		{
			// Add a free-text description to pre-auth keys, set via the
			// v2 keys API.
			id: "202606191501-pre-auth-key-description",
			run: func(tx *Tx) error {
				return tx.ex.addColumnIfMissing("pre_auth_keys", "description", typeText)
			},
		},
		{
			// Add a revoked timestamp to pre-auth keys. The v2 API's DELETE
			// soft-revokes a key (set revoked = now) rather than destroying
			// it; the row is reaped later by the background collector.
			id: "202606201200-pre-auth-key-revoked",
			run: func(tx *Tx) error {
				return tx.ex.addColumnIfMissing("pre_auth_keys", "revoked", typeTimestamp)
			},
		},
		{
			// Add the OAuth client + access token tables backing the v2
			// API's OAuth client-credentials flow. They mirror the
			// api_keys / pre_auth_keys security model: a public id/prefix
			// plus an Argon2id hash of the secret. The DDL matches
			// schema.sql (SQLite) and schema_postgres.sql byte for byte.
			id:  "202606211200-oauth-clients-and-tokens",
			run: createOAuthTables,
		},
		{
			// Clear stale key expiry on tagged nodes. A tagged node is
			// owned by its tags and never expires (KB 1068), but a buggy
			// handleLogout stamped a past expiry on it, leaving it
			// permanently Expired and unable to re-authenticate. The
			// buggy writer is fixed, so this only repairs rows written
			// before the upgrade; a fixed server cannot recreate them.
			// Match the tagged-node predicate the earlier
			// clear-tagged-node-user-id migration uses (a nil tags slice
			// marshals to 'null', so exclude it).
			// Fixes: https://github.com/juanfont/headscale/issues/3371
			id: "202607241200-clear-tagged-node-expiry",
			run: func(tx *Tx) error {
				_, err := tx.ex.execRaw(`
UPDATE nodes
SET expiry = NULL
WHERE tags IS NOT NULL AND tags != '[]' AND tags != '' AND tags != 'null'
	AND expiry IS NOT NULL;
`)
				if err != nil {
					return fmt.Errorf("clearing expiry on tagged nodes: %w", err)
				}

				return nil
			},
		},
		{
			// Users gain an administrative role (see types.Role). Every
			// existing user is a member; the operator promotes the first
			// owner with `headscale users set-role`, and a database that
			// has no users yet makes the first user created its owner.
			id: "202609062100-user-role",
			run: func(tx *Tx) error {
				err := tx.ex.addColumnIfMissing("users", "role", typeText)
				if err != nil {
					return err
				}

				_, err = tx.ex.execRaw(`UPDATE users SET role = 'member' WHERE role IS NULL OR role = ''`)
				if err != nil {
					return fmt.Errorf("defaulting user roles: %w", err)
				}

				return nil
			},
		},
		{
			// Device and user approval: nodes and users gain approved_at,
			// pre-auth keys gain preauthorized, and the settings table
			// holds the two approval switches. Everything that exists
			// when the migration runs was admitted under the old rules,
			// so it is backfilled as approved and every existing key as
			// preauthorized; the switches default to off.
			id:  "202609070900-approval",
			run: migrateApproval,
		},
		{
			// Node sharing: node_shares records the users a node is
			// shared with, so members can give each other access to
			// a device without an administrator editing the policy.
			id:  "202609071200-node-shares",
			run: migrateNodeShares,
		},
		{
			// Global exit node: nodes gain global_exit_node, off for
			// everything that exists.
			id: "202609071500-global-exit-node",
			run: func(tx *Tx) error {
				err := tx.ex.addColumnIfMissing("nodes", "global_exit_node", typeBoolFalse)
				if err != nil {
					return err
				}

				_, err = tx.ex.execRaw(`UPDATE nodes SET global_exit_node = false WHERE global_exit_node IS NULL`)
				if err != nil {
					return fmt.Errorf("backfilling nodes.global_exit_node: %w", err)
				}

				return nil
			},
		},
		{
			// Console sign-in through the identity provider: sessions maps
			// a browser's cookie token to a user until it expires.
			id:  "202609080900-sessions",
			run: migrateSessions,
		},
		{
			// Audit log: audit_events records every writing API request
			// and the server's own sign-in events, by value, so the
			// history outlives the users and objects it names.
			id:  "202609080930-audit-events",
			run: migrateAuditEvents,
		},
		{
			// Access control: groups of nodes and users, access rules
			// between groups, and the groups a pre-auth key enrols
			// nodes into. See docs/ref/access-control.md.
			id:  "202609081000-access-groups",
			run: migrateAccessGroups,
		},
		{
			// Networks: prefixes reached through routing nodes and handed
			// out to groups, NetBird style. See docs/ref/networks.md.
			id:  "202609091000-networks",
			run: migrateNetworks,
		},
	}
}

// migrateAccessGroups (202609081000) creates the groups, group_nodes,
// group_users, access_rules and access_rule_groups tables and adds
// pre_auth_keys.groups.
func migrateAccessGroups(tx *Tx) error {
	err := tx.ex.addColumnIfMissing("pre_auth_keys", "groups", typeText)
	if err != nil {
		return err
	}

	tables := []tableDefinition{
		{
			name: "groups",
			sqlite: `CREATE TABLE groups(
  id integer PRIMARY KEY AUTOINCREMENT,
  name text NOT NULL,
  description text,
  builtin text,
  created_at datetime,
  updated_at datetime
)`,
			postgres: `CREATE TABLE groups(
  id bigserial PRIMARY KEY,
  name text NOT NULL,
  description text,
  builtin text,
  created_at timestamptz,
  updated_at timestamptz
)`,
			indexes: []string{`CREATE UNIQUE INDEX idx_groups_name ON groups(name)`},
		},
		{
			name: "group_nodes",
			sqlite: `CREATE TABLE group_nodes(
  id integer PRIMARY KEY AUTOINCREMENT,
  group_id integer NOT NULL,
  node_id integer NOT NULL,
  created_at datetime,
  CONSTRAINT fk_group_nodes_group FOREIGN KEY(group_id) REFERENCES groups(id) ON DELETE CASCADE,
  CONSTRAINT fk_group_nodes_node FOREIGN KEY(node_id) REFERENCES nodes(id) ON DELETE CASCADE
)`,
			postgres: `CREATE TABLE group_nodes(
  id bigserial PRIMARY KEY,
  group_id bigint NOT NULL,
  node_id bigint NOT NULL,
  created_at timestamptz,
  CONSTRAINT fk_group_nodes_group FOREIGN KEY(group_id) REFERENCES groups(id) ON DELETE CASCADE,
  CONSTRAINT fk_group_nodes_node FOREIGN KEY(node_id) REFERENCES nodes(id) ON DELETE CASCADE
)`,
			indexes: []string{`CREATE UNIQUE INDEX idx_group_nodes_group_node ON group_nodes(group_id, node_id)`},
		},
		{
			name: "group_users",
			sqlite: `CREATE TABLE group_users(
  id integer PRIMARY KEY AUTOINCREMENT,
  group_id integer NOT NULL,
  user_id integer NOT NULL,
  created_at datetime,
  CONSTRAINT fk_group_users_group FOREIGN KEY(group_id) REFERENCES groups(id) ON DELETE CASCADE,
  CONSTRAINT fk_group_users_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
)`,
			postgres: `CREATE TABLE group_users(
  id bigserial PRIMARY KEY,
  group_id bigint NOT NULL,
  user_id bigint NOT NULL,
  created_at timestamptz,
  CONSTRAINT fk_group_users_group FOREIGN KEY(group_id) REFERENCES groups(id) ON DELETE CASCADE,
  CONSTRAINT fk_group_users_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
)`,
			indexes: []string{`CREATE UNIQUE INDEX idx_group_users_group_user ON group_users(group_id, user_id)`},
		},
		{
			name: "access_rules",
			sqlite: `CREATE TABLE access_rules(
  id integer PRIMARY KEY AUTOINCREMENT,
  name text NOT NULL,
  description text,
  enabled numeric DEFAULT true,
  protocol text NOT NULL,
  ports text,
  bidirectional numeric DEFAULT false,
  created_at datetime,
  updated_at datetime
)`,
			postgres: `CREATE TABLE access_rules(
  id bigserial PRIMARY KEY,
  name text NOT NULL,
  description text,
  enabled boolean DEFAULT true,
  protocol text NOT NULL,
  ports text,
  bidirectional boolean DEFAULT false,
  created_at timestamptz,
  updated_at timestamptz
)`,
		},
		{
			name: "access_rule_groups",
			sqlite: `CREATE TABLE access_rule_groups(
  id integer PRIMARY KEY AUTOINCREMENT,
  rule_id integer NOT NULL,
  group_id integer NOT NULL,
  side text NOT NULL,
  CONSTRAINT fk_access_rule_groups_rule FOREIGN KEY(rule_id) REFERENCES access_rules(id) ON DELETE CASCADE,
  CONSTRAINT fk_access_rule_groups_group FOREIGN KEY(group_id) REFERENCES groups(id) ON DELETE CASCADE
)`,
			postgres: `CREATE TABLE access_rule_groups(
  id bigserial PRIMARY KEY,
  rule_id bigint NOT NULL,
  group_id bigint NOT NULL,
  side text NOT NULL,
  CONSTRAINT fk_access_rule_groups_rule FOREIGN KEY(rule_id) REFERENCES access_rules(id) ON DELETE CASCADE,
  CONSTRAINT fk_access_rule_groups_group FOREIGN KEY(group_id) REFERENCES groups(id) ON DELETE CASCADE
)`,
			indexes: []string{
				`CREATE UNIQUE INDEX idx_access_rule_groups_rule_group_side
  ON access_rule_groups(rule_id, group_id, side)`,
			},
		},
	}

	return createTables(tx, tables)
}

// tableDefinition is a table a migration creates, with the DDL per
// dialect and its indexes.
type tableDefinition struct {
	name             string
	sqlite, postgres string
	indexes          []string
}

// createTables creates the tables that do not exist yet.
func createTables(tx *Tx, tables []tableDefinition) error {
	for _, t := range tables {
		exists, err := tx.ex.hasTable(t.name)
		if err != nil {
			return err
		}

		if exists {
			continue
		}

		ddl := t.sqlite
		if tx.ex.dialect == dialectPostgres {
			ddl = t.postgres
		}

		err = tx.ex.execAll("creating "+t.name+" table", append([]string{ddl}, t.indexes...))
		if err != nil {
			return err
		}
	}

	return nil
}

// migrateNetworks (202609091000) creates the networks, network_prefixes,
// network_routers and network_groups tables.
func migrateNetworks(tx *Tx) error {
	tables := []tableDefinition{
		{
			name: "networks",
			sqlite: `CREATE TABLE networks(
  id integer PRIMARY KEY AUTOINCREMENT,
  name text NOT NULL,
  description text,
  enabled numeric DEFAULT true,
  created_at datetime,
  updated_at datetime
)`,
			postgres: `CREATE TABLE networks(
  id bigserial PRIMARY KEY,
  name text NOT NULL,
  description text,
  enabled boolean DEFAULT true,
  created_at timestamptz,
  updated_at timestamptz
)`,
			indexes: []string{
				`CREATE UNIQUE INDEX idx_networks_name ON networks(name)`,
			},
		},
		{
			name: "network_prefixes",
			sqlite: `CREATE TABLE network_prefixes(
  id integer PRIMARY KEY AUTOINCREMENT,
  network_id integer NOT NULL,
  prefix text NOT NULL,
  CONSTRAINT fk_network_prefixes_network FOREIGN KEY(network_id) REFERENCES networks(id) ON DELETE CASCADE
)`,
			postgres: `CREATE TABLE network_prefixes(
  id bigserial PRIMARY KEY,
  network_id bigint NOT NULL,
  prefix text NOT NULL,
  CONSTRAINT fk_network_prefixes_network FOREIGN KEY(network_id) REFERENCES networks(id) ON DELETE CASCADE
)`,
			indexes: []string{
				`CREATE UNIQUE INDEX idx_network_prefixes_network_prefix ON network_prefixes(network_id, prefix)`,
			},
		},
		{
			name: "network_routers",
			sqlite: `CREATE TABLE network_routers(
  id integer PRIMARY KEY AUTOINCREMENT,
  network_id integer NOT NULL,
  node_id integer NOT NULL,
  CONSTRAINT fk_network_routers_network FOREIGN KEY(network_id) REFERENCES networks(id) ON DELETE CASCADE,
  CONSTRAINT fk_network_routers_node FOREIGN KEY(node_id) REFERENCES nodes(id) ON DELETE CASCADE
)`,
			postgres: `CREATE TABLE network_routers(
  id bigserial PRIMARY KEY,
  network_id bigint NOT NULL,
  node_id bigint NOT NULL,
  CONSTRAINT fk_network_routers_network FOREIGN KEY(network_id) REFERENCES networks(id) ON DELETE CASCADE,
  CONSTRAINT fk_network_routers_node FOREIGN KEY(node_id) REFERENCES nodes(id) ON DELETE CASCADE
)`,
			indexes: []string{
				`CREATE UNIQUE INDEX idx_network_routers_network_node ON network_routers(network_id, node_id)`,
			},
		},
		{
			name: "network_groups",
			sqlite: `CREATE TABLE network_groups(
  id integer PRIMARY KEY AUTOINCREMENT,
  network_id integer NOT NULL,
  group_id integer NOT NULL,
  CONSTRAINT fk_network_groups_network FOREIGN KEY(network_id) REFERENCES networks(id) ON DELETE CASCADE,
  CONSTRAINT fk_network_groups_group FOREIGN KEY(group_id) REFERENCES groups(id) ON DELETE CASCADE
)`,
			postgres: `CREATE TABLE network_groups(
  id bigserial PRIMARY KEY,
  network_id bigint NOT NULL,
  group_id bigint NOT NULL,
  CONSTRAINT fk_network_groups_network FOREIGN KEY(network_id) REFERENCES networks(id) ON DELETE CASCADE,
  CONSTRAINT fk_network_groups_group FOREIGN KEY(group_id) REFERENCES groups(id) ON DELETE CASCADE
)`,
			indexes: []string{
				`CREATE UNIQUE INDEX idx_network_groups_network_group ON network_groups(network_id, group_id)`,
			},
		},
	}

	return createTables(tx, tables)
}

// migrateSessions (202609080900) creates the sessions table.
func migrateSessions(tx *Tx) error {
	hasSessions, err := tx.ex.hasTable("sessions")
	if err != nil {
		return err
	}

	if hasSessions {
		return nil
	}

	ddl := `CREATE TABLE sessions(
  id integer PRIMARY KEY AUTOINCREMENT,
  token_hash blob NOT NULL,
  user_id integer NOT NULL,
  created_at datetime,
  expires_at datetime,
  last_seen_at datetime,
  CONSTRAINT fk_sessions_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
)`
	if tx.ex.dialect == dialectPostgres {
		ddl = `CREATE TABLE sessions(
  id bigserial PRIMARY KEY,
  token_hash bytea NOT NULL,
  user_id bigint NOT NULL,
  created_at timestamptz,
  expires_at timestamptz,
  last_seen_at timestamptz,
  CONSTRAINT fk_sessions_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
)`
	}

	return tx.ex.execAll("creating sessions table", []string{
		ddl,
		`CREATE UNIQUE INDEX idx_sessions_token_hash ON sessions(token_hash)`,
	})
}

// migrateAuditEvents (202609080930) creates the audit_events table.
func migrateAuditEvents(tx *Tx) error {
	hasEvents, err := tx.ex.hasTable("audit_events")
	if err != nil {
		return err
	}

	if hasEvents {
		return nil
	}

	ddl := `CREATE TABLE audit_events(
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
)`
	if tx.ex.dialect == dialectPostgres {
		ddl = `CREATE TABLE audit_events(
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
)`
	}

	return tx.ex.execAll("creating audit_events table", []string{
		ddl,
		`CREATE INDEX idx_audit_events_created_at ON audit_events(created_at)`,
		`CREATE INDEX idx_audit_events_actor_user_id ON audit_events(actor_user_id)`,
	})
}

// migrateNodeShares (202609071200) creates the node_shares table.
func migrateNodeShares(tx *Tx) error {
	hasShares, err := tx.ex.hasTable("node_shares")
	if err != nil {
		return err
	}

	if hasShares {
		return nil
	}

	ddl := `CREATE TABLE node_shares(
  id integer PRIMARY KEY AUTOINCREMENT,
  node_id integer NOT NULL,
  user_id integer NOT NULL,
  created_by integer,
  created_at datetime,
  CONSTRAINT fk_node_shares_node FOREIGN KEY(node_id) REFERENCES nodes(id) ON DELETE CASCADE,
  CONSTRAINT fk_node_shares_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
)`
	if tx.ex.dialect == dialectPostgres {
		ddl = `CREATE TABLE node_shares(
  id bigserial PRIMARY KEY,
  node_id bigint NOT NULL,
  user_id bigint NOT NULL,
  created_by bigint,
  created_at timestamptz,
  CONSTRAINT fk_node_shares_node FOREIGN KEY(node_id) REFERENCES nodes(id) ON DELETE CASCADE,
  CONSTRAINT fk_node_shares_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
)`
	}

	return tx.ex.execAll("creating node_shares table", []string{
		ddl,
		`CREATE UNIQUE INDEX idx_node_shares_node_user ON node_shares(node_id, user_id)`,
	})
}

// migrateApproval (202609070900) adds the approval columns and the
// settings table.
func migrateApproval(tx *Tx) error {
	columns := []struct {
		table, column string
		typ           columnType
		backfill      string
	}{
		{"nodes", "approved_at", typeTimestamp, `UPDATE nodes SET approved_at = created_at WHERE approved_at IS NULL`},
		{"users", "approved_at", typeTimestamp, `UPDATE users SET approved_at = created_at WHERE approved_at IS NULL`},
		{
			"pre_auth_keys", "preauthorized", typeBoolTrue,
			`UPDATE pre_auth_keys SET preauthorized = true WHERE preauthorized IS NULL`,
		},
	}

	for _, c := range columns {
		err := tx.ex.addColumnIfMissing(c.table, c.column, c.typ)
		if err != nil {
			return err
		}

		_, err = tx.ex.execRaw(c.backfill)
		if err != nil {
			return fmt.Errorf("backfilling %s.%s: %w", c.table, c.column, err)
		}
	}

	hasSettings, err := tx.ex.hasTable("settings")
	if err != nil {
		return err
	}

	if hasSettings {
		return nil
	}

	ddl := `CREATE TABLE settings(
  key text PRIMARY KEY,
  value text,
  updated_at datetime
)`
	if tx.ex.dialect == dialectPostgres {
		ddl = `CREATE TABLE settings(
  key text PRIMARY KEY,
  value text,
  updated_at timestamptz
)`
	}

	return tx.ex.execAll("creating settings table", []string{ddl})
}

// migrateRoutesToApprovedRoutes (202502131714) denormalises the enabled
// routes of the dropped routes table onto nodes.approved_routes.
func migrateRoutesToApprovedRoutes(tx *Tx) error {
	err := tx.ex.addColumnIfMissing("nodes", "approved_routes", typeText)
	if err != nil {
		return err
	}

	hasRoutes, err := tx.ex.hasTable("routes")
	if err != nil {
		return err
	}

	if !hasRoutes {
		return nil
	}

	nodeRoutes, err := enabledRoutesByNode(tx)
	if err != nil {
		return err
	}

	for nodeID, routes := range nodeRoutes {
		slices.SortFunc(routes, netip.Prefix.Compare)
		routes = slices.Compact(routes)

		data, err := json.Marshal(routes)
		if err != nil {
			return fmt.Errorf("encoding approved routes for node %d: %w", nodeID, err)
		}

		_, err = tx.ex.execRaw("UPDATE nodes SET approved_routes = $1 WHERE id = $2", string(data), nodeID)
		if err != nil {
			return fmt.Errorf("saving approved routes to new column: %w", err)
		}
	}

	return tx.ex.dropTableIfExists("routes")
}

// enabledRoutesByNode reads the routes table as the ORM did: soft-deleted
// rows are skipped and prefixes are stored as text.
func enabledRoutesByNode(tx *Tx) (map[uint64][]netip.Prefix, error) {
	rows, err := tx.ex.queryRaw("SELECT node_id, prefix FROM routes WHERE enabled AND deleted_at IS NULL")
	if err != nil {
		return nil, fmt.Errorf("fetching routes: %w", err)
	}

	defer func() { _ = rows.Close() }()

	nodeRoutes := map[uint64][]netip.Prefix{}

	for rows.Next() {
		var (
			nodeID uint64
			prefix string
		)

		err = rows.Scan(&nodeID, &prefix)
		if err != nil {
			return nil, fmt.Errorf("scanning route: %w", err)
		}

		pfx, parseErr := netip.ParsePrefix(prefix)
		if parseErr != nil {
			return nil, fmt.Errorf("parsing route prefix %q: %w", prefix, parseErr)
		}

		nodeRoutes[nodeID] = append(nodeRoutes[nodeID], pfx)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("reading routes: %w", err)
	}

	return nodeRoutes, nil
}

// cleanUserProviderIdentifiers (202505141324) normalises provider
// identifiers that carried a double slash.
func cleanUserProviderIdentifiers(tx *Tx) error {
	users, err := listUsersBeforeRoles(tx)
	if err != nil {
		return fmt.Errorf("listing users: %w", err)
	}

	for _, user := range users {
		cleaned := types.CleanIdentifier(user.ProviderIdentifier.String)
		if cleaned == user.ProviderIdentifier.String {
			continue
		}

		user.ProviderIdentifier.String = cleaned

		err := UpdateUser(tx, &user)
		if err != nil {
			return fmt.Errorf("saving user: %w", err)
		}
	}

	return nil
}

// recreateSQLiteTables (202507021200) rebuilds every SQLite table to the
// shape of schema.sql at the time, copying all data across.
//
//nolint:funlen // legacy: one migration, kept as the sequence of statements it runs
func recreateSQLiteTables(tx *Tx) error {
	if tx.ex.dialect != dialectSQLite {
		log.Info().Msg("skipping schema migration on non-SQLite database")

		return nil
	}

	log.Info().Msg("starting schema recreation with table renaming")

	// Drop the routes table if it still exists (it should have been
	// migrated already).
	hasRoutes, err := tx.ex.hasTable("routes")
	if err != nil {
		return err
	}

	if hasRoutes {
		log.Info().Msg("dropping leftover routes table")

		err = tx.ex.dropTableIfExists("routes")
		if err != nil {
			return err
		}
	}

	// Drop all indexes first to avoid conflicts.
	for _, index := range []string{
		"idx_users_deleted_at",
		"idx_provider_identifier",
		"idx_name_provider_identifier",
		"idx_name_no_provider_identifier",
		"idx_api_keys_prefix",
		"idx_policies_deleted_at",
	} {
		err = tx.ex.dropIndexIfExists(index)
		if err != nil {
			return err
		}
	}

	tablesToRename := []string{"users", "pre_auth_keys", "api_keys", "nodes", "policies"}

	for _, table := range tablesToRename {
		exists, lookupErr := tx.ex.hasTable(table)
		if lookupErr != nil {
			return lookupErr
		}

		if !exists {
			continue
		}

		// Drop old table if it exists from previous failed migration.
		err = tx.ex.dropTableIfExists(table + "_old")
		if err != nil {
			return err
		}

		_, err = tx.ex.execRaw("ALTER TABLE " + table + " RENAME TO " + table + "_old")
		if err != nil {
			return fmt.Errorf("renaming table %s to %s_old: %w", table, table, err)
		}
	}

	err = tx.ex.execAll("creating new table", []string{
		`CREATE TABLE users(
  id integer PRIMARY KEY AUTOINCREMENT,
  name text,
  display_name text,
  email text,
  provider_identifier text,
  provider text,
  profile_pic_url text,
  created_at datetime,
  updated_at datetime,
  deleted_at datetime
)`,
		`CREATE TABLE pre_auth_keys(
  id integer PRIMARY KEY AUTOINCREMENT,
  key text,
  user_id integer,
  reusable numeric,
  ephemeral numeric DEFAULT false,
  used numeric DEFAULT false,
  tags text,
  expiration datetime,
  created_at datetime,
  CONSTRAINT fk_pre_auth_keys_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE SET NULL
)`,
		`CREATE TABLE api_keys(
  id integer PRIMARY KEY AUTOINCREMENT,
  prefix text,
  hash blob,
  expiration datetime,
  last_seen datetime,
  created_at datetime
)`,
		`CREATE TABLE nodes(
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
  user_id integer,
  register_method text,
  forced_tags text,
  auth_key_id integer,
  last_seen datetime,
  expiry datetime,
  approved_routes text,
  created_at datetime,
  updated_at datetime,
  deleted_at datetime,
  CONSTRAINT fk_nodes_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT fk_nodes_auth_key FOREIGN KEY(auth_key_id) REFERENCES pre_auth_keys(id)
)`,
		`CREATE TABLE policies(
  id integer PRIMARY KEY AUTOINCREMENT,
  data text,
  created_at datetime,
  updated_at datetime,
  deleted_at datetime
)`,
	})
	if err != nil {
		return err
	}

	const nodeColumns = `id, machine_key, node_key, disco_key, endpoints, host_info, ipv4, ipv6,
             hostname, given_name, user_id, register_method, forced_tags, auth_key_id, last_seen,
             expiry, approved_routes, created_at, updated_at, deleted_at`

	err = tx.ex.execAll("copying data", []string{
		`INSERT INTO users (id, name, display_name, email, provider_identifier, provider,
             profile_pic_url, created_at, updated_at, deleted_at)
             SELECT id, name, display_name, email, provider_identifier, provider, profile_pic_url,
             created_at, updated_at, deleted_at
             FROM users_old`,

		`INSERT INTO pre_auth_keys (id, key, user_id, reusable, ephemeral, used, tags,
             expiration, created_at)
             SELECT id, key, user_id, reusable, ephemeral, used, tags, expiration, created_at
             FROM pre_auth_keys_old`,

		`INSERT INTO api_keys (id, prefix, hash, expiration, last_seen, created_at)
             SELECT id, prefix, hash, expiration, last_seen, created_at
             FROM api_keys_old`,

		"INSERT INTO nodes (" + nodeColumns + ")\n" +
			"             SELECT " + nodeColumns + "\n             FROM nodes_old",

		`INSERT INTO policies (id, data, created_at, updated_at, deleted_at)
             SELECT id, data, created_at, updated_at, deleted_at
             FROM policies_old`,
	})
	if err != nil {
		return err
	}

	err = tx.ex.execAll("creating index", []string{
		"CREATE INDEX idx_users_deleted_at ON users(deleted_at)",
		`CREATE UNIQUE INDEX idx_provider_identifier ON users(
  provider_identifier
) WHERE provider_identifier IS NOT NULL`,
		`CREATE UNIQUE INDEX idx_name_provider_identifier ON users(
  name,
  provider_identifier
)`,
		`CREATE UNIQUE INDEX idx_name_no_provider_identifier ON users(
  name
) WHERE provider_identifier IS NULL`,
		"CREATE UNIQUE INDEX idx_api_keys_prefix ON api_keys(prefix)",
		"CREATE INDEX idx_policies_deleted_at ON policies(deleted_at)",
	})
	if err != nil {
		return err
	}

	// Drop old tables only after everything succeeds. A failure here is
	// not fatal: the new tables are complete and the leftovers are removed
	// by 202510311551.
	for _, table := range tablesToRename {
		err = tx.ex.dropTableIfExists(table + "_old")
		if err != nil {
			log.Warn().
				Str("table", table+"_old").
				Err(err).
				Msg("failed to drop old table, but migration succeeded")
		}
	}

	log.Info().Msg("schema recreation completed successfully")

	return nil
}

// migrateHostinfoRequestTags (202601121700) validates the RequestTags each
// node advertised against the policy and merges the authorised ones into
// the tags column.
func migrateHostinfoRequestTags(tx *Tx, cfg *types.Config) error {
	// 1. Load policy from file or database based on configuration.
	policyData, err := PolicyBytes(tx, cfg)
	if err != nil {
		log.Warn().
			Err(err).
			Msg("failed to load policy, skipping RequestTags migration " +
				"(tags will be validated on node reconnect)")

		return nil
	}

	if len(policyData) == 0 {
		log.Info().
			Msg("no policy found, skipping RequestTags migration " +
				"(tags will be validated on node reconnect)")

		return nil
	}

	// 2. Load users and nodes to create PolicyManager. The pre-auth key
	// table lacks columns added by later migrations, so nodes are loaded
	// without their keys, which the tag check does not need.
	users, err := listUsersBeforeRoles(tx)
	if err != nil {
		return fmt.Errorf("loading users for RequestTags migration: %w", err)
	}

	nodes, err := listNodesWithoutAuthKeys(tx)
	if err != nil {
		return fmt.Errorf("loading nodes for RequestTags migration: %w", err)
	}

	// 3. Create PolicyManager (handles HuJSON parsing, groups, nested tags, etc.)
	polMan, err := policy.NewPolicyManager(policyData, users, nodes.ViewSlice())
	if err != nil {
		log.Warn().
			Err(err).
			Msg("failed to parse policy, skipping RequestTags migration " +
				"(tags will be validated on node reconnect)")

		return nil
	}

	// 4. Process each node.
	for _, node := range nodes {
		if node.Hostinfo == nil {
			continue
		}

		requestTags := node.Hostinfo.RequestTags
		if len(requestTags) == 0 {
			continue
		}

		existingTags := node.Tags

		var validatedTags, rejectedTags []string

		nodeView := node.View()

		for _, tag := range requestTags {
			if polMan.NodeCanHaveTag(nodeView, tag) {
				if !slices.Contains(existingTags, tag) {
					validatedTags = append(validatedTags, tag)
				}
			} else {
				rejectedTags = append(rejectedTags, tag)
			}
		}

		if len(validatedTags) == 0 {
			if len(rejectedTags) > 0 {
				log.Debug().
					EmbedObject(node).
					Strs("rejected_tags", rejectedTags).
					Msg("RequestTags rejected during migration (not authorized)")
			}

			continue
		}

		mergedTags := append(slices.Clone(existingTags), validatedTags...)
		slices.Sort(mergedTags)
		mergedTags = slices.Compact(mergedTags)

		tagsJSON, err := json.Marshal(mergedTags)
		if err != nil {
			return fmt.Errorf("serializing merged tags for node %d: %w", node.ID, err)
		}

		_, err = tx.ex.execRaw("UPDATE nodes SET tags = $1 WHERE id = $2", string(tagsJSON), node.ID.Uint64())
		if err != nil {
			return fmt.Errorf("updating tags for node %d: %w", node.ID, err)
		}

		log.Info().
			EmbedObject(node).
			Strs("validated_tags", validatedTags).
			Strs("rejected_tags", rejectedTags).
			Strs("existing_tags", existingTags).
			Strs("merged_tags", mergedTags).
			Msg("Migrated validated RequestTags from host_info to tags column")
	}

	return nil
}

// createOAuthTables (202606211200) adds the oauth_clients and
// oauth_access_tokens tables.
func createOAuthTables(tx *Tx) error {
	hasClients, err := tx.ex.hasTable("oauth_clients")
	if err != nil {
		return err
	}

	if !hasClients {
		ddl := `CREATE TABLE oauth_clients(
  id integer PRIMARY KEY AUTOINCREMENT,
  client_id text,
  secret_hash blob,
  scopes text,
  tags text,
  description text,
  user_id integer,
  created_at datetime,
  revoked datetime
)`
		if tx.ex.dialect == dialectPostgres {
			ddl = `CREATE TABLE oauth_clients(
  id bigserial PRIMARY KEY,
  client_id text,
  secret_hash bytea,
  scopes text,
  tags text,
  description text,
  user_id bigint,
  created_at timestamptz,
  revoked timestamptz
)`
		}

		err = tx.ex.execAll("creating oauth_clients table", []string{
			ddl,
			`CREATE UNIQUE INDEX idx_oauth_clients_client_id ON oauth_clients(client_id)`,
		})
		if err != nil {
			return err
		}
	}

	hasTokens, err := tx.ex.hasTable("oauth_access_tokens")
	if err != nil {
		return err
	}

	if hasTokens {
		return nil
	}

	ddl := `CREATE TABLE oauth_access_tokens(
  id integer PRIMARY KEY AUTOINCREMENT,
  prefix text,
  hash blob,
  client_id text,
  scopes text,
  tags text,
  expiration datetime,
  created_at datetime
)`
	if tx.ex.dialect == dialectPostgres {
		ddl = `CREATE TABLE oauth_access_tokens(
  id bigserial PRIMARY KEY,
  prefix text,
  hash bytea,
  client_id text,
  scopes text,
  tags text,
  expiration timestamptz,
  created_at timestamptz
)`
	}

	return tx.ex.execAll("creating oauth_access_tokens table", []string{
		ddl,
		`CREATE UNIQUE INDEX idx_oauth_access_tokens_prefix ON oauth_access_tokens(prefix)`,
	})
}
