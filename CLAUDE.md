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

Needs go, golangci-lint, mdformat, and prettier on PATH; `nix develop` pins
the CI versions but isn't required. `prek install` once; `--no-verify` is for
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
- `hscontrol/servertest/` is an in-memory server harness. Prefer it over
  `integration/` when Docker isn't needed. It runs the NodeStore with a 5ms
  write batch, so one change reaches clients as several map responses; check
  netmaps with `WaitForCondition` or the `Assert*` helpers rather than reading
  `Netmap()` right after a change. Its OAuth cases shell out to `tofu`, which
  only the nix shell provides. `make test` passes `-short`, which skips
  `TestHAProberProperty`; that one runs 100 real handshake rounds and takes
  over half an hour, so run it explicitly when touching HA election.

## Invariants

Migrations run in place on users' production databases, so their rules are
strict. Order is immutable and new migrations go at the end. IDs are
`YYYYMMDDHHMM-short-description`. `migrationsRequiringFKDisabled` in
`hscontrol/db/db.go` has been frozen since 2025-07-02. Never rename a column a
later migration references; let AutoMigrate add a new one.

Tags XOR users. A node is tagged or user-owned, never both. `node.IsTagged()`
is authoritative; a tagged node may still carry a `UserID` as "created by", so
`UserID().Valid()` alone says nothing about ownership. `validateNodeOwnership`
in `hscontrol/state/tags.go` enforces this.

API responses read through `NodeView`, `UserView`, and `PreAuthKeyView`.
`AsStruct()` clones the whole record and is only for write/merge copies.

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
