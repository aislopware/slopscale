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
| `ip:address`                        | The address the machine's control connection comes from, as seen by the server                  |
| `ip:country`                        | The ISO country code of that address; needs `policy.geoip_database`                             |

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

## Postures

A posture names conditions a machine must meet. It has a list of
expressions, all of which must hold, and optionally a weekly schedule
outside of which it does not hold at all. A posture is attached to access
rules, where it narrows the rule's sources to the machines that satisfy
it, and it can be written into the policy file the way Tailscale's
`postures` and `srcPosture` work. Postures are evaluated on the server
from the attributes above, so a client cannot claim one.

### Expressions

An expression is an attribute, an operator and a value, in
[Tailscale's syntax](https://tailscale.com/docs/features/device-posture#posture-conditions):

```text
node:os == 'macos'
node:tsVersion >= '1.80'
node:os IN ['macos', 'windows']
node:serialNumber NOT IN ['C02XYZ123']
custom:oncall == true
custom:blocked NOT SET
ip:address IN ['203.0.113.0/24', '198.51.100.9']
ip:country == 'VN'
```

The operators are `==`, `!=`, `<`, `<=`, `>`, `>=`, `IN`, `NOT IN`,
`IS SET` and `NOT SET`. Values are strings in single or double quotes,
numbers, `true` and `false`, or a list in brackets. Version strings such
as `node:tsVersion` compare segment by segment, so `'1.9'` is smaller than
`'1.10'`. A string that is a CIDR matches an address inside it. An
attribute the machine does not have satisfies only `NOT SET`; a
list-valued attribute such as `node:serialNumber` satisfies `==`, `IN` and
the ordered operators when any element does, and `!=` and `NOT IN` when
none does.

`ip:address` is the address the machine's control connection comes from,
so behind a reverse proxy it is the proxy's address unless the proxy is
configured to preserve the client address. `ip:country` needs a MaxMind
GeoLite2 or GeoIP2 country database at `policy.geoip_database`; without
one the server refuses a posture that uses it. A machine's source address
counts once it has connected; a posture that uses one of the `ip:`
attributes recomputes the policy for a machine when its address changes.

`POST /api/v1/posture/check` and `headscale postures check --expr ...`
parse expressions without storing them, and the console's posture editor
checks each line as it is typed.

### Schedules

A schedule is a set of weekdays, a start and an end as `HH:MM`, and an
IANA time zone, UTC when empty. An end before the start wraps past
midnight, so `sat 22:00`–`06:00` runs into Sunday morning. The server
recomputes the policy when a schedule boundary passes, to the minute, so
the rules open and close on their own. A posture may have a schedule and
no expressions, which makes a plain time window.

### Attaching postures to rules

A rule with postures admits a source machine only when it satisfies at
least one of them; a rule without postures admits every machine in its
source groups. The destinations are never narrowed. A posture a rule
names cannot be deleted; remove it from the rule first.

```console
headscale postures create --name "Current client" \
  --expr "node:tsVersion >= '1.80'" --expr "custom:blocked NOT SET"
headscale postures create --name "Office hours" \
  --days mon,tue,wed,thu,fri --start 09:00 --end 18:00 --timezone Asia/Ho_Chi_Minh
headscale postures list
headscale access-rules create --name "SSH from current clients" \
  --src 2 --dst 3 --protocol tcp --ports 22 --posture 1
headscale nodes posture show --identifier 7
```

Through the API, `GET`, `POST /api/v1/posture`, `GET`, `PUT`,
`DELETE /api/v1/posture/{id}` and the `postureIds` field of an access
rule, under `policy_file` (`policy_file:read` to list). `GET /api/v1/node/{id}/postures` lists the postures a machine satisfies right
now, and the console shows them under _Device posture_ on the machine's
page. The audit log records `posture.create`, `posture.update` and
`posture.delete`. In the console, postures live on the _Postures_ page under
_Access controls_, next to the rules and groups, and a rule's editor has a
_Required postures_ picker.

### Postures in the policy file

The policy file takes Tailscale's `postures`, `srcPosture` and
`defaultSrcPosture`:

```json
{
  "postures": {
    "posture:latestMac": ["node:os == 'macos'", "node:tsVersion >= '1.80'"],
    "posture:office": ["ip:address IN ['203.0.113.0/24']"]
  },
  "defaultSrcPosture": ["posture:latestMac"],
  "grants": [
    { "src": ["group:eng"], "dst": ["tag:prod"], "ip": ["22"], "srcPosture": ["posture:office"] },
    { "src": ["autogroup:member"], "dst": ["tag:web"], "ip": ["443"], "srcPosture": [] }
  ]
}
```

Posture names start with `posture:`. `srcPosture` on an ACL or a grant
narrows its sources to the machines that satisfy any of the named
postures; every expression inside one posture must hold.
`defaultSrcPosture` applies to every rule without `srcPosture`, and an
explicit empty `srcPosture` turns the default off for that rule. Postures
in the file have no schedule; use a database posture for that. The
`posture:#` prefix is reserved for the postures the access rules use.
