# Device and user approval

By default any node that presents a valid pre-auth key or completes a login
joins the tailnet at once, and any user that logs in through OpenID Connect is
created ready to register nodes. Two tailnet-wide switches put an administrator
between those steps, following
[Tailscale's device approval](https://tailscale.com/docs/features/device-approval)
and user approval:

| Setting             | Effect when on                                                                                                                                          |
| ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `devicesApprovalOn` | A newly registered node waits for an administrator unless it registered with a _preauthorized_ pre-auth key.                                            |
| `usersApprovalOn`   | A user created by an OpenID Connect login waits for an administrator before any node of theirs can register. Users an administrator creates never wait. |

Both are off on a fresh server and after an upgrade; everything that existed
before the switches did counts as approved. A third switch,
`postureIdentityOn`, belongs to [device trust](device-trust.md) and lets
the server collect serial numbers from clients.

## Settings

```console
slopscale settings get
slopscale settings set --devices-approval=true
slopscale settings set --users-approval=true
```

Through the API, `GET` and `POST /api/v1/settings` with
`{"devicesApprovalOn": true}`; the v2 API reads and patches the same values on
`/api/v2/tailnet/-/settings`. Both need the `feature_settings` scope, which
the owner and admins hold.

Switching a setting off approves every node or user that was waiting, so
nothing stays stuck behind a requirement that no longer exists.

An [invited user](console.md#inviting-users) is created approved whatever
`usersApprovalOn` says: the administrator who sent the invitation already
vouched for the address, so making them wait again would ask the same question
twice.

### Key expiry

The same endpoint carries the tailnet's key expiry, following
[Tailscale's key expiry](https://tailscale.com/docs/features/key-expiry):
`keyExpiryDays` caps how long a login stays valid. A client that asks for
longer is shortened to it, and a login that asks for nothing gets it. It
applies to the next login of each node, never to tagged nodes, and does not
touch a node whose expiry an administrator disabled. Zero, the default,
leaves the config file's `node.expiry` and the client in charge; the
response reports that file value as `defaultKeyExpiryDays`.

```console
slopscale settings set --key-expiry-days 90
```

The v2 API exposes it as `devicesKeyDurationDays`. The console's _Settings_
page has it under _Key expiry_, next to a read-only _Server_ card showing the
build, addresses, DERP regions and config file values from
`GET /api/v1/server`.

## Devices

A node waiting for approval is registered and keeps its address, but it gets
no peers, no peer sees it and its packet filter is empty. The Tailscale client
shows it as needing machine authorization (`tailscale status` reports
"Machine is not yet approved") and picks up the approval live, without logging
in again.

```console
slopscale nodes list           # the Approved column reads "pending"
slopscale nodes approve --identifier 7
slopscale nodes approve --identifier 7 --revoke
```

Through the API, `POST /api/v1/node/{id}/approve`, with
`{"approved": false}` to withdraw the approval; the node object carries
`approved` and `approvedAt`. The v2 API's `POST /api/v2/device/{id}/authorized`
does the same, with `authorized: false` withdrawing, and the device object's
`authorized` field reflects it.

A pre-auth key is preauthorized unless created otherwise, so keys minted for
automation keep working when device approval is switched on. A key that should
not bypass approval is created with `--preauthorized=false`
(`"preauthorized": false` on `POST /api/v1/preauthkey`, or the
`preauthorized` capability of the v2 keys API).

```console
slopscale preauthkeys create --user 1 --preauthorized=false
```

## Users

A user waiting for approval shows as pending in `slopscale users list`, with
`approved: false` in the v1 user object and `"status": "needs-approval"` in
the v2 one. A login attempt by such a user is refused with a message that the
account awaits approval; the user exists, so an administrator can find and
approve them.

```console
slopscale users approve --name alice
slopscale users approve --name alice --revoke
```

Through the API, `POST /api/v1/user/{id}/approve`, with `{"approved": false}`
to withdraw; the v2 API's `POST /api/v2/users/{id}/approve`, `/suspend` and
`/restore` map onto the same state. Withdrawing a user's approval also
withdraws every node the user owns; approving the user again brings the nodes
back unless device approval is on, in which case each node waits its own turn.
Tagged nodes belong to their tags and are never affected by a user's approval.
