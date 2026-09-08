# CLAUDE.md

Headscale is an open-source Tailscale control server in Go. This file holds
what the code cannot tell you. Procedures live next to the code:
`cmd/hi/README.md` for running integration tests, `integration/README.md` for
writing them, `hscontrol/api/v2/README.md` for the v2 API.

## Working here

When you need a decision from the user, offer the likely branches as a
multiple-choice list plus "other". It saves a round trip.

Read the README before running `hi`. Its flags are not guessable and a wrong
run leaves stale containers behind.

Keep changes to what the task needs. A bug fix doesn't need surrounding
cleanup, and three similar lines beat a premature helper.

## Commands

```bash
make dev                     # fmt + lint + test + build
make build / test / fmt / lint
make generate                # regenerate gen/; never edit it by hand
go run ./cmd/hi doctor
go run ./cmd/hi run "TestName" [--postgres]
```

Needs go, golangci-lint, mdformat and bun on PATH; `nix develop` pins the
CI versions but isn't required. Markup and config files outside `docs/`
are formatted by the console's oxfmt (`make fmt-markup`, root
`.oxfmtrc.json`); `docs/` stays on mdformat because python-markdown needs
four-space list indents. `prek install` once; `--no-verify` is for
WIP commits on feature branches only.

## Orientation

- Cross-subsystem operations go through `State` in `hscontrol/state/`, not the
  DB directly. `State.UpdateNodeFromMapRequest` is where client Hostinfo,
  endpoint, and route changes land.
- `NodeStore` is copy-on-write: reads are a pointer load, writes rebuild the
  whole snapshot. It sits on the MapRequest hot path, so measure before
  changing it. Same for `hscontrol/mapper/`.
- `hscontrol/policy/v2/` is the policy engine; `policy/policy.go` is thin
  wrappers. There is no v1.
- `hscontrol/db/` has no ORM. Queries are built once with go-jet's SQLite
  builder against `gen/jet/table` (generated from `schema.sql` by
  `cmd/gen-jet`, never edited by hand) and run through `executor`, which
  rewrites placeholders and quoting for PostgreSQL, so there is one source
  per query and no dialect branches in query code. Raw SQL is for
  migrations and schema introspection only, with `$n` placeholders. Row
  types live in `model.go`; jet maps results by table alias, so a
  destination struct needs an `alias:"table"` tag when its type name is not
  the table name. `SaveNode`/`SaveUser` keep GORM's update-or-insert
  meaning; `UpdateNode` takes a `NodeUpdate` selecting expiry and auth key,
  because the map request path must never write a stale `auth_key_id`.
  Statements on the map request and registration paths are `fixedSQL`:
  rendered once, arguments bound per call; `fixed_test.go` pins each one
  to the jet statement it replaces, so add a case there for every new one.
- `schema.sql` is the schema's source of truth: squibble validates every
  SQLite database against it, and `TestPostgresSchemaMatchesGolden` pins
  `schema_postgres.sql` to the schema GORM used to create, so a change to
  one must be mirrored in the other and in the golden file. SQLite is
  `mattn/go-sqlite3`, C compiled by cgo, imported only by
  `hscontrol/db/sqliteconfig`, which registers it as `sqlite` and applies
  the pragmas per connection through a `driver.Connector` (depguard
  rejects the driver elsewhere and every other SQLite driver anywhere).
  Hardening (`SQLITE_DQS=0`, defensive mode) is compile-time: the flags
  live in `sqlite.cflags` and reach the compiler as `CGO_CFLAGS` through
  the Makefile, the flake, the Dockerfiles and the goreleaser env; a
  binary built without them logs a warning at startup and the hardening
  test skips. Cross builds need a C compiler per target: `zigcc` in the
  devShell maps `GOOS`/`GOARCH` to a zig target and links statically
  against musl.
