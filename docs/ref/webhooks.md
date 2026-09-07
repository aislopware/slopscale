# Webhooks

A webhook is a URL Headscale posts to when something happens in the tailnet:
a machine joins or leaves, a user is created or changes role, the policy
changes. Each endpoint has a secret and picks the events it wants. The
delivery format is Tailscale's, so a receiver written for Tailscale's
webhooks, and the `tailscale.com/client/tailscale/v2` webhook endpoints,
work unchanged.

## Setting one up

Create the endpoint from the console's _Webhooks_ page, with the CLI, or
through `/api/v1/webhook`:

```console
$ headscale webhooks create --url https://ops.example.com/headscale \
    --event nodeCreated --event nodeDeleted --event userCreated
```

The response carries the secret once. Store it on the receiver; a later read
never shows it again. `headscale webhooks rotate` issues a new one, and
`headscale webhooks test` sends a `test` event so you can check the receiver
before anything real happens.

The server keeps the last 100 deliveries per endpoint, each with the event
type, the final HTTP status or error, how many attempts it took and how long.
The list shows the newest one; _Deliveries_ in the console's row menu,
`headscale webhooks deliveries`, or `GET /api/v1/webhook/{id}/deliveries`
show the rest, newest first.

## Events

| Type                | When                                                                               |
| ------------------- | ---------------------------------------------------------------------------------- |
| `nodeCreated`       | A machine registered.                                                              |
| `nodeNeedsApproval` | A machine registered while device approval is on.                                  |
| `nodeApproved`      | A machine was approved.                                                            |
| `nodeKeyExpired`    | A machine's key expired.                                                           |
| `nodeDeleted`       | A machine was removed.                                                             |
| `nodeSuspended`     | An administrator suspended a machine; see [Device trust](device-trust.md).         |
| `nodeUnsuspended`   | A machine's suspension was lifted.                                                 |
| `policyUpdate`      | The policy file was saved or reloaded, or a group, access rule or network changed. |
| `userCreated`       | A user was created, by an operator or by a first login.                            |
| `userNeedsApproval` | A user was created while users approval is on.                                     |
| `userApproved`      | A user was approved.                                                               |
| `userRoleUpdated`   | A user's role changed.                                                             |
| `userDeleted`       | A user was deleted.                                                                |

`GET /api/v1/webhook/event-types` and `headscale webhooks event-types` list
them. The `test` event goes to every endpoint on request and needs no
subscription.

## Delivery format

Every delivery is a JSON array of events, so a receiver must expect more than
one. Each event has this shape:

```json
[
  {
    "timestamp": "2026-09-07T09:12:44Z",
    "version": 1,
    "type": "nodeCreated",
    "tailnet": "example.com",
    "message": "Node laptop (alice) joined the tailnet.",
    "data": {
      "nodeID": "12",
      "deviceName": "laptop.example.com",
      "managedBy": "alice@example.com",
      "url": "https://headscale.example.com/admin/machines/12",
      "expiration": "2027-03-06T09:12:44Z",
      "addresses": ["100.64.0.12", "fd7a:115c:a1e0::c"]
    }
  }
]
```

`tailnet` is the MagicDNS base domain, or the server's host when there is
none. The `data` fields are named as Tailscale names them, so a receiver
written for Tailscale reads them as is. Node events carry `nodeID`,
`deviceName` (the MagicDNS name), `managedBy` (the owner's email or login,
or `tagged-devices`), `url` (the console page) and, when the key expires,
`expiration`; `addresses` and `tags` are extra. User events carry `user`,
`url` and `userID`, plus `displayName` when set; `userRoleUpdated` adds
`oldRoles`, `newRoles` and `actor`. `policyUpdate` has no data.

A failed delivery is retried three times, after 2, 10 and 30 seconds, when
the receiver answered with a 5xx or 429 or did not answer at all. A 4xx is
taken as the receiver's verdict and not retried, and so is a redirect: the
payload goes to the configured URL only. At most 16 deliveries are in flight
at once and each waits up to 15 seconds for a response.

### Verifying the signature

Each request carries a `Tailscale-Webhook-Signature` header:

```
Tailscale-Webhook-Signature: t=1757236364,v1=5257a869e7ecebeda32affa62cdca3fa51cad7e77a0e56ff536d0ce8e108d8bd
```

`t` is the Unix time the request was signed and `v1` is the hex HMAC-SHA256
of `<t>.<body>` under the endpoint's secret. To verify, rebuild the HMAC from
the raw request body and compare it in constant time, and reject a `t` more
than a few minutes away from now to stop replays. In Go,
`webhook.Verify(secret, header, body, time.Now(), 5*time.Minute)` from
`github.com/juanfont/headscale/hscontrol/webhook` does both. In Python:

```python
import hmac, hashlib, time

def verify(secret: str, header: str, body: bytes, tolerance: int = 300) -> bool:
    parts = dict(p.split("=", 1) for p in header.split(","))
    t, v1 = parts["t"], parts["v1"]
    if abs(time.time() - int(t)) > tolerance:
        return False
    digest = hmac.new(secret.encode(), f"{t}.".encode() + body, hashlib.sha256).hexdigest()
    return hmac.compare_digest(digest, v1)
```

## Chat providers

An endpoint with a provider type gets the message alone, in the shape the
service's incoming webhook expects, instead of the signed array:

| Provider     | Body                 |
| ------------ | -------------------- |
| `slack`      | `{"text": "..."}`    |
| `mattermost` | `{"text": "..."}`    |
| `googlechat` | `{"text": "..."}`    |
| `discord`    | `{"content": "..."}` |

Paste the incoming webhook URL the service gave you and pick the provider; the
signature header is still sent but those services ignore it.

## Tailscale API

The v2 endpoints mirror Tailscale's:
`GET/POST /api/v2/tailnet/-/webhooks`, `GET/PATCH/DELETE`, `/test` and
`/rotate` on `/api/v2/webhooks/{endpointId}`. They require the
`webhooks` scope (`webhooks:read` to list and read), which every admin role
holds; an auditor reads.
