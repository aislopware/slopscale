# Log streaming

Log streaming ships the [audit log](audit.md) to a SIEM or log store as it
is written, the way Tailscale's log streaming ships the configuration log.
Every event the server records is queued for each stream, batched, encoded
in the shape the destination expects and posted. The audit log itself is
never held back for a sink: a sink that stays down loses the batch after a
few retries, and the loss is counted on the stream.

## Setting one up

Create the stream from the _Log streams_ tab of the console's
_Integrations_ page, with the CLI, or through `/api/v1/log-stream`:

```console
$ headscale log-streams create --name siem --destination splunk \
    --url https://splunk.example.com:8088/services/collector/event \
    --token 11111111-2222-3333-4444-555555555555
```

The token is stored and never listed again; an update that leaves it empty
keeps the stored one. `headscale log-streams test` ships one synthetic entry
(`logstream.test`) and reports the sink's answer, and the list shows the
newest batch's status with how many entries the sink has accepted and how
many were dropped over the stream's life. A stream can be disabled to keep
its settings without shipping.

Managing streams needs the `logs:configuration` scope, which the owner, the
admins, the network admin and the IT admin hold; reading them needs
`logs:configuration:read`, which an auditor also holds.

## Entries

A sink receives one object per audit event:

```json
{
  "time": "2026-09-12T09:00:00Z",
  "type": "configuration",
  "tailnet": "example.ts.net",
  "id": 1204,
  "action": "user.role.set",
  "actor": { "kind": "api_key", "id": "3", "name": "alice" },
  "target": { "kind": "user", "id": "5", "name": "bob" },
  "outcome": 200,
  "detail": { "role": "admin" },
  "remoteAddr": "203.0.113.9"
}
```

`id` is the event's ID in the audit log, so a finding in the SIEM can be
traced back with `headscale audit` or the console. `type` is always
`configuration`: the server has no network flow logs to stream, since it
never sees the data plane.

## Destinations

| Destination | URL                                           | Credential                                   | Body                                                                                                        |
| ----------- | --------------------------------------------- | -------------------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| `http`      | any collector that takes JSON                 | optional, sent as `Authorization: Bearer`    | a JSON array of entries                                                                                     |
| `splunk`    | the HTTP Event Collector endpoint             | the HEC token, as `Authorization: Splunk`    | one HEC event per entry, `sourcetype` `headscale:configuration`, `host` the tailnet                         |
| `elastic`   | the index's `_bulk` URL                       | optional API key, as `Authorization: ApiKey` | a bulk request; each document is the entry with `@timestamp`                                                |
| `datadog`   | the logs intake of your site (`/api/v2/logs`) | the API key, as `DD-API-KEY`                 | the entry's fields with `ddsource`, `service`, `ddtags` (`type`, `tailnet`, `stream`) and a `message` line  |
| `axiom`     | the dataset's `/ingest` URL                   | the API token, as `Authorization: Bearer`    | a JSON array of entries with `_time`                                                                        |
| `loki`      | the push API (`/loki/api/v1/push`)            | optional, sent as `Authorization: Bearer`    | one stream labelled `job=headscale`, `type`, `stream` and `tailnet`; each value is the entry as a JSON line |

The `http` destination fits Cribl, Panther, a Vector or Fluent Bit HTTP
source, or your own receiver. Splunk, Datadog and Axiom refuse a stream
without a token; the others take one when the sink is behind
authentication.

## Delivery

A worker per stream takes entries off a queue of 4096, waits up to two
seconds for more, and posts up to 100 at a time. A network error or a 5xx or
429 answer is retried after 2, 10 and 30 seconds; a 4xx is the sink's verdict
and is not retried. Redirects are not followed. A batch that is given up on
is counted as dropped, as is an entry that found the queue full, and the
last batch's status stays on the stream. Nothing about a batch is kept
beyond that: the audit log is the record, the stream a copy.

On shutdown the workers get five seconds to ship what they hold.