- `hscontrol/servertest/` is an in-memory server harness. Prefer it over
  `integration/` when Docker isn't needed. It runs the NodeStore with a 5ms
  write batch, so one change reaches clients as several map responses; check
  netmaps with `WaitForCondition` or the `Assert*` helpers rather than reading
  `Netmap()` right after a change. Its OAuth cases shell out to `tofu`, which
  only the nix shell provides. `make test` passes `-short`, which skips
  `TestHAProberProperty`; that one runs 100 real handshake rounds and takes
  over half an hour, so run it explicitly when touching HA election.

The admin console in `web/` is a separate toolchain (bun, TypeScript 7,
oxlint, oxfmt, Vite; see `web/README.md`) with its own strict gate
(`make lint-web`, part of `make lint`). Its UI is Cloudflare Kumo: use Kumo
components and semantic tokens, never hand-rolled primitives or raw
colours, and follow the Kumo design skill in `.claude/skills/kumo-design`. It talks only to `/api/v1` with the
operator's API key, so it holds no privilege of its own; `web/embed.go`
embeds `web/dist` and serves it at `/admin/`, or a "not built" page when
`make web` did not run. `web/dist/.gitkeep` keeps the embed pattern valid
on a fresh checkout. Its API types come from `gen/openapi/v1.yaml` via
`make web-generate`; regenerate and commit both after changing the v1 API.

## Invariants

Migrations run in place on users' production databases, so their rules are
strict. Order is immutable and new migrations go at the end. IDs are
`YYYYMMDDHHMM-short-description`, written by hand in
`hscontrol/db/migrations.go` and run by `migrate.go`, one transaction each.
On SQLite everything up to `lastMigrationRequiringFKDisabled` runs with
foreign keys off; that boundary has been frozen since 2025-07-02. A fresh
database gets `schema.sql` directly and every ID marked applied; an existing
database without a `migrations` table is refused rather than guessed at.
Never rename a column a later migration references; add a new one.

Tags XOR users. A node is tagged or user-owned, never both. `node.IsTagged()`
is authoritative; a tagged node may still carry a `UserID` as "created by", so
`UserID().Valid()` alone says nothing about ownership. `validateNodeOwnership`
in `hscontrol/state/tags.go` enforces this.

Roles bound credentials, not devices. `hscontrol/api/principal` turns a
credential into a `Principal` for both API versions: the socket and a key
without a user are all-access, a user-owned key gets `scope.ForRole` of the
user's _current_ role, an OAuth token its own scopes. Both middlewares check
the scope each operation declares (`principal.RequireScope`); a guard test in
each API package fails on an authenticated operation without one. Role rules
(one owner, transfer only, nobody edits their own role) live in
`State.SetUserRole`; the policy manager, not `Node.User`, is the source for
role autogroups and the `is-admin`/`is-owner` caps because the node's user
copy is loaded once and goes stale. A role change is a `PolicyChange` with
`IncludeSelf`, because those caps live on the self node, which a broadcast
policy change never carries.

Approval is a node property (`nodes.approved_at`; users have their own) and
is enforced in exactly two places: the NodeStore's peer function drops
unapproved nodes before the policy builds the peer map, and
`State.ListPeers`'s explicit-ID branch, `FilterForNode` and `SSHPolicy`
apply the same rule for the incremental paths. Nothing in the mapper or the
policy engine knows about approval. `persistNodeToDB` never writes
`approved_at` (like expiry); `NodeSetApproval` does. An approval change is a
`PolicyChange` with `IncludeSelf`, not `OriginNode`: one change can carry a
single origin, but switching a setting off admits many nodes at once and
each client needs its self node to see `MachineAuthorized` flip. The
tailnet-wide switches live in the `settings` key/value table and are cached
on `State`; a fresh or upgraded server has them off, and the migration
backfills everything that exists as approved. Test helpers (`CreateUser`,
`CreateNodeForTest`, the servertest keys) create approved rows, so a test
that wants a pending node must ask for it (`PreAuthKeySpec` with
`Preauthorized: false`, `CreateUserFromLogin`). A servertest client is torn
down with the `testing.TB` it was created for, so create clients on the
parent test, not inside a subtest that later subtests depend on.

