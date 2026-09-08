# Audit log

Headscale records who changed what. Every writing request to the API, from
the CLI, the [admin console](console.md) or any other client, becomes one
audit event once it has run, and so do the sign-in events the server
performs itself. The log is append-only; nothing in the API edits or deletes
it.

## What an event holds

| Field                                  | Meaning                                                                                                                                                                                                                                                                                                                                          |
| -------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `createdAt`                            | When the request finished.                                                                                                                                                                                                                                                                                                                       |
| `actorKind`                            | How the caller authenticated: `local` (the unix socket, so the CLI on the server), `api_key`, `oauth`, `session` (a console sign-in), `node` (a machine reporting through the control protocol: `node.client.disconnect` when a machine under an always-on device policy records why its user disconnected) or `system` (the server on its own). |
| `actorUserId`, `actorName`             | The user behind the credential, by ID and by the name it had then. A key without a user records its prefix as the name.                                                                                                                                                                                                                          |
| `action`                               | What happened, dotted and object first: `user.role.set`, `node.delete`, `preauthkey.create`, `policy.set`, `console.login`. A `system` `user.role.set` with `source: oidc.admin_users` is a promotion by configuration.                                                                                                                          |
| `targetKind`, `targetId`, `targetName` | The object acted on, copied by value so the entry stays readable after the object is gone.                                                                                                                                                                                                                                                       |
| `outcome`                              | The HTTP status the request ended with. A refused request (403, 404, 409) is recorded too; an unauthenticated one (401) is not, since it has no actor.                                                                                                                                                                                           |
| `detail`                               | Action-specific fields: the new role, the routes approved, the tags set, a key's expiry. Never a secret: keys appear as their prefix, the policy as its size.                                                                                                                                                                                    |
| `remoteAddr`                           | The caller's IP address.                                                                                                                                                                                                                                                                                                                         |

Actions are listed in the OpenAPI document: each writing operation carries
`x-audit-action`.

## Reading the log

From the CLI:

```console
$ headscale audit list
$ headscale audit list --action node. --limit 20
$ headscale audit list --user 3 --since 2026-09-01T00:00:00Z
$ headscale audit list --since 24h --target-kind node
```

`--since` takes an RFC 3339 time or a duration back from now. The next page
is `--before <id of the last event shown>`.

Or from the API, newest first:

```console
$ curl -H "Authorization: Bearer $KEY" \
    "https://headscale.example.com/api/v1/audit?action=user.role.set&limit=50"
```

`action` keeps one action, or every action under a prefix when it ends with a
dot (`node.` keeps every node action). `actorUserId`, `targetKind` and
`targetId` keep one actor or one object; `since` and `until` bound the time.
Pages follow the `nextBefore` cursor: pass it as `before` to get the next
page.

Reading the log needs the `logs:configuration:read` scope, which every
[role](roles.md) except member holds. The console shows it under *Audit
log*. To ship it to a SIEM as it is written, see [Log
streaming](log-streaming.md).

## Exporting

`GET /api/v1/audit/export` returns the audit log as a file instead of a JSON
page. It takes the same filters as the list, `actorUserId`, `action`,
`targetKind`, `targetId`, `since`, `until` and `before`, plus `format`, which is
`csv` (the default) or `json`. Events come oldest first, so a spreadsheet reads
top to bottom in the order things happened. The console's _Audit log_ page has
an _Export_ button for the window and filters on screen, and the CLI writes the
same file:

```console
headscale audit export --since 2026-09-01T00:00:00Z --until 2026-10-01T00:00:00Z -o audit-september.csv
```

The CLI takes the same filters as `headscale audit list`, `--user`, `--action`,
`--target-kind`, `--target-id`, `--since` and `--before`, and adds `--until` and
`--format`. `--since` and `--until` accept an RFC 3339 time or a duration back
from now, such as `24h`. Without `--output` the file is written to stdout, so it
can be piped.

```console
curl -H "Authorization: Bearer $HEADSCALE_API_KEY" \
  "https://headscale.example.com/api/v1/audit/export?since=2026-09-01T00:00:00Z&until=2026-10-01T00:00:00Z" \
  -o audit-september.csv
```

The response is an attachment, `text/csv; charset=utf-8` or `application/json`,
named after the window it covers (`audit-20260901T000000Z-20261001T000000Z.csv`).
Without `since` the name starts at `start`; without `until` it ends at the time
of the request.

The CSV has a header row and one row per event, with the columns `id`, `time`,
`action`, `actorKind`, `actorUserId`, `actorName`, `targetKind`, `targetId`,
`targetName`, `outcome`, `remoteAddr` and `detail`. `time` is RFC 3339 in UTC,
`outcome` is the HTTP status the request ended with, and `detail` is the
action's own fields as JSON in a single cell. The JSON format is one array of
the same events in the shape `GET /api/v1/audit` returns them.

The server reads the log in batches while it writes the response, so the export
never sits in memory, and it stops at 100000 events. When a filter matches more
than that, the file holds the oldest 100000 and the newest are left out; export
one window at a time with `since` and `until` to get all of them.

Exporting needs the `logs:configuration:read` scope, the same as reading the
list, and is a read: it records no audit event of its own.

## Retention

Events are kept forever by default. To bound the table, set a retention in
the configuration; an hourly collector deletes older events:

```yaml
audit:
  retention: 2160h # 90 days
```
