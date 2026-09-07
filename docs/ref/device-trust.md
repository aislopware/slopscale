# Device trust

Approval decides whether a machine may join at all (see
[Device and user approval](approval.md)). Device trust is what an
administrator can do with a machine after that: cut it off for a while
without deleting it, and let the policy look at what the machine is before
letting its traffic through.

## Suspending a machine

A suspended machine stays registered and keeps its addresses and its key,
but it gets no peers, no peer sees it, its packet filter and SSH policy are
empty, and its client is told it is not authorized. The Tailscale client
shows the same state as a machine waiting for approval, and a health
message, "This device is suspended", says why. Lifting the suspension gives
everything back live; nobody has to sign in on the device.

Suspension is the reversible alternative to expiring the key or removing the
machine: a lost laptop, a contractor on leave, a server under
investigation. It is separate from approval, so a suspended machine is
still approved and a suspended machine that is later unsuspended does not
wait for approval again.

```console
headscale nodes list                        # the Approved column reads "suspended"
headscale nodes suspend --identifier 7
headscale nodes suspend --identifier 7 --revoke
```

Through the API, `POST /api/v1/node/{id}/suspend`, with
`{"suspended": false}` to lift it; the node object carries `suspended` and
`suspendedAt`. It needs the `devices:core` scope. The console has _Suspend_
in a machine's menu and in the danger zone of its page, where a suspended
machine shows a red _Suspended_ badge. Both edges are audited as
`node.suspension.set` and raise the `nodeSuspended` and `nodeUnsuspended`
webhook events.

The v2 API has no counterpart, because Tailscale's API has none; a
suspended machine reads as `authorized: true` there.

## Device posture

A posture is what the policy can check about a machine before it lets
traffic through, following
[Tailscale's device posture](https://tailscale.com/docs/features/device-posture).
Each machine carries a set of attributes: most are derived from what its
client reports, the serial numbers are collected from the client on request,
and `custom:` attributes are set by an operator.

| Attribute                           | Source                                                                                          |
| ----------------------------------- | ----------------------------------------------------------------------------------------------- |
| `node:os`                           | Hostinfo, lowercased the way Tailscale writes it: `linux`, `macos`, `windows`, `ios`, `android` |
| `node:osVersion`                    | Hostinfo                                                                                        |
| `node:tsVersion`                    | Hostinfo, the client version without the build suffix                                           |
| `node:tsReleaseTrack`               | `stable` or `unstable`, from the client version                                                 |
| `node:tsAutoUpdate`                 | Whether the client has auto-update on                                                           |
| `node:hostname`                     | The name the client reports                                                                     |
| `node:machine`                      | The CPU architecture                                                                            |
| `node:distro`, `node:distroVersion` | Linux distribution and release                                                                  |
| `node:deviceModel`                  | The hardware model on macOS, iOS and Android                                                    |
| `node:package`                      | How the client was installed                                                                    |
| `node:tagged`                       | Whether the node is tagged                                                                      |
| `node:serialNumber`                 | The serial numbers the client collected, once identity collection is on                         |
| `custom:...`                        | Set through the API, the CLI or the console                                                     |

`GET /api/v1/node/{id}/posture` returns the whole map, the identity report
and the custom attributes; the v2 API has Tailscale's
`GET /api/v2/device/{id}/attributes`. Both need `devices:posture_attributes:read`.
The console shows the map in a _Device posture_ section of the machine's
page.

### Identity collection

The client only hands over serial numbers when asked, and only when its
user allowed it with `tailscale set --posture-checking=true`. The server
asks over the control connection (a "c2n" request carried by the map
stream, answered over the noise channel), so the machine has to be
connected. Collection is off by default; the `postureIdentityOn` setting,
`headscale settings set --posture-identity=true` or _Collect device
identity_ under the console's _Settings_ turns it on. While it is on, the
server asks each machine when it connects and again once a day, and
`POST /api/v1/node/{id}/posture/collect`, `headscale nodes posture collect`
or the _Refresh_ button asks now. A client with posture checking off
answers that it is disabled, which the posture shows so the operator knows
to ask the user.

### Custom attributes

A custom attribute has a key of the form `custom:name`, a value that is a
string, a number or a boolean, an optional comment and an optional expiry.
An expired attribute stops counting at once and is deleted by a sweep; the
expiry is how a temporary marker such as an on-call rotation is made.
Setting one recomputes the policy for the machine.

```console
headscale nodes posture show --identifier 7
headscale nodes posture set --identifier 7 custom:oncall=true --expiry 8h --comment "pager week"
headscale nodes posture set --identifier 7 custom:tier=3
headscale nodes posture delete --identifier 7 custom:oncall
```

Through the API, `PUT /api/v1/node/{id}/attributes/{key}` with
`{"value": true, "expiry": "2026-09-08T09:00:00Z", "comment": "..."}` and
`DELETE` on the same path; the v2 API has Tailscale's
`POST` and `DELETE /api/v2/device/{id}/attributes/{key}`. Both need
`devices:posture_attributes`, which the owner, admins, network admins and IT
admins hold. The audit log records `node.attribute.set`,
`node.attribute.delete` and `node.posture.collect`.