Sharing lives on the node: `nodes.SharedWith` (from `node_shares`, attached
by every production node read in `hscontrol/db/node.go`) is written only by
`State.ShareNode`/`UnshareNode` and takes effect only through the policy.
`autogroup:shared` is a source that resolves per destination node
(`grantCategoryShared` in `policy/v2/compiled.go`, `resolveSSHSources` for
SSH), narrowed to that node, so shares never open other nodes; the mapper
stamps `Sharer` on the peer view for sharees. `HasPolicyChange` compares
`SharedWith`, so `SetNodes` recompiles after a share, and every share is a
`PolicyChange` because the sharer marker changes even when the policy does
not. Deleting a user cascades the rows in the database and
`dropSharesWithUser` mirrors that in the NodeStore.

Exit node suggestions follow the hosted control plane's wire shape (the
issue_3212 captures): `PeerCapMap` in `policy/v2/tailnet_state_caps.go` puts
`suggest-exit-node` on the peer view of every approved exit node, never on a
self view, and the mapper passes it whether a global exit node exists. The
global exit node is `nodes.global_exit_node`, written only by
`State.SetGlobalExitNode` (which also approves the exit routes) and read by
`stampGlobalExitNodes` in `policy/v2/compiled.go`: `suggest-exit-node` on the
marked nodes' self caps, which `PeerCapMap` then requires while any node is
marked, so a mark narrows the suggestion; `auto-exit-node` on every node
while one exists, with or without a policy. The node-attrs fast path in
`refreshNodeAttrsLocked` must stay open while a global exit node exists, and
`HasPolicyChange` compares the flag so `SetNodes` recompiles. The control
server cannot make a client use an exit node; the caps only drive the
client's own suggestion and auto pick.

API responses read through `NodeView`, `UserView`, and `PreAuthKeyView`.
`AsStruct()` clones the whole record and is only for write/merge copies.

## Quality gate

All code here is machine-written, so the gate is deliberately strict.
`.golangci.yaml` enables every linter and formatter (golines wraps at 120,
gofumpt, gci); the few disabled ones carry a reason next to them. Complexity
limits (gocognit, cyclop, funlen, maintidx, nestif) apply to production
code, not tests. `make lint` is the full gate: golangci-lint, `go vet` with
the integration tag, `go mod tidy -diff`, and `go tool govulncheck`.
`make fmt-go` runs `go fix` first, so new code lands in current idioms.
Suppress a linter only with `//nolint:<name> // <why>` on the line above
the statement; nolintlint rejects bare or unused directives. Large legacy
functions carry a `// legacy:` suppression; split them when you next touch
them rather than adding to them.

## Conventions

- Commits follow Conventional Commits with the Go package as scope:
  `fix(db): scope DestroyUser to the target user's pre-auth keys`,
  `feat(policy/v2): ...`, `docs: ...`. Imperative mood, lowercase, no period.
- Regenerated `gen/` code goes in its own commit, before the code that uses
  it. CI checks that `gen/` matches its sources.
- User-facing changes get a CHANGELOG.md entry under the unreleased version,
  written for operators and linked to the PR.
- zerolog: with four or more fields, or conditional ones, build incrementally
  and reassign, `e = e.Str(k, v)`. Forgetting the reassignment silently drops
  the field.
- Integration tests start with `IntegrationSkip(t)`. Reads such as
  `client.Status` go inside `EventuallyWithT`; mutations such as
  `tailscale set` do not. Flakes are almost always code; read
  `hs-*.stderr.log` before blaming Docker.
- SQLite locally, PostgreSQL with `--postgres`. Some races show up on only one.
