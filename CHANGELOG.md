# CHANGELOG

## 0.32.0 (202x-xx-xx)

### Changes

- Map requests that only bump LastSeen, endpoints or the DERP region no longer resend the whole node to every peer: each request is reduced to the narrowest change it justifies, a Hostinfo field no peer reads (shields-up, device model, client version) is stored without a broadcast, and a client reporting a new OS or version only recompiles the policy when a grant in use names a posture. Going offline sends peers the offline patch alone, health probes that change nothing no longer write, a user list that did not change no longer recompiles the policy, and an empty change is dropped before fan-out. Adds `slopscale_mapper_changes_dropped_total` and `slopscale_ha_health_updates_total` [juanfont/headscale#3417](https://github.com/juanfont/headscale/issues/3417)
- Two copies of the same node loaded separately no longer count as a policy change because their user pointers differ, and enabling or approving exit routes now does, so `autogroup:internet` follows the approval
- The noise transport serves HTTP/2 through the standard library, as the `golang.org/x/net/http2` server it used is deprecated; nothing changes on the wire
- Fix a node being listed among its own peers in an incremental map update, which crashes the Tailscale Android app on the device list [juanfont/headscale#3459](https://github.com/juanfont/headscale/pull/3459)
- The container documentation mounts `/tmp` as tmpfs alongside the socket directory, as the image expects
- The console's sidebar lists _Keys_ under _Access_, next to the access controls and _My access_, instead of under _Administration_. A member with no admin role has API keys of their own, so the group that held them could not be called administration; _Administration_ now holds only the settings and the integrations, which only a role that may read them sees

## 0.31.0 (2026-09-11)

### Changes

- The admin console moved from `/admin/` to `/console/`, and the server's front page (`/`) now sends a browser to it instead of showing a blank page. `/admin/` and everything under it redirect to the same page under `/console/`, so links in webhooks, invitations and bookmarks made before this release keep working. A proxy rule that blocks the console should cover both paths
- The console's app and machine preference forms edit a list of domains, connectors or routes as rows, each in its own input with a remove button and an "Add …" button under them, instead of chips in one box. A row can be read whole and edited in place, Enter adds the next row, and a wrong row says why under itself. A long domain no longer pushes the dialog wider than the screen
- Tags, groups, scopes, domains, routes and other identifiers across the console are plain text, one per line, rather than pills. A pill had to truncate to fit and hid the part that told two values apart
- The console no longer reports "Page not found" when a reverse proxy in front of slopscale answers a bare 404 while the server restarts; the page now says the server did not answer and may be restarting. Every error slopscale sends itself carries problem details, so a 404 without them can only come from the proxy
- `slopscale apps create`, `apps update`, `services create` and `services update` no longer crash on start: their `-c` shorthand (`--connector`, `--comment`) collided with the global `-c --config`. The long flags are unchanged; the shorthand is gone. A test now walks every command so a shorthand that shadows a global flag cannot ship again

## 0.30.0 (2026-09-10)

**Minimum supported Tailscale client version: v1.82.0**

### Renamed to slopscale

This fork of headscale is now called slopscale, and every name follows. The
binary and the CLI are `slopscale`, the config directory is `/etc/slopscale`,
the data directory is `/var/lib/slopscale`, the socket is
`/var/run/slopscale/slopscale.sock`, the environment prefix is `SLOPSCALE_`,
the Debian package, its system user and the systemd unit are `slopscale`, the
container image is `ghcr.io/aislopware/slopscale`, the NixOS module is
`services.slopscale` and the Go module is `github.com/aislopware/slopscale`.
The SSH recorder joins the tailnet as `slopscale-recorder` with the tag
`tag:slopscale-recorder`. Nothing changes inside the database.

An operator upgrading in place moves `/etc/headscale` and `/var/lib/headscale`
to the new directories, or points `database.sqlite.path` and the recording
directories at the old ones, renames `HEADSCALE_*` variables and replaces
`tag:headscale-recorder` in the policy. The console forgets the chosen theme
once, because its storage key moved.

### v1 REST API replaced; gRPC and Protobuf removed

The v1 REST API now provides an OpenAPI 3.1 specification at
`/api/v1/openapi.yaml`, with interactive documentation at `/api/v1/docs`. This
replaces the Swagger 2.0 document and the `/swagger` UI. The Protobuf, gRPC and
grpc-gateway stack behind it is gone, and the `slopscale` CLI now talks to the
HTTP API directly.

[#3324](https://github.com/juanfont/headscale/pull/3324)

### OAuth clients and scopes for the v2 API

The v2 API now authenticates with OAuth 2.0 client-credentials, the way the
Tailscale ecosystem does. An OAuth client mints short-lived access tokens whose
scopes limit which operations they may perform and whose tags limit the devices
they may create, so a credential can be issued with only the access it needs.
The `slopscale oauth-clients` command and the console's _Keys_ page manage
them, and the v1 API lists, creates and revokes them at `/api/v1/oauth-client`.
This lets the Tailscale Terraform provider and Kubernetes operator drive
Slopscale unchanged; admin API keys remain all-access.

[#3334](https://github.com/juanfont/headscale/pull/3334)

### User roles

Every user now has a role, one of `owner`, `admin`, `network-admin`, `it-admin`,
`auditor` and `member`, that bounds what the user may do through the admin API,
following Tailscale's user roles. The first user of a fresh server becomes the
owner; existing users start as members, and `slopscale users set-role` picks an
owner and assigns the rest. An API key created for a user
(`slopscale apikeys create --user`) is bounded by that user's role, on the v1 and
v2 APIs alike; keys without a user keep their all-access meaning. A key can
further be limited to some operations with `--scope` (or the scope picker on
the console's _Keys_ page) and carry a `--description`; scopes never reach past
the minting caller or the owner's role, a scoped key that names no scopes
passes its own on, and an OAuth token cannot mint API keys. The policy
gains `autogroup:owner`, `autogroup:admin`, `autogroup:network-admin`,
`autogroup:it-admin` and `autogroup:auditor`, and the devices of the owner and
admins carry Tailscale's `is-admin` capability. See
[User roles](https://aislopware.github.io/slopscale/ref/roles/).

### Device and user approval

Two tailnet-wide settings put an administrator between a node or user and the
tailnet, following Tailscale's device approval and user approval. With
`devicesApprovalOn`, a newly registered node waits until
`slopscale nodes approve` admits it. It has no peers, no peer sees it and the
client shows it as needing machine authorization. The wait is skipped when it
registered with a preauthorized pre-auth key (the default for keys,
`--preauthorized=false` opts out). With
`usersApprovalOn`, a user created by an OpenID Connect login cannot register
nodes until `slopscale users approve` admits them. Both switches are off after
an upgrade and everything that exists counts as approved; switching one off
approves everything that was waiting. `slopscale settings` manages the
switches, and the v2 API's `authorized` device flag, `needs-approval` user
status, `preauthorized` key capability and tailnet settings `PATCH` now carry
real meaning, so Tailscale tooling can drive approval. See
[Device and user approval](https://aislopware.github.io/slopscale/ref/approval/).

### Node sharing

A user can share one of their nodes with another user, following Tailscale's
node sharing: `slopscale nodes share --identifier <node> --user <user id>` (or
`POST /api/v1/node/{id}/share`), which a member may do for the nodes they own.
The policy decides what a share allows through the new `autogroup:shared`
source, which stands, per destination node, for the personal devices of the
users that node is shared with; the destination is always narrowed to the
shared node, so a share never opens anything else. The sharee's devices see the
node as a peer marked with the owner as sharer, the shared node gets no access
back, and a policy that never names `autogroup:shared` ignores shares. See
[Node sharing](https://aislopware.github.io/slopscale/ref/sharing/).

### DNS settings at runtime

The DNS configuration no longer needs a restart. Global nameservers, override
local DNS, split DNS, search domains and extra records can be changed from the
admin console's _DNS_ page, `slopscale dns set`, `PUT /api/v1/dns` or
Tailscale's `/api/v2/tailnet/-/dns/*` endpoints, and reach every client at
once; MagicDNS and the base domain stay in the configuration file. Settings set
this way are stored in the database, replace the file's `dns` section until
`slopscale dns reset` (or `DELETE /api/v1/dns`) returns to it, and are logged as
`dns.set` and `dns.reset`. The new `dns` and `dns:read` scopes gate them; a
network admin may write, an IT admin may read. Only what the Tailscale
client can use is accepted: a nameserver is an IP, an IP with port, or the
DNS over HTTPS URL of a provider the client knows (Cloudflare, Google, Quad9,
NextDNS and the like), and extra records are A or AAAA. An extra-records file
(`dns.extra_records_path`) keeps owning the records while the rest is edited.
Split DNS can also be handed to some groups only: a group DNS rule names
domains, nameservers and the groups whose machines receive them, from the
console's _DNS_ page, `slopscale dns rules` or `/api/v1/dns/rule`. See
[DNS](https://aislopware.github.io/slopscale/ref/dns/).

### DERP relays at runtime, embedded relay on by default

The embedded DERP relay is on by default, so every tailnet has a relay next
to its control server and a machine that cannot connect directly still has
a nearby path. It is published as region 999 next to Tailscale's public
relays; machines pick the closest region by latency. Its key is created
next to the noise key unless `derp.server.private_key_path` says otherwise,
and STUN listens on udp/3478. Set `derp.server.enabled: false` to keep the
old behaviour. The relay settings no longer need a restart either: the map
URLs, the refetch schedule, the embedded relay (on, off, region, STUN
address, published addresses, client verification) and relays you run
yourself can be changed from the admin console's _Relays_ page,
`slopscale derp set`, `slopscale derp relay add`, or `PUT /api/v1/derp`,
and reach every machine at once. Settings set this way are stored in the
database, replace the file's `derp` section until `slopscale derp reset`
(or `DELETE /api/v1/derp`) returns to it, and are logged as `derp.set`,
`derp.refresh` and `derp.reset`. The `feature_settings` and
`feature_settings:read` scopes gate them. A change fetches the maps first
and is refused, leaving everything as it was, when a map cannot be fetched;
`POST /api/v1/derp/refresh` refetches on demand. The map files in
`derp.paths`, the relay's key and
`automatically_add_embedded_derp_region` stay in the configuration file.
`GET /api/v1/server` now reports the region count and whether the relay
runs instead of listing the regions; the list, with where each region came
from, is at `GET /api/v1/derp`. Turning the relay off or client
verification on drops the machines connected to it, so they reconnect
under the new rule; a request that leaves `verifyClients` out gets
verification on; and a fetched map with a broken relay entry is served
without it rather than failing the refresh. See
[DERP](https://aislopware.github.io/slopscale/ref/derp/).

- `GET /api/v1/derp` returns an `ETag` for the settings in force and
  `PUT /api/v1/derp` accepts it back as `If-Match`: when someone else changed
  the settings in between, the request is refused with `412 Precondition
Failed` and nothing is written, so two operators editing the relays at the
  same time no longer overwrite each other. A request without `If-Match`
  behaves as before, and the `PUT` and the reset return the new `ETag`. The
  console sends the tag it read.

### Groups and access rules

Access can now be managed without a policy file, the way NetBird does it.
Machines and users go into named groups (a user's machines follow the user,
tagged machines join directly, the builtin _All_ group holds every machine),
and access rules let source groups reach destination groups on a protocol and
ports, one way or both ways. Rules only allow; the first enabled rule makes
everything else unreachable, and rules combine with the policy file when
there is one. Pre-auth keys can carry groups so the machines they register
join them. Manage it all from the console's _Access controls_ page, with
`slopscale groups` and `slopscale access-rules`, or through `/api/v1/group`
and `/api/v1/access-rule`. See
[Groups and access rules](https://aislopware.github.io/slopscale/ref/access-control/).

A new server no longer starts open. The builtin group _Own machines_ is
Tailscale's `autogroup:self`, a rule destination meaning the machines owned
by the same user as the source, and the first start seeds one builtin rule
from _All_ to it, enabled: each machine reaches the other machines of its
own user and nothing else, so machines of different users do not see each
other until an operator adds a rule. The builtin rule can be switched off,
which opens the tailnet, but not edited or deleted. On a database that
already has machines the rule is seeded switched off, so an upgrade does not
cut anything off.

### Key expiry setting and server info

`slopscale settings set --key-expiry-days` (or `keyExpiryDays` on
`/api/v1/settings`, `devicesKeyDurationDays` on the v2 tailnet settings)
caps how long a login stays valid, the way Tailscale's key expiry setting
does, without a config change or restart. `GET /api/v1/server` reports the
build, addresses, DERP regions and config file values of the running server,
and the console's _Settings_ page shows both. See
[Device and user approval](https://aislopware.github.io/slopscale/ref/approval/#key-expiry).

### Webhooks

Slopscale can now post events to your own endpoints or to a Slack,
Mattermost, Google Chat or Discord incoming webhook: a machine joining,
needing approval, being approved, expiring or being removed, a user being
created, approved, changing role or being deleted, and policy changes
(including group, rule and network edits). The delivery format, the event
data fields and the `Tailscale-Webhook-Signature` header are Tailscale's, so a
receiver written for Tailscale works unchanged, and the v2 API exposes the
same endpoints and `webhooks`/`webhooks:read` scopes as Tailscale's `webhooks`
resource; every admin role manages them, an auditor reads. Each endpoint has
a secret shown once, a test button, a rotate action and a history of its last
hundred deliveries with status, attempts and timing. Deliveries are posted to
the configured URL only (no redirects), at most sixteen at a time from a
bounded queue, and logs and audit entries carry the endpoint's host rather
than the URL, which for chat providers is a credential. Manage them from the
console's _Webhooks_ page, with `slopscale webhooks`, or through
`/api/v1/webhook`. See
[Webhooks](https://aislopware.github.io/slopscale/ref/webhooks/).

### Device trust

A machine can be suspended: it stays registered with its key and addresses
but loses every peer and cannot reach the tailnet, and its client shows a
health message saying so, until an administrator lifts the suspension.
Nobody has to sign in on the device afterwards, which makes it the
reversible alternative to expiring the key. `slopscale nodes suspend`,
`POST /api/v1/node/{id}/suspend`, the machine's menu and danger zone in the
console, the `node.suspension.set` audit action and the `nodeSuspended` and
`nodeUnsuspended` webhook events cover it.

Every machine now carries a device posture the policy can check, following
Tailscale's device posture: `node:os`, `node:tsVersion` and the other
attributes derived from what the client reports, `node:serialNumber` once the
new `postureIdentityOn` setting lets the server ask clients for their
identity, and `custom:...` attributes an operator sets with an optional
expiry, so a temporary marker such as an on-call rotation removes itself.
`GET /api/v1/node/{id}/posture`, `PUT` and `DELETE /api/v1/node/{id}/attributes/{key}`, Tailscale's
`/api/v2/device/{id}/attributes`, `slopscale nodes posture` and a _Device
posture_ section on the machine's page in the console cover it, under the
new `devices:posture_attributes` scope.

Postures turn those attributes into conditions. A posture is a list of
expressions in Tailscale's syntax (`node:tsVersion >= '1.80'`,
`node:serialNumber IN [...]`, `custom:oncall == true`,
`ip:address IN ['203.0.113.0/24']`, `ip:country == 'VN'` with a MaxMind
database at the new `policy.geoip_database`) and an optional weekly
schedule with a time zone, outside of which it does not hold. Attached to
an access rule, a posture narrows the rule's sources to the machines that
satisfy it, and the policy recomputes itself when an attribute, the source
address or a schedule boundary changes. The policy file takes Tailscale's
`postures`, `srcPosture` and `defaultSrcPosture` as well. `/api/v1/posture`,
`POST /api/v1/posture/check`, `GET /api/v1/node/{id}/postures`,
`slopscale postures`, the `--posture` flag of `slopscale access-rules`, a
_Postures_ page and a _Required postures_ picker in the console's access
controls cover it. See
[Device trust](https://aislopware.github.io/slopscale/ref/device-trust/).

### Temporary access

Access can now end on its own. An access rule takes an `expiresAt` and
stops applying past it, kept in the list as expired until it is extended
or deleted; a machine or user can be added to a group until a time and
leaves it again afterwards. A group can be marked requestable, and a
signed-in user, a member included, asks to join it for five minutes to
thirty days, for one machine or all of theirs, with a reason; anyone with
the `policy_file` scope approves for the duration asked or another one,
or denies with a note, and the membership is added with its expiry at
once. The server sweeps expired rules and memberships every minute and
rebuilds the policy. `--expires` on `slopscale access-rules` and
`slopscale groups add-node|add-user`, `--requestable` on
`slopscale groups`, `slopscale access-requests`,
`/api/v1/access-request`, the `accessRequestCreated`,
`accessRequestApproved` and `accessRequestDenied` webhook events, the
`access_request.*` audit actions, a _My access_ page for every signed-in
user and a _Requests_ page under the console's access controls cover it.
See [Temporary access](https://aislopware.github.io/slopscale/ref/temporary-access/).

### Notifications and log streaming

Webhooks reach people now, not only services. Four providers join the chat
ones: `teams` posts to a Microsoft Teams incoming webhook or workflow,
`telegram` posts to a bot's `sendMessage` URL with the chat taken from its
`chat_id` query parameter, `ntfy` posts to a topic, and `email` sends each
event as mail to the `mailto:` recipients through the server configured under
`notifications.smtp` (host, port, username, password, from, and `starttls`,
`tls` or `none`). Log streaming ships the audit log to a SIEM as it is
written, the way Tailscale's log streaming does: a stream names a
destination (`http`, `splunk`, `elastic`, `datadog`, `axiom` or `loki`), a
URL and a credential, and every audit event is batched and posted in the
shape that sink expects, with retries, counters of delivered and dropped
entries and a test entry on demand. The `logs:configuration` scope, held by
every admin role, manages streams; `logs:configuration:read` lists them
without their tokens. `slopscale log-streams`, `/api/v1/log-stream` and a
_Log streams_ tab on the console's _Integrations_ page (formerly _Webhooks_)
cover it. See [Log streaming](https://aislopware.github.io/slopscale/ref/log-streaming/)
and [Webhooks](https://aislopware.github.io/slopscale/ref/webhooks/#notifications).

### SSH session recording

Tailscale SSH sessions can be recorded, the way Tailscale's session
recording works. `ssh_recording.enabled` in the config file runs a recorder
inside the server: it joins the tailnet as `slopscale-recorder`
(`tag:slopscale-recorder`), takes the upload every machine's client sends
when a session starts, stores one asciinema file per session under
`ssh_recording.dir`, and deletes them after `ssh_recording.retention`. The
tailnet default recorders are a setting (`slopscale settings set --ssh-recorders tag:recorder`, `sshRecorders` in `/api/v1/settings`, the
_SSH session recording_ section of the console's _Settings_ page), an SSH
rule may name its own with `recorder` and require it with
`enforceRecorder`, and the tailnet-wide `sshRecordingEnforce` switch
rejects sessions that cannot be recorded. The policy needs no rule for a
recorder: the server adds a grant to every recorder's port. A client
reports a failed recording to `/machine/ssh/event`, which lands in the
audit log as `ssh.recording.*` and fires the `sshRecordingFailed` webhook
event. Recordings are listed, downloaded and deleted from the console's _SSH
sessions_ page, `slopscale ssh-recordings` and `/api/v1/ssh-recording`
under the `logs:configuration` scopes. See [SSH session
recording](https://aislopware.github.io/slopscale/ref/ssh-recording/).

### HTTPS certificates

Machines can get Let's Encrypt certificates for their MagicDNS names with
`tailscale cert` and `tailscale serve`, the way they do on Tailscale. With
`https_certificates.enabled`, every machine's name is announced as a cert
domain and the server answers `/machine/set-dns` by publishing the DNS-01
challenge record through the configured provider: `cloudflare` (API
token), `rfc2136` (dynamic update, TSIG-signed) or `command` (a program
given the record name and value). A machine may publish only its own
name's challenge record, and each one lands in the audit log as
`node.cert_challenge`. The base domain must be a public zone. See [HTTPS
certificates](https://aislopware.github.io/slopscale/ref/https-certificates/).

### Networks

Subnets and exit nodes can now be handed to groups as networks, the way
NetBird's networks and routes work. A network names prefixes, the machines
that route them and the groups that receive them: the routes are approved on
the routers when the network is created and withdrawn when it is disabled or
deleted (a route the operator approved by hand before the network stays), and
only the machines in its groups ever see them, which makes a split tunnel
without a policy file. Two routers make a failover pair. A network can narrow
what its groups reach behind the routers to a protocol and ports, the way an
access rule does, so a printer subnet can be handed out on TCP 631 alone.
Manage networks from the console's _Networks_ page, with every route any
machine advertises on the _Routes_ page next to it, with `slopscale networks`, or through
`/api/v1/network`. See [Networks](https://aislopware.github.io/slopscale/ref/networks/).

### Global exit node

`slopscale nodes global-exit-node --identifier <node>` (or
`POST /api/v1/node/{id}/global-exit-node`) marks an exit node every client is
told to prefer, with no policy involved: its exit routes are approved, the
marked nodes alone carry `suggest-exit-node` on every other client's view of
them and every node carries `auto-exit-node`, so `tailscale exit-node suggest`
names one and clients set to `--exit-node=auto:any` pick it. See
[Global exit node](https://aislopware.github.io/slopscale/ref/routes/#global-exit-node).

### Admin console

The server now serves a web console at `/admin/`: machines, users, pre-auth
and API keys, the policy editor, device and user approval, node sharing and the
global exit node and the audit log, all from a browser. The overview shows
counts, what is waiting for approval and the machines seen most recently. The
sidebar counts pending approvals next to _Machines_ and _Users_, Cmd-K
opens a search over pages, machines and users, and every table filters in
place. _Add machine_ hands over the join command per platform (Linux, macOS,
Windows, Docker, the phone apps) with a QR code carrying the same line, so a
machine without a shared clipboard can scan it. Panels, tables and dialogs
are framed: a tinted band carries the title, the column headers or the row
count, and the content sits on an inset panel with concentric corners; a
table's search, filters and primary action sit on the page above its frame; a
dialog's buttons sit on the band under its panel, in reach however long the
form is, since the panel is what scrolls. A
page with several parts, such as the access controls, DNS, relays, settings,
the keys or the integrations, is a branch of the sidebar with a page and an
address per part rather than a row of tabs. The policy file editor
underlines what the server would refuse as it is typed, completes section
names, rule keys and the names the file defines, explains them on hover,
and asks the server about the rest once typing pauses; the posture editor
colours and checks expressions the same way. URLs show their host in the
foreground. Copyable values keep their copy icon in view. There is a dark mode. It signs
in only through the configured identity
provider (Google, or any OIDC issuer) and shows what that user's role allows.
_Settings_ carries the IP backfill (`slopscale nodes backfillips`).
Long tables page at fifty rows, with the page size and the page controls
on the band under the rows, scroll sideways on a phone with the row menu
kept in reach, and figures line up on the right; the page is the only thing
that scrolls. The command that joins a machine, on the overview and in the
add-machine dialog, is coloured by what each word does and wraps between
words, in the same box the new key is shown in.
When something goes wrong, one page says what it means and what to do,
whether the server did not answer, the session ended, the account lacks
access or the address names nothing, with the server's own words folded
under it and a copy of them for a bug report; inside the console it keeps
the sidebar. A session that ends under an open page sends the operator to
the sign-in page and back to the same address afterwards. A binary built
without the console answers with a page in the same style.
_Machines_ keeps its filters in the URL, so a filtered list can be shared,
selects rows for approving, expiring or deleting in one go, and refreshes
on its own every fifteen seconds while the tab is open. A disabled control
says why on hover, the theme is a Light, Dark, System menu, and dates are
picked with a calendar rather than the browser's own field.
A state is a word in the row ("Connected", "Expired", "HTTP 500"), coloured
only when it needs attention, with no dot or pill in front of it; a page's
state sits on the line under its title rather than above it. Empty lists say
what is missing and offer the one action, without an icon over the heading.
The primary button is ink on the page rather than blue, as are the switches,
so blue is left to links. The console uses the platform's own type face.
Release binaries and container images include it. When building from source,
run `make web` before `make build`.
See [Admin console](https://aislopware.github.io/slopscale/ref/console/).

### Audit log

Every writing API request, whether from the CLI, the console or a script, is
recorded with who made it, what it touched and how it ended, alongside console
sign-ins. Read it with `slopscale audit list`, `GET /api/v1/audit` or the
console's _Audit log_ page; bound it with `audit.retention`. See
[Audit log](https://aislopware.github.io/slopscale/ref/audit/).

The log can be downloaded as a file: `GET /api/v1/audit/export` takes the same
filters as the list plus `format=csv|json` and streams every matching event,
oldest first, as an attachment named after the window it covers. The server
reads the log in pages while it writes, so a long export does not build up in
memory; one export carries at most 100000 events, so narrow `since` and
`until` to walk a longer log. It needs the same `logs:configuration:read`
scope as reading the list. The console's _Audit log_ page has an _Export_
button and `slopscale audit export` writes the file from the CLI.

### Console sessions and user invites

Console sign-ins can be listed and ended. `GET /api/v1/auth/sessions` shows
every unexpired browser session with the user, when it was opened, when it was
last active, the address and browser it came from, and which one is making the
request. An administrator can end any session with
`DELETE /api/v1/auth/sessions/{id}` or sign a user out of every browser with
`DELETE /api/v1/user/{id}/sessions` (_Sign out everywhere_ on the console's
user page); a member sees and ends only their own. From the CLI these are
`slopscale sessions list`, `slopscale sessions end` and
`slopscale users sign-out`. Both are recorded in the audit log as
`session.end` and `user.sessions.end`.

Users can be invited by email. `POST /api/v1/invite`, `slopscale invites
create` or _Invite_ on the console's _Users_ page returns a one-time link and
mails it to the address when `notifications.smtp` is configured. The first
login that opens the link, or whose verified email matches the invitation,
creates the user approved, even while user approval is on, with the invited
role and groups. Invitations expire (seven days by default, thirty at most),
can be revoked with `DELETE /api/v1/invite/{id}` and re-sent with a fresh link
with `POST /api/v1/invite/{id}/resend`. An invitation cannot hand out
ownership; transfer it instead. See
[Console](https://aislopware.github.io/slopscale/ref/console/).

### API key rotation

An API key can be rotated: `POST /api/v1/apikey/{prefix}/rotate`,
`slopscale apikeys rotate` or the _Rotate_ action on the console's _Keys_ page
mints a new secret for an existing key and returns it once, while the key keeps
its id, owner, scopes, description and expiry, so nothing that refers to the
key has to be re-created. The old secret is refused from the moment the call
returns, which makes rotation the way to replace a leaked key without a window
in which both work. The body may carry an `expiration` to set a new expiry;
omitting it keeps the current one. An expired key cannot be rotated, since
expiry is how a key is revoked, so a replacement must be created instead. A key
that carries its own scopes may only rotate a key no wider than itself, and
rotations are recorded in the audit log as `apikey.rotate`.

### Funnel

`tailscale funnel` works: a machine the policy grants the `funnel` node
attribute can expose a service to the internet, and the server adds the
Funnel ports and the grant that lets the ingress reach it. The ingress is a
node you run, since Tailscale's ingress servers are not available to a
self-hosted control plane: `funnel.enabled` runs one inside the server
(joining as `slopscale-ingress`), and `slopscale ingress` runs one on any
machine with a public address. It reads the server name from the TLS client
hello and hands the bytes to the machine over the tailnet, so the machine
terminates TLS with its own certificate and the ingress sees nothing in
clear. Funnel needs HTTPS certificates on and public DNS pointing the
machines' names at the ingress. `tailscale funnel` fails plainly while no
ingress node has joined. Certificate assistance now also stamps the `https`
node attribute, so `tailscale serve` no longer needs it granted in the
policy. The console marks Funnel machines on the machines list and page,
and the Server page shows the ingress nodes. See
[Funnel](https://aislopware.github.io/slopscale/ref/funnel/).

### Tailscale Services

A service is a name with addresses of its own, served by one or more
tagged machines, the way Tailscale Services work: `tailscale serve
--service=svc:web` on a machine announces it, an operator approves the
machine (or `autoApprovers.services` in the policy does), `tailscale serve
advertise svc:web` makes it active, and `https://web.<base domain>` reaches
whichever approved machine is serving, an online one first, so a service
can move or run on two machines without anyone noticing. The server
allocates the service's addresses, learns what each machine serves over
its control connection, routes every peer to one host, publishes the name
in MagicDNS and lists the service in the Tailscale apps. The policy takes
`svc:web` as a destination. `slopscale services` manages them, the console
has a _Services_ page and a section on each machine's page, the v1 API has
`/api/v1/services` and `/api/v1/node/{id}/approve_services` under the new
`services` scope, and the v2 API has Tailscale's `/vip-services`, so the
Terraform provider's `tailscale_tailnet_service` works. See
[Services](https://aislopware.github.io/slopscale/ref/services/).

A policy change and an approval or service change landing in the same
batch no longer lose the self node the second one asked for: the batcher
folds what a dropped repeat asked for into the change it keeps.

### Tailnet lock

Tailnet lock works with the stock client: `tailscale lock init` on a
machine owned by the owner or an admin proposes the authority, the server
has the node sign every machine and switches the lock on, and from then on
every machine drops a peer whose node key carries no signature from a
trusted key, so a compromised control server cannot add a machine
unnoticed. The server keeps the authority's log in the database, hands
every machine the head so it bootstraps or syncs over its control
connection, verifies and stores the signatures `tailscale lock sign` and
pre-signed auth keys bring, hands a machine whose key changes its old
signature so it re-signs the new one by itself, and serves `add`,
`remove`, `revoke-keys` and `disable`. `slopscale lock status` and
`GET /api/v1/tailnet-lock` show the state, the trusted keys and which
machines are waiting for a signature; `slopscale lock disable`,
`POST /api/v1/tailnet-lock/disable` and the console's _Switch off_ on the
_Settings_ page switch it off with the secret minted by
`--gen-disablement-for-support`. See
[Tailnet lock](https://aislopware.github.io/slopscale/ref/tailnet-lock/).

### Identity tokens

`tailscale id-token <audience>` works: the server signs a JSON Web Token
about the machine (its MagicDNS name, node key, addresses, tags or user,
in the claims Tailscale documents) with an ES256 key it makes on first use
and stores in the database, so every server on the same database signs
alike. A verifier finds the key through OpenID discovery at the server
URL, `/.well-known/openid-configuration` and `/.well-known/jwks.json`,
which lets a secrets store or a cloud account's OpenID federation trust a
machine without a password. A machine waiting for approval, suspended or
expired gets no token. See
[Identity tokens](https://aislopware.github.io/slopscale/ref/identity-tokens/).

### Apps and app connectors

An app is a set of domains reached through app connectors, Tailscale's
feature: a machine running `tailscale set --advertise-connector` and
carrying one of the app's tags resolves the domains, advertises a route
for every address it learns and forwards the traffic, and the server
approves those single-address routes as they appear, so the operator
only ever approves a real subnet. Every machine gets split DNS for the
app's domains pointing at the connectors it can reach, the way the
hosted control plane does, so a query reaches a connector and it learns
the address; nothing has to be set up under DNS. Wildcards
(`*.example.com`) work, an app can pin routes the connectors always
advertise, `*` picks every tagged machine running the connector, and a
connector is a tagged machine, as with Tailscale. `slopscale apps` manages them, the console has an _Apps_ page
under _Connectivity_ with each connector's learned and pending routes,
and the v1 API has `/api/v1/apps` under the `policy_file` scope. See
[Apps](https://aislopware.github.io/slopscale/ref/apps/).

### Posture integrations

The server can ask CrowdStrike Falcon, SentinelOne, Microsoft Intune,
Jamf Pro, Kandji and Kolide what they know about each machine, matched by
the serial number the client reports, and write the answer as posture
attributes with the prefix and names Tailscale's integrations use
(`falcon:ztaScore`, `intune:complianceState`, `jamfPro:fileVaultStatus`,
`kolide:authState` and the rest), so a posture written for Tailscale works
unchanged. Every enabled integration syncs every fifteen minutes, when it
is saved and on request; a provider that fails keeps the previous
attributes and shows the error on the integration. One enabled
integration per provider. The console's _Integrations › Device posture_
page sets them up with a connection test, `slopscale posture-integrations`
does the same from the shell, and the v1 API has
`/api/v1/posture-integrations` under the `devices:posture_attributes`
scope; secrets are stored and never returned. See
[Device trust](https://aislopware.github.io/slopscale/ref/device-trust/).

### SSH from the console

A machine page has an _SSH_ button that opens a terminal in the browser
to any online machine running Tailscale SSH. The console runs Tailscale's
own in-browser client, compiled to WebAssembly and served with the
console, which joins the tailnet as an ephemeral machine of the operator's
own with a one-time key from `POST /api/v1/ssh-session`; whether the
session is allowed is the SSH policy's decision, as for any other machine
of theirs, and the machine disappears when the tab closes. An operator
whose role does not read devices can only open a session to a machine a
machine of theirs already sees. `make web`
builds the client (`make wasm` alone builds only it), and a console built
without it says so on the terminal page.

### Funnel ingress shuts down promptly

Stopping the embedded Funnel ingress closes the connections it is relaying
from both ends, so a client that holds a connection open without sending
anything no longer keeps the server from shutting down.

### Relay latency and the access graph

Every client reports how far each relay is; the console's _Relays ›
Latency_ page and `GET /api/v1/derp/latency` show, per region, how many
clients prefer it and their median and 90th-percentile latency, and a
machine's page shows its own report with its home relay. _Access
controls › Graph_ and `GET /api/v1/access-graph?node=` show, for one
machine, which peers it can reach and which can reach it, with the
ports, computed from the compiled policy exactly as the clients receive
it. The machine object carries `netInfo`, `appConnector` and `sshServer`.

### Machines set their own attributes

A machine can set its own `custom:` posture attributes over its control
connection, Tailscale's experimental `set-device-attr`, once the new
`deviceAttributesOn` setting is on (`slopscale settings set
--device-attributes=true`, or _Machines set their own attributes_ under
_Settings_ in the console). It is off by default, because root on a machine
could otherwise give it any attribute a policy trusts. Attributes set this
way carry the comment _Set by the machine_ and land in the audit log with
the machine as the actor.

### Client update notices

The server reads the latest stable Tailscale release from
pkgs.tailscale.com once a day and tells each client whether it runs it, so
clients behind an older release show Tailscale's own "update available"
health warning and, with auto-update on, update themselves. `client_updates`
in the config file turns the lookup off or changes how often it runs. The
node object carries `clientVersion` and `updateAvailable`, the server info
`latestClientVersion`, and the console shows the client version on the
machine page with an _Update available_ status and the latest release on
the Server page.

### Hardware attestation

A client started with `tailscaled --hardware-attestation` signs every map
request with a key generated inside the machine's TPM, which cannot be
copied off it. The server verifies the signature against the machine's node
key and offers the result to the policy as `node:hardwareAttested`, so a
rule can insist that traffic comes from the machine itself rather than from
a copied node key; `node:tpm` says whether the client found a TPM at all. A
machine that stops signing loses the attribute on its next map request. The
node object carries `hardwareAttestation` and `tpm`,
`slopscale nodes attestation reset` and
`DELETE /api/v1/node/{id}/hardware-attestation` forget the record after a
TPM was cleared, and the audit log records every change of state. Linux and
Windows machines with a TPM 2.0 can attest; the Apple and Android clients
do not.

### Control dial plan

`control_dial_plan` in the config file lists addresses clients try for the
server before resolving its name, and keep between runs, so a tailnet
survives a DNS outage; clients fall back to the name when none answers.

### Database backup and restore

`slopscale db backup` copies the SQLite database through SQLite itself, so a
backup can be taken while the server runs; copying the file with `cp` cannot,
because recent writes sit in the write-ahead log and the two files are read at
different moments. `slopscale db verify` checks a backup, and
`slopscale db restore` puts one back, keeping the database it replaces as
`<database>.pre-restore-<timestamp>`. PostgreSQL is still backed up with
`pg_dump`. See
[Backup and restore](https://aislopware.github.io/slopscale/setup/backup/).

### Peer relays

The grant that lets machines relay traffic for each other, which slopscale
has always compiled, is now documented, together with the
`disable-relay-server` node attribute and how to check a relay. See
[Peer relays](https://aislopware.github.io/slopscale/ref/networks/#peer-relays).

### Managing machines over their control connection

The server can now ask a connected machine to update its Tailscale
installation, report the health warnings it would show its own user, hand
over a support dump (its preferences, network map, metrics, goroutines,
socket stats or tailnet lock log), list the logins it suggests for a
Tailscale SSH session, report the routes it learned as an app connector and
the state of the certificate it caches for Serve and Funnel. All of it
rides the control connection the client already holds, so no inbound port
or agent is involved, and a client that refuses is reported with its own
words. `slopscale nodes update`, `nodes health` and `nodes diagnostics`
drive it, and the API adds these under `/api/v1/node/{id}/`, with
`POST /api/v1/nodes/client-update` for a fleet at once. The console's machine
page shows the same: an _Update now_ button and a bulk _Update clients_
action, a _Client health_ section with the diagnostics downloads, a
_Preferences_ section, the certificate state under _Connectivity_, an app
connector's learned routes, and the login hints on the SSH page.

A machine whose owner ran `tailscale set --remote-config` also lets the
server change a curated set of its preferences, from the routes it
advertises to whether it runs Tailscale SSH, with `slopscale nodes prefs
get` and `nodes prefs set` or `PATCH /api/v1/node/{id}/preferences`. That
opt-in hands the tailnet admin the machine's whole local API, so it belongs
on fleet machines rather than personal ones. See
[Device management](https://aislopware.github.io/slopscale/ref/device-management/).

### Terraform provider and Kubernetes operator

The v2 API now covers what the Tailscale Terraform/OpenTofu provider and the
Kubernetes operator ask of a control server, so both drive Slopscale
unchanged. The provider can validate a policy before applying it
(`tailscale_acl` checks every plan), read the stored policy back byte for byte
with its comments intact, change an OAuth client in place, manage the whole
DNS configuration in one resource, and manage posture integrations and the
audit log stream as resources. A device read with `fields=all` now carries the
detail the provider's device data source expects: whether the machine blocks
incoming connections, whether it holds a control connection, its distribution,
its tailnet lock key, whether it runs Tailscale SSH, its collected serial
numbers and its connectivity, including per-relay latency. The default device
fields are unchanged.

The Kubernetes operator's whole startup and reconcile path is covered by a
test: it authenticates as an OAuth client, probes devices, keys and services,
mints an auth key for a proxy, registers the proxy's service and deletes the
device when the proxy goes away. A service created before its ports are known
(the operator sends `do-not-validate`) is now accepted rather than refused.

Not everything the provider offers exists here: `tailscale_contacts`,
`tailscale_aws_external_id` and network flow log streams have nothing behind
them in Slopscale and are refused with an explanation. See
[API](https://aislopware.github.io/slopscale/ref/api/) for the resource-by-resource
table.

### Workload identity federation

A CI job or cloud workload can now get a Slopscale access token by presenting
the OIDC token its own platform already signs for it, so no long-lived secret
has to be stored in a pipeline. Register the workload as a key with
`keyType: "federated"`, naming the issuer, audience and subject its tokens
carry, optionally with extra claim rules it must satisfy, and give it the
scopes and tags it needs. The workload exchanges its token at
`POST /api/v2/oauth/token-exchange` for a one-hour access token with exactly
that grant. The presented token is verified against the issuer's published
keys, with a minute of clock leeway, and every exchange is recorded in the
audit log as `oauth.token.exchange`, refusals included.

The console and the CLI manage identities next to OAuth clients, so neither
needs the Tailscale-compatible API. The console's OAuth clients page and
`slopscale oauth-clients create --federated` make one, and the page and
`slopscale oauth-clients update` change its scopes, tags and trust conditions
in place, where a field left out keeps the value it has. An OAuth client on
`/api/v1` now reports its `keyType` and, for an identity, its issuer, audience,
subject and claim rules; `PATCH /api/v1/oauth-client/{clientId}` is the
endpoint behind the edit.

### Protocol

- A machine waiting for approval now gets a health message saying so, with
  a link to the console's machines page, the way a suspended machine
  already did.
- `GET /machine/whoami`, which `tailscale debug ts2021` uses to check a
  control connection, answers with the machine's nodes instead of `501`.

### BREAKING

#### API

- The gRPC API is removed; all programmatic access now goes through the HTTP API at `/api/v1` [#3324](https://github.com/juanfont/headscale/pull/3324)
- API errors are now RFC 7807 `application/problem+json`, including authentication failures, instead of the previous gRPC-status JSON shape [#3324](https://github.com/juanfont/headscale/pull/3324)
- Errors that previously returned HTTP 500, such as unknown users or nodes, malformed input and duplicate names, now return the correct 404, 400 or 409 [#3324](https://github.com/juanfont/headscale/pull/3324)
- `GET /api/v1/policy` returns 404 instead of 500 when no policy has been set
- The OpenAPI document is OpenAPI 3.1 at `/api/v1/openapi.yaml` (docs at `/api/v1/docs`), replacing Swagger 2.0 at `/swagger` [#3324](https://github.com/juanfont/headscale/pull/3324)

#### CLI

- `--output json` / `--output yaml` now emit the API's shape (camelCase fields, string-encoded IDs, RFC3339 timestamps) instead of the old Protobuf encoding [#3324](https://github.com/juanfont/headscale/pull/3324)
- `slopscale policy` renames the database-bypass flag from `--bypass-grpc-and-access-database-directly` to `--bypass-server-and-access-database-directly` [#3324](https://github.com/juanfont/headscale/pull/3324)

### Changes

- SQLite is now driven by mattn/go-sqlite3, the C library compiled through cgo (SQLite 3.53.4), instead of the transpiled-to-Go modernc.org/sqlite, which cuts CPU time per query on small machines such as a Raspberry Pi. Release binaries and container images are statically linked against musl for linux amd64, arm64 and armv7; macOS and FreeBSD binaries are no longer published, build them from source with a C compiler or through the Nix flake. Go module, Nix flake and container base images are refreshed to their current releases
- GORM is gone: the database layer now builds its queries with go-jet/jet and hand-written migrations, and PostgreSQL connects through pgx's own pool. The on-disk schema and every migration are unchanged; existing databases upgrade in place
- `database.gorm` is replaced by `database.query_log` (`slow_threshold`, `log_not_found`, `parameterized`); the old keys are still read with a deprecation warning and `prepare_stmt` is dropped because statements are always prepared and cached
- SQLite runs with a 64 MiB page cache per connection instead of SQLite's 2 MiB default
- SQLite maps up to 256 MiB of the database file into memory (`mmap_size`) and keeps temporary tables in memory instead of on disk, so reads on a machine with spare RAM skip the page cache copy and sorts do not touch the disk. The lookups on the map request and registration paths (`nodes.node_key`, `nodes.machine_key`, `nodes.user_id`, `pre_auth_keys.key`) are indexed by a migration, which a large database on PostgreSQL will notice on every registration. The hot paths do less work per request: a node is read from the in-memory store without a copy, an unchanged Hostinfo is recognised without cloning it four times, the peer list is filtered through the policy once per map response instead of twice, the DERP map is shared instead of cloned into every full map, and a node's posture inputs are compared in place on every store write. `make build` now strips the binary like the release builds do
- The in-memory node store no longer recomputes who may see whom on every write. A write that changes none of the inputs of that computation, which is every endpoint, DERP region or last-seen update, carries the previous peer map forward; only a new or deleted node, a change of tags, user, addresses, routes, shares, posture or approval, or a policy reload recomputes it. On a 1000-node tailnet that turns a 30 ms, 23 MiB rebuild per write into 1.6 ms and 1.5 MiB. The recompute itself is faster too: pairs are indexed by position instead of by map key (46% less time for 1000 nodes), the snapshot rebuild allocates a third of what it did, a map request from an unchanged address no longer queues a store write, a broadcast change is no longer copied once per connected node, and the poll session keeps a view of the node instead of cloning it. A server start runs the SQLite foreign key check only after a migration actually ran.
- A DERP map source that cannot be reached no longer keeps the server from starting: the map is built from the local regions, the failure shows on the Relays page and in `slopscale derp`, and the server retries every five minutes until a fetch succeeds. The retry runs off the scheduler, so an unreachable source no longer stalls key expiry and health checks for the length of its back-off.
- Map responses cost less CPU and memory to build and send: the control protocol is encoded with Go's `encoding/json/v2` into pooled buffers, via grants are resolved once per policy change instead of once per viewer-peer pair, and the hot database statements are rendered once and only bound per call. A full map for a 100-node tailnet takes about 40% less CPU and 60% fewer allocations than before
- `SLOPSCALE_DEBUG_DEADLOCK` and `SLOPSCALE_DEBUG_DEADLOCK_TIMEOUT` are removed; they configured a lock detector no lock used
- SQLite is compiled in defensive mode with double-quoted string literals disabled (the flags in `sqlite.cflags`), so SQL that could corrupt the database file is refused and a mistyped `"identifier"` is an error rather than a silent string; a binary built without those flags still runs but logs a warning at startup
- Expiring or deleting a non-existent pre-auth key now returns an error instead of silently succeeding [#3324](https://github.com/juanfont/headscale/pull/3324)
- A machine that re-registers under a new hostname (`tailscale up --force-reauth` after a re-image) gets its MagicDNS name from the new hostname, as it does when the hostname changes on a running machine; a name an administrator chose is kept [#3432](https://github.com/juanfont/headscale/issues/3432)
- The embedded DERP server's `/bootstrap-dns` now answers the `q` parameter a client sends when its own DNS is broken, and includes the control server's own address next to the DERP nodes, resolved every ten minutes instead of on every request. A client can therefore find the server through the DERP it still reaches by IP, which is what `tailscale switch` between two servers needs [#2757](https://github.com/juanfont/headscale/issues/2757)
- Online peers now carry `LastSeen` in the map response and in the online and offline patches, as Tailscale's control plane sends it. The Apple clients on 1.102 read a peer without it as never seen and showed it offline in the peer list, and the `tailscale status` last-seen column was empty for online machines [#3415](https://github.com/juanfont/headscale/issues/3415), [#3420](https://github.com/juanfont/headscale/issues/3420)
- The `dns-subdomain-resolve` node attribute now reaches peers: a client answers `*.<machine>` with that machine's addresses only when the attribute is on its peer entry, and slopscale set it on the machine's own entry alone [#3322](https://github.com/juanfont/headscale/issues/3322)
- Deleting a machine, by hand or by ephemeral clean-up, now ends its map stream with its own key expired, and a machine that polls with a key the server no longer knows gets the same answer instead of a 404. The client goes to "needs login" at once, where before it kept receiving keep-alives, then retried the 404 until someone ran `tailscale up --force-reauth`, and its stale stream held up a graceful shutdown [#3410](https://github.com/juanfont/headscale/issues/3410)
- Every approved exit node is now suggested to the other machines (`suggest-exit-node` on their view of it), as Tailscale's control plane does with no policy at all. The macOS and iOS apps since Tailscale 1.102 build their exit node list from the suggestion and showed "No exit nodes available" without one. Marking a global exit node narrows the suggestion to the marked machines, as before [#3415](https://github.com/juanfont/headscale/issues/3415)
- Extra DNS records from `dns.extra_records` and `dns.extra_records_path` are lowercased like those set through the API, so `Printer.fritz.box` resolves [#2782](https://github.com/juanfont/headscale/issues/2782). The watched file is read once it has been quiet for a moment rather than on the first of a write's several events, which parsed half a file [#2753](https://github.com/juanfont/headscale/issues/2753), and an event carrying several operations at once (a truncating rewrite on macOS reports write and chmod together) is no longer dropped
- A map request that changes nothing, a periodic re-send, a reconnect with matching state or STUN-only endpoint churn, no longer counts as "node added" and fans a peer change out to every connected machine [#3417](https://github.com/juanfont/headscale/issues/3417)
- Peers now see each machine's own capability version (`Cap` on the peer entry), as Tailscale's control plane sends it, and an upgrade reaches them as a patch. Every peer used to carry the viewer's version, so a current client took an old peer for one that speaks peer relay and other newer paths
- A client that asks for a one-shot map (`Stream` off without `OmitPeers`) now gets one full map response instead of an empty body. A register request with a malformed body is refused with 400 before it can consume a pre-auth key, and a client below the supported capability version is refused before the request has any effect
- A machine's own endpoint and DERP updates are no longer echoed back to it as a patch about itself, and the user profiles in a map response are limited to the users of the machines the recipient can see, as Tailscale's control plane does. A peer's entry no longer carries its `NetInfo` (link measurements and the DERP latency table), which the hosted control plane trims and which nothing on the receiving side reads
- The tailnet's key expiry, when set, is published as the `tailnet.maxKeyDuration` node capability in seconds, the shape the hosted control plane uses, for clients that read it
- `/machine/audit-log`, which a client under an always-on device policy posts when its user disconnects with a reason, is recorded in the audit log as action `node.client.disconnect` with actor kind `node`; the server answered 501 before and the client gave up on the entry. `/machine/feature/query`, behind `tailscale serve --https` on a machine without the capability, now names the node attribute to grant, links to the policy page and keeps the command waiting until the policy grants it; `tailscale funnel` gets an error, as before, because Funnel needs Tailscale's public ingress
- Warnings a client reports in its map requests (`warn-ip-forwarding-off` on a subnet router whose kernel drops forwarded packets, `warn-router-unhealthy`, `warn-etc-apt-source-disabled`) are kept on the node as `clientWarnings` in `GET /api/v1/node` and shown on the console's machine page, where before they were dropped
- A nameserver can be kept in use while a machine routes through an exit node, like Tailscale's per-nameserver "Use with exit node" setting: `dns.nameservers.use_with_exit_node` in the configuration file, the switch next to each nameserver and split DNS domain on the console's _DNS_ page, `slopscale dns set --use-with-exit-node` and `--split-use-with-exit-node`, and `useWithExitNode`/`splitUseWithExitNode` in `PUT /api/v1/dns`. The rest of the machine's DNS goes through the exit node then, as before. Global nameservers need override local DNS, as the client only honours the flag on the resolvers it uses for every query, and a split DNS domain survives only when every one of its nameservers is kept; the API enforces the first and the console marks the whole domain. Needs Tailscale 1.88.1 or later on the client [#2816](https://github.com/juanfont/headscale/issues/2816), [#3376](https://github.com/juanfont/headscale/issues/3376), [#2234](https://github.com/juanfont/headscale/issues/2234)
- A client that asks to be ephemeral in its register request is now ephemeral: a `tailscaled` with `--state=mem:`, a `tsnet` program with `Ephemeral` set or the browser client is deleted on logout and after the ephemeral inactivity timeout offline, with a regular pre-auth key or an interactive login alike, where before only an ephemeral pre-auth key counted and such nodes piled up. `slopscale nodes list`, the console and the `ephemeral` field of the v1 node report either kind
- A DERP map file can carry `homeparams.regionscore` to prefer or avoid regions when a client picks its home DERP, as the hosted control plane's map does. The scores are merged across the loaded maps, later files winning, where before they were dropped in the merge. See [DERP](https://aislopware.github.io/slopscale/ref/derp/#customize-derp-map)
- Groups can be synced from the identity provider: with `oidc.groups.sync` on, a user's `groups` claim is mirrored into slopscale groups of the same name at every sign-in (optionally only the claims with `oidc.groups.prefix`, stripped), and the user leaves the synced groups the claim drops, like Tailscale's user and group provisioning. Synced groups show as such in the console and the API (`source: oidc`); their users and name cannot be edited by hand, machines and description can, and an operator-made group is never taken over by name. Each sync that moved a membership is logged as `group.sync`
- `oidc.match_by_email`: a login whose provider identifier is unknown is matched to the existing OIDC user with the same verified email, the user moves to the new identifier and keeps its machines, so an identity provider can be switched without editing the database. The switch is logged as `user.provider.switch`; a login is refused when several users share the email [#2438](https://github.com/juanfont/headscale/issues/2438)
- `nodeAttrs` accepts the `app` field, application capabilities with data such as Tailscale's app connector definitions (`tailscale.com/app-connectors`): the values reach the targets' node capability map verbatim, values from several entries for the same capability add up, and the policy is refused when a capability is not domain-qualified or a value is not a JSON object. A policy with `app` was rejected as unknown before [#3021](https://github.com/juanfont/headscale/issues/3021)
- `nodeAttrs` accepts `ipPool`, Tailscale's IP pools: a new node whose user, group, tag or autogroup a grant names is numbered from the grant's IPv4 ranges, the first pool with room first; existing nodes keep their address, a full set of pools refuses the registration, and pools must lie within `prefixes.v4` [#2912](https://github.com/juanfont/headscale/issues/2912)
- Regional routing for high availability subnet routers: when the routers for a prefix report different DERP home regions, each client is steered to the router in its own region and falls back to the tailnet-wide primary when its region has no healthy router; a client that changes region is steered again. The `/debug/routes` endpoint lists the per-region primaries [#3237](https://github.com/juanfont/headscale/issues/3237)
- A user's display name, email and profile picture can be changed after creation with `slopscale users set` or `PATCH /api/v1/user/{id}`, and the clients show the new profile on their next map update; the picture must be an https URL. Renaming a user reaches the clients the same way, where before the map response kept the old profile until a restart [#2166](https://github.com/juanfont/headscale/issues/2166)
- Improve systemd service file hardening [#3341](https://github.com/juanfont/headscale/pull/3341)
- Slopscale now requires Go 1.27 to build
- Fix `slopscale policy set --bypass-server-and-access-database-directly` storing the policy with its comments blanked out; the file is now saved as written
- Fix the OIDC success page always saying "Node registered"; a node logging in again now sees "Node reauthenticated"
- Fix a registration followup that arrives after the login completed being refused with "extending key is not allowed"; the client now gets its registered node
- Fix a node that registers or changes under a policy which hides it (one whose own filter is empty, such as `autogroup:shared` before anything is shared, or `autogroup:self`) still reaching the netmaps of nodes that could not access it: the incremental map update skipped the policy whenever the recipient's own filter was empty, and now applies the same pairwise rule as the full map
- User roles: `slopscale users set-role`, a `Role` column in `slopscale users list`, `POST /api/v1/user/{id}/role`, `GET /api/v1/whoami`, a `userId` on API keys and `slopscale apikeys create --user`; the v2 user object's `role` field and `?role=` filter now reflect the real role
- The `is-admin` node capability, previously stamped on every node, is now stamped only on devices of the owner and admins; `is-owner` on the owner's. Clients use these for admin-console affordances in their UI only
- `POST /api/v1/apikey` without an `expiration` now mints a key that never expires instead of one that was already expired
- Admin console at `/admin/`, embedded in the binary; `make web` builds it from `web/`
- Console sign-in through the identity provider only: `/oidc/login` opens a seven-day session cookie for the OIDC user, `GET /api/v1/auth/console` names the provider, `DELETE /api/v1/auth/session` signs out; `GET /api/v1/whoami` reports `kind: session`. The client ID and secret may be set as `SLOPSCALE_OIDC_CLIENT_ID` and `SLOPSCALE_OIDC_CLIENT_SECRET`
- `oidc.admin_users` (`SLOPSCALE_OIDC_ADMIN_USERS`): email addresses that become admins the moment they sign in, so a fresh server can be administered from the console without CLI role grants; the promotion is recorded in the audit log
- `go run ./cmd/dev` starts a mock identity provider next to the development server and `make test-e2e` signs in through it from a browser, so the console's sign-in can be exercised without Google
- Audit log: `audit_events` table, `GET /api/v1/audit` with `actorUserId`, `action`, `targetKind`, `targetId`, `since`, `until`, `before` and `limit`, `slopscale audit list`, the `logs:configuration:read` scope (held by every role but member) and `audit.retention` in the configuration
- Global exit node: `slopscale nodes global-exit-node`, `POST /api/v1/node/{id}/global-exit-node` and `globalExitNode` on nodes
- Node sharing: `slopscale nodes share|unshare`, `POST /api/v1/node/{id}/share`, `DELETE /api/v1/node/{id}/share/{userId}`, `sharedWith` on nodes and the `autogroup:shared` policy source
- Device and user approval: `slopscale settings get|set`, `slopscale nodes approve`, `slopscale users approve`, `slopscale preauthkeys create --preauthorized`, an `Approved` column in `slopscale nodes list` and `slopscale users list`, `GET|POST /api/v1/settings`, `POST /api/v1/node/{id}/approve`, `POST /api/v1/user/{id}/approve`, `approved`/`approvedAt` on nodes and users and `preauthorized` on pre-auth keys; the v2 API's `PATCH /api/v2/tailnet/{tailnet}/settings` now updates `devicesApprovalOn` and `usersApprovalOn` instead of returning 501, `POST /api/v2/device/{id}/authorized` accepts `false`, and `POST /api/v2/users/{id}/approve|suspend|restore` exist
- Security: the v1 API now applies the same tag boundary to OAuth tokens as the v2 API when it creates pre-auth keys, sets a node's tags or registers a node; an invite may only carry a role its creator could assign directly, so an IT admin cannot mint an admin through an invitation; an OAuth client's tokens are bounded by its creator's current role and die with the creator's account; `oidc.admin_users` no longer promotes a login whose email the provider has not verified when `email_verified_required` is on; `match_by_email` no longer lets a second identity at the same provider take over an existing account, and a user's email may not be changed to one another user holds; the OAuth token endpoint is audited; unauthenticated requests are bounded to nothing instead of local trust; the register-confirm CSRF token is compared in constant time; the Discord webhook payload disables mentions
- Security: webhooks, log streams and DERP map URLs may no longer point at loopback or link-local addresses, checked again at dial time so DNS rebinding cannot get past it (`egress.allow_loopback_targets` re-allows loopback for development, `egress.deny_private_targets` blocks RFC1918 ranges too); the DERP fetch refuses redirects, caps the body and checks the status; delivery status and test results report `HTTP <code>`, `unreachable` or `rejected` instead of the raw transport error; unmapped internal errors return a reference id instead of the error text; the audit CSV export neutralises spreadsheet formulas; invite addresses must parse as email; SSH recording uploads are capped by `ssh_recording.max_session_bytes` and refused from unknown nodes; `POST /api/v1/debug/node` is only registered with `debug.node_api_enabled`
- Fix a posture such as `node:os NOT IN ['ios','android']` matching a machine that reported nothing: a missing attribute now fails every check except `NOT SET`, as Tailscale documents
- Fix `expiry` and `lastSeen` in node responses reading `0001-01-01T00:00:00Z` where the schema promises `null`, and `subnetRoutes` meaning different things on the list, the single node and the route approval responses
- A pre-auth key may not carry a tag the policy does not define, and `tailscale up --authkey=<tagged key> --advertise-tags=<its tags>` is accepted instead of refused; sharing a machine with a user still waiting for approval is refused
- Fix expired temporary memberships never being swept when they ran out while the server was down
- The audit log's action filter is always a prefix match, and the rename error no longer repeats itself three times
- Console: approving or deleting a user, saving the policy, deleting or renaming a machine and ending a user's sessions refresh every list they change; "sign out everywhere" on your own user signs the console out; exit routes on the machine page are one row, so rejecting one half of the pair works; the pending route count agrees between the sidebar, the Routes header and its table; the role picker shows role names; the machine page tells you when postures could not be loaded; DNS dialogs no longer open with the previous dialog's error; a new extra record defaults to type A; removing a custom attribute asks first; the QR code is only shown for phones; the global exit node switch is disabled until the machine advertises an exit route; the share picker hides users waiting for approval; rename and invite forms say what is wrong before the server does; long names truncate instead of widening tables; the posture schedule fields and the custom attribute value no longer overflow their dialog; the theme button is icon only on small screens; the Server page names its key expiry row as the config file's value; the audit action filter waits for typing to pause
- Console: states such as Connected, Pending or Failed are a coloured dot and plain text instead of a pill, roles and qualifiers are plain text, and a pill is kept for tags, scopes and other identifiers; a router that stopped advertising a prefix or a route the machine no longer offers is flagged by name, with the reason shown on hover or tap; the pre-auth key list says "Reusable · Ephemeral" in words; the webhook list counts events instead of listing them; the theme button is icon only on small screens; the paging band fits a phone
- Console: every row's actions sit behind one … menu, on SSH recordings, console sessions, nameservers, search domains, relay regions and map URLs, split DNS, DNS rules, extra records and a machine's groups, shares and custom attributes, the way the tables already did; the Disconnected and Used dots are painted again; a table that scrolls sideways can be moved from the keyboard; the pre-auth key list folds the user and options under the key on a phone
- Outbound requests slopscale makes to operator-supplied URLs (webhook receivers, log stream sinks, DERP map URLs) can no longer reach the server's own loopback services, a link-local cloud metadata endpoint or an unspecified address: the URL is checked when it is stored and again against the address it resolves to when it is dialed, so a name that points at a blocked address is refused at connect time. Private ranges stay reachable, because a receiver on the LAN is a normal self-hosted setup; `egress.deny_private_targets` refuses them too and `egress.allow_loopback_targets` re-allows loopback for development
- A DERP map fetched from a URL no longer follows redirects, is read to at most 4 MiB and must answer 2xx, so a source that redirects or answers with an error page leaves the previous map in place
- A webhook or log stream delivery is now reported as `unreachable`, `rejected` or its HTTP status instead of the raw transport error, which named the address the server resolved and dialed; the whole error is in the server log
- The audit log's CSV export prefixes any cell starting with `=`, `+`, `-`, `@`, a tab or a carriage return with an apostrophe, so a node or user name cannot become a formula when the export is opened in a spreadsheet
- An unexpected server error now answers with an id (`internal error, see the server log for id <8 hex chars>`) and logs the error under that id, instead of returning the internal error text; errors the API maps deliberately are unchanged
- An SSH session upload is bounded by `ssh_recording.max_session_bytes` (512 MiB by default); a session that runs past it is kept, marked incomplete, and the upload is refused. An upload from an address that is not a node is refused with 403
- An invite address must be one plain mailbox: a display name, a list or an embedded header is refused
- `POST /api/v1/debug/node` (`slopscale debug create-node`) is off unless `debug.node_api_enabled` is set, and while it is off the operation is not registered and is absent from the OpenAPI document the server serves
- On the v1 API an OAuth access token is now bounded by its tags the way it already was on v2: it may create a pre-auth key or tag a node only with tags it holds or that those tags own, it cannot create an untagged (user-owned) key, and it cannot create a key for a user or register a node into a user's account. Admin API keys are unaffected
- A registration id is recorded in the audit log by its first eight characters only, because the whole id is the secret a node registers with
- Deleting a node now removes the database row before the in-memory copy, and publishes the removal even when the policy refresh that follows it fails, so a committed deletion always tears down the deleted node's live map session [#3410](https://github.com/juanfont/headscale/issues/3410)
- The OIDC registration confirmation page is served from its own URL (`GET /register/confirm/{auth_id}`) instead of being rendered on the callback that carries the single-use authorization code, so a reload, the back button or a browser extension no longer re-enters the spent code exchange and paints an error over it. Its CSRF cookie is `SameSite=Lax`, because Firefox withheld a Strict cookie on the hop out of the identity provider's redirect chain and the page answered 403; the OIDC cookies are scoped to the browser-facing paths derived from `server_url`, so a server behind a reverse proxy path prefix keeps them. Returning to a link that was already used says so instead of showing the generic expired-session error [#3365](https://github.com/juanfont/headscale/issues/3365)
- Extra records read from `dns.extra_records_path` are normalized the way records written inline in the config file already are. Names are matched by clients against the lowercased query name, so a record such as `Printer.fritz.box` in the file never resolved and the query fell through to the global nameserver [#2782](https://github.com/juanfont/headscale/issues/2782)
- Fix HTTP metrics only counting `OPTIONS` requests, so `http_requests_total` and `http_request_duration_seconds` now cover regular traffic [#3414](https://github.com/juanfont/headscale/pull/3414)
- Fix extra-records filewatcher hanging on shutdown after the watched file is deleted, and leaking the watcher when setup fails [#3437](https://github.com/juanfont/headscale/pull/3437)
- Fix `slopscale users rename` sending the raw `--identifier` flag value instead of the matched user's identifier, so renaming by name works again [#3442](https://github.com/juanfont/headscale/pull/3442)
- Fix tailsql not shutting down with slopscale, leaving the process hanging on graceful shutdown [#3400](https://github.com/juanfont/headscale/pull/3400)
- Fix tvOS setup instructions: install the VPN configuration before setting the coordination server URL [#3431](https://github.com/juanfont/headscale/pull/3431)

## 0.29.3 (2026-07-29)

**Minimum supported Tailscale client version: v1.80.0**

### Changes

- Fix tagged node stuck expired after `tailscale logout`, unable to re-authenticate [#3394](https://github.com/juanfont/headscale/pull/3394)
- Re-registering a tagged node with a different pre-auth key now applies the new key's tags instead of silently keeping the old ones [#3394](https://github.com/juanfont/headscale/pull/3394)
- Fix re-authenticating an already-tagged node with `--advertise-tags` being rejected when the authenticating user owns the tags [#3394](https://github.com/juanfont/headscale/pull/3394)
- Fix ephemeral nodes lingering as disconnected after reconnect churn [#3383](https://github.com/juanfont/headscale/pull/3383)
- Fix node registration falsely returning `401 registration timed out` when auth completes as the request context expires [#3392](https://github.com/juanfont/headscale/pull/3392)
- Check the machine key on the followup registration poll so a leaked auth ID cannot return the registering user's identity [#3393](https://github.com/juanfont/headscale/pull/3393)
- Reject `/key` requests below the supported capability version floor, matching `/ts2021` [#3391](https://github.com/juanfont/headscale/pull/3391)
- Remove a leftover trace log that always rendered a JSON marshaling error [#3398](https://github.com/juanfont/headscale/pull/3398)

## 0.29.2 (2026-07-01)

**Minimum supported Tailscale client version: v1.80.0**

### Changes

- Fix map generation serializing on the policy lock, so a mass reconnect on `autogroup:self`, via or relay policies no longer stalls clients into `unexpected EOF` retry loops [#3358](https://github.com/juanfont/headscale/pull/3358)
- Fix `/ts2021` rejecting the WebSocket `GET` upgrade with 405, which prevented Tailscale JS/WASM control clients from connecting [#3359](https://github.com/juanfont/headscale/pull/3359)
- Gracefully handle nodes with an invalid FQDN (empty or too long) instead of failing map delivery; offending names are logged at startup with the fix command [#3349](https://github.com/juanfont/headscale/pull/3349)

## 0.29.1 (2026-06-18)

**Minimum supported Tailscale client version: v1.80.0**

### Changes

- Fix nodes with `tags='null'` losing their assigned user on upgrade [#3325](https://github.com/juanfont/headscale/pull/3325)

## 0.29.0 (2026-06-17)

**Minimum supported Tailscale client version: v1.80.0**

### Tailscale ACL compatibility improvements

Extensive test cases were systematically generated using Tailscale clients and the official SaaS
to understand how the packet filter should be generated. We discovered a few differences, but
overall our implementation was very close.
[#3036](https://github.com/juanfont/headscale/pull/3036)

### SSH check action

SSH rules with `"action": "check"` are now supported. When a client initiates a SSH connection to a node
with a `check` action policy, the user is prompted to authenticate via OIDC or CLI approval before access
is granted. OIDC approval requires the authenticated user to own the source node; tagged source nodes
cannot use SSH check-mode.

A new `slopscale auth` CLI command group supports the approval flow:

- `slopscale auth approve --auth-id <id>` approves a pending authentication request (SSH check or web auth)
- `slopscale auth reject --auth-id <id>` rejects a pending authentication request
- `slopscale auth register --auth-id <id> --user <user>` registers a node (replaces deprecated `slopscale nodes register`)

[#1850](https://github.com/juanfont/headscale/pull/1850)
[#3180](https://github.com/juanfont/headscale/pull/3180)

### Policy tests (beta)

Slopscale now evaluates the `tests` block in a policy file. Tests assert reachability between
named sources and destinations and cover the whole policy — both `acls` and `grants` rules
contribute. They run on user-initiated writes via `slopscale policy set`, on SIGHUP reload
(`systemctl reload slopscale` / `kill -HUP $(pidof slopscale)`), and on `slopscale policy check`.
A failing test rejects the write before it is applied, with the same error message Tailscale SaaS
would return for the same policy.

At boot a stored policy whose tests no longer pass — for example because a referenced user was
deleted while the server was offline — logs a warning and the server keeps running. Fix the
policy and reload.

This feature is **beta** while behavioural coverage against Tailscale SaaS broadens.

[#3229](https://github.com/juanfont/headscale/pull/3229)

### SSH policy tests (beta)

Slopscale now evaluates the `sshTests` block in a policy file. Each entry names a source, one or
more destination hosts, and three optional user lists: `accept` asserts the listed login users
reach every destination via an accept- or check-action SSH rule, `deny` asserts none of them
reach any destination, and `check` requires reachability specifically through a check-action
rule. Tests run on `slopscale policy set`, on SIGHUP reload (`systemctl reload slopscale` /
`kill -HUP $(pidof slopscale)`), and on `slopscale policy check`. A failing test rejects the
write before it is applied, with the same error message Tailscale SaaS would return for the same
policy.

At boot a stored policy whose sshTests no longer pass — for example because a referenced user was
deleted while the server was offline — logs a warning and the server keeps running. Fix the
policy and reload.

This feature is **beta** while behavioural coverage against Tailscale SaaS broadens.

[#3263](https://github.com/juanfont/headscale/pull/3263)

### SSH rule validation

SSH rule parsing now trims surrounding whitespace on `action`, `users`, `src`, and `dst`,
rejects empty or wildcard entries in `users`, rejects empty `acceptEnv`, and rejects negative
`checkPeriod`. `hosts:` aliases are rejected as SSH destinations, non-ASCII tag names are
rejected at parse time, and the wording for group-nesting cycles matches Tailscale SaaS.
[#3263](https://github.com/juanfont/headscale/pull/3263)

### Grants

We now support [Tailscale grants](https://tailscale.com/docs/features/access-control/grants)
alongside ACLs. Grants extend what you can express in a policy beyond packet filtering: the `app`
field controls application-level features like Taildrive file sharing and peer relay, and the `via`
field steers traffic through specific tagged subnet routers or exit nodes. The `ip` field works like
an ACL rule. Grants can be mixed with ACLs in the same policy file.
[#2180](https://github.com/juanfont/headscale/pull/2180)

As part of this, we added `autogroup:danger-all`. It resolves to `0.0.0.0/0` and `::/0`, all IP
addresses, including those outside the tailnet. This replaces the old behaviour where `*` matched
all IPs (see BREAKING below). The name is intentional: accepting traffic from the entire
internet is a security-sensitive choice. `autogroup:danger-all` can only be used as a source.

### Node attributes (`nodeAttrs`)

ACL policies now accept a `nodeAttrs` block. Each entry hands a list of
Tailscale node capabilities to every node matching `target`. The accepted
target forms are the same as `acls.src` and `grants.src`: users, groups,
tags, hosts, prefixes, `autogroup:member`, `autogroup:tagged`, and `*`.

```jsonc
{
  "randomizeClientPort": true,
  "nodeAttrs": [
    {
      "target": ["autogroup:tagged"],
      "attr": ["disable-captive-portal-detection"],
    },
    { "target": ["alice@example.com"], "attr": ["nextdns:abc123"] },
  ],
}
```

Frequently requested capabilities this unlocks include `magicdns-aaaa`,
`disable-relay-server`, `disable-captive-portal-detection`,
`nextdns:<profile>` / `nextdns:no-device-info`, `randomize-client-port`,
and the Taildrive `drive:share` / `drive:access` pair. The set is not
limited to these, any string-only cap an operator places in policy
reaches clients unchanged.

`randomizeClientPort` also lands as a top-level policy field that toggles
the default for every node, replacing the old server-config knob.

A new `auto_update.enabled` config option controls the tailnet-wide
default for client auto-update. When true, every node's CapMap carries
`default-auto-update: [true]` so fresh clients pick up the default
unless they make a local opt-in / opt-out choice.

Policies that use the `funnel` cap, `ipPool` blocks, or
`autogroup:admin` / `autogroup:owner` targets are rejected at load —
those features depend on machinery slopscale does not yet ship.

[#3251](https://github.com/juanfont/headscale/pull/3251)

### Taildrive

Taildrive ([file-sync between
nodes](https://tailscale.com/docs/features/taildrive)) is now
configurable through policy. Grant `drive:share` to the node that
hosts files and `drive:access` to nodes that read or write them; pair
with a `tailscale.com/cap/drive` grant to set the per-share access
mode:

```jsonc
{
  "nodeAttrs": [
    { "target": ["tag:fileserver"], "attr": ["drive:share"] },
    { "target": ["autogroup:member"], "attr": ["drive:access"] },
  ],
  "grants": [
    {
      "src": ["autogroup:member"],
      "dst": ["tag:fileserver"],
      "app": {
        "tailscale.com/cap/drive": [{ "shares": ["*"], "access": "rw" }],
      },
    },
  ],
}
```

A wildcard `nodeAttrs` (`"target": ["*"]`) hands the caps to every
node when fine-grained control is not needed.

### Hostname sanitisation

Hostnames are now santised using Tailscales `magicdns` sanitisation rules, matching Tailscale SaaS behavior. This means that hostnames with non-ASCII characters, special characters, or reserved DNS label characters are now transformed into valid DNS labels for MagicDNS. This improves our previously too strict sanitisation that rejected hostnames based on our guesswork and not based on the Tailscale upstream behaviour.

Examples that previously regressed and now work:

| Input                | Raw (Hostname)       | DNS label (GivenName) |
| -------------------- | -------------------- | --------------------- |
| `Joe's Mac mini`     | `Joe's Mac mini`     | `joes-mac-mini`       |
| `Yuri's MacBook Pro` | `Yuri's MacBook Pro` | `yuris-macbook-pro`   |
| `Test@Host`          | `Test@Host`          | `test-host`           |
| `mail.server`        | `mail.server`        | `mail-server`         |
| `My-PC!`             | `My-PC!`             | `my-pc`               |
| `我的电脑`           | `我的电脑`           | `node`                |

[#3202](https://github.com/juanfont/headscale/pull/3202)

### HA subnet router health probing

Slopscale now actively probes HA subnet routers to detect nodes that are connected but not
forwarding traffic. The control plane periodically pings HA subnet routers via the Noise
control channel and fails over to a healthy standby if the primary stops responding. This is
enabled by default (`node.routes.ha.probe_interval: 10s`, `probe_timeout: 5s`) and only
active when HA routes exist (2+ nodes advertising the same prefix). Set `probe_interval` to
`0` to disable. This complements the existing disconnect-based failover, catching "zombie
connected" routers that maintain their control session but cannot route packets.
[#3194](https://github.com/juanfont/headscale/pull/3194)

### BREAKING

#### Hostname handling

- The `GivenName` collision policy changed from an 8-char random hash suffix (`laptop-abc12xyz`) to a monotonic numeric suffix (`laptop`, `laptop-1`, `laptop-2`, …), matching Tailscale SaaS. Empty / all-non-ASCII hostnames now fall back to the literal `node` instead of `invalid-<rand>`. MagicDNS names change on upgrade for any node whose previous label was a random-suffix form; the raw `Hostname` column is unchanged. [#3202](https://github.com/juanfont/headscale/pull/3202)

#### ACL Policy

- Wildcard (`*`) in ACL sources and destinations now resolves to Tailscale's CGNAT range (`100.64.0.0/10`) and ULA range (`fd7a:115c:a1e0::/48`) instead of all IPs (`0.0.0.0/0` and `::/0`) [#3036](https://github.com/juanfont/headscale/pull/3036)
  - This better matches Tailscale's security model where `*` means "any node in the tailnet" rather than "any IP address"
  - Policies that need to match all IP addresses including non-Tailscale IPs should use `autogroup:danger-all` as a source, or explicit CIDR ranges as destinations [#2180](https://github.com/juanfont/headscale/pull/2180)
  - `autogroup:danger-all` can only be used as a source; it cannot be used as a destination
  - **Note**: Users with non-standard IP ranges configured in `prefixes.ipv4` or `prefixes.ipv6` (which is unsupported and produces a warning) will need to explicitly specify their CIDR ranges in ACL rules instead of using `*`
- Validate `autogroup:self` source restrictions matching Tailscale behavior - tags, hosts, and IPs are rejected as sources for `autogroup:self` destinations [#3036](https://github.com/juanfont/headscale/pull/3036)
  - Policies using tags, hosts, or IP addresses as sources for `autogroup:self` destinations will now fail validation
- The `proto:icmp` protocol name now only includes ICMPv4 (protocol 1), matching Tailscale behavior [#3036](https://github.com/juanfont/headscale/pull/3036)
  - Previously, `proto:icmp` included both ICMPv4 and ICMPv6
  - Use `proto:ipv6-icmp` or protocol number `58` explicitly for ICMPv6

#### Upgrade Path

- Slopscale now enforces a strict version upgrade path [#3083](https://github.com/juanfont/headscale/pull/3083)
  - Skipping minor versions (e.g. 0.27 → 0.29) is blocked; upgrade one minor version at a time
  - Downgrading to a previous minor version is blocked
  - Patch version changes within the same minor are always allowed

#### Configuration

- The `randomize_client_port` server-config key was removed; the
  toggle now lives in the policy file as a top-level
  `randomizeClientPort` field, matching the Tailscale-hosted schema. [#3251](https://github.com/juanfont/headscale/pull/3251)
  Slopscale refuses to start when the old key is set. Move it to the
  policy file referenced by `policy.path`:

  ```jsonc
  {
    "randomizeClientPort": true,
  }
  ```

  If you do not have a policy file yet, create one with that minimal
  content and point `policy.path` at it. The default carries over —
  empty / absent policy means `randomizeClientPort: false`, matching
  the previous behaviour for operators who never set the key. Per-node
  opt-in via `nodeAttrs` is also supported and stacks on top of the
  global default.

#### CLI

- `slopscale nodes register` is deprecated in favour of `slopscale auth register --auth-id <id> --user <user>` [#1850](https://github.com/juanfont/headscale/pull/1850)
  - The old command continues to work but will be removed in a future release

### Changes

#### ACL Policy

- Fix subnet-to-subnet peer visibility — subnet routers now correctly become peers when ACL rules reference only subnet CIDRs as sources, without requiring node IP rules [#3175](https://github.com/juanfont/headscale/pull/3175)
- Fix filter rule reduction to use only approved subnet routes instead of all advertised routes, matching Tailscale SaaS behavior [#3175](https://github.com/juanfont/headscale/pull/3175)
- Add ICMP and IPv6-ICMP protocols to default filter rules when no protocol is specified [#3036](https://github.com/juanfont/headscale/pull/3036)
- Fix autogroup:self handling for tagged nodes - tagged nodes no longer incorrectly receive autogroup:self filter rules [#3036](https://github.com/juanfont/headscale/pull/3036)
- Use CIDR format for autogroup:self destination IPs matching Tailscale behavior [#3036](https://github.com/juanfont/headscale/pull/3036)
- Merge filter rules with identical SrcIPs and IPProto matching Tailscale behavior - multiple ACL rules with the same source now produce a single FilterRule with combined DstPorts [#3036](https://github.com/juanfont/headscale/pull/3036)
- Fix exit nodes incorrectly receiving filter rules for destinations that only overlap via exit routes [#3169](https://github.com/juanfont/headscale/issues/3169) [#3175](https://github.com/juanfont/headscale/pull/3175)
- Fix address-based aliases (hosts, raw IPs) incorrectly expanding to include the matching node's other address family [#2180](https://github.com/juanfont/headscale/pull/2180)
- Fix identity-based aliases (tags, users, groups) resolving to IPv4 only; they now include both IPv4 and IPv6 matching Tailscale behavior [#2180](https://github.com/juanfont/headscale/pull/2180)
- Fix wildcard (`*`) source in ACLs now using actually-approved subnet routes instead of autoApprover policy prefixes [#2180](https://github.com/juanfont/headscale/pull/2180)
- Fix non-wildcard source IPs being dropped when combined with wildcard `*` in the same ACL rule [#2180](https://github.com/juanfont/headscale/pull/2180)
- Fix exit node approval not triggering filter rule recalculation for peers [#2180](https://github.com/juanfont/headscale/pull/2180)
- Policy validation error messages now include field context (e.g., `src=`, `dst=`) and are more descriptive [#2180](https://github.com/juanfont/headscale/pull/2180)
- Reject policies whose `user@` tokens match multiple DB users; rename the duplicate via `slopscale users rename` to load [#3160](https://github.com/juanfont/headscale/issues/3160)
- Evaluate the policy `tests` block on user-initiated writes across both `acls` and `grants`; reject policies whose tests fail (beta) [#1803](https://github.com/juanfont/headscale/issues/1803)

#### Grants

- Add support for policy grants with `ip`, `app`, and `via` fields [#2180](https://github.com/juanfont/headscale/pull/2180)
- Add `autogroup:danger-all` as a source-only autogroup resolving to all IP addresses [#2180](https://github.com/juanfont/headscale/pull/2180)
- Add capability grants for Taildrive (`cap/drive`) and peer relay (`cap/relay`) with automatic companion capabilities [#2180](https://github.com/juanfont/headscale/pull/2180)
- Add per-viewer via route steering — grants with `via` tags control which subnet router or exit node handles traffic for each group of viewers [#2180](https://github.com/juanfont/headscale/pull/2180)
- Enable Taildrive node attributes on all nodes; actual access is controlled by `cap/drive` grants [#2180](https://github.com/juanfont/headscale/pull/2180)

#### SSH Policy

- Add support for `localpart:*@<domain>` in SSH rule `users` field, mapping each matching user's email local-part as their OS username [#3091](https://github.com/juanfont/headscale/pull/3091)
- Add SSH `check` action support with OIDC and CLI-based approval flows [#1850](https://github.com/juanfont/headscale/pull/1850)

#### CLI

- Add `slopscale auth register`, `slopscale auth approve`, and `slopscale auth reject` CLI commands [#1850](https://github.com/juanfont/headscale/pull/1850)
- Deprecate `slopscale nodes register --key` in favour of `slopscale auth register --auth-id` [#1850](https://github.com/juanfont/headscale/pull/1850)
- `slopscale policy check --bypass-grpc-and-access-database-directly` validates `user@` tokens against the live user database [#3160](https://github.com/juanfont/headscale/issues/3160)
- Remove deprecated `--namespace` flag from `nodes list`, `nodes register`, and `debug create-node` commands (use `--user` instead) [#3093](https://github.com/juanfont/headscale/pull/3093)
- Remove deprecated `namespace`/`ns` command aliases for `users` and `machine`/`machines` aliases for `nodes` [#3093](https://github.com/juanfont/headscale/pull/3093)
- Fix `DestroyUser` deleting all pre-auth keys in the database instead of only the target user's keys [#3155](https://github.com/juanfont/headscale/pull/3155)
- `slopscale policy check` evaluates the `tests` block when invoked with `--bypass-grpc-and-access-database-directly`; without the flag it warns instead of running the tests against empty data [#1803](https://github.com/juanfont/headscale/issues/1803)

#### API

- Add `auth` related routes. The `auth/register` endpoint now expects data as JSON [#1850](https://github.com/juanfont/headscale/pull/1850)
- Remove gRPC reflection from the remote (TCP) server [#3180](https://github.com/juanfont/headscale/pull/3180)

#### OIDC

- Add a confirmation page before completing node registration, showing the device hostname and machine key fingerprint [#3180](https://github.com/juanfont/headscale/pull/3180)
- Generalise auth templates into reusable `AuthSuccess` and `AuthWeb` components [#1850](https://github.com/juanfont/headscale/pull/1850)
- Unify auth pipeline with `AuthVerdict` type, supporting registration, reauthentication, and SSH checks [#1850](https://github.com/juanfont/headscale/pull/1850)

#### Configuration

- Add `node.expiry` configuration option to set a default node key expiry for nodes registered via auth key [#3122](https://github.com/juanfont/headscale/pull/3122)
  - Tagged nodes (registered with tagged pre-auth keys) are exempt from default expiry
  - `oidc.expiry` has been removed; use `node.expiry` instead (applies to all registration methods including OIDC)
  - `ephemeral_node_inactivity_timeout` is deprecated in favour of `node.ephemeral.inactivity_timeout`
- Add `trusted_proxies` to gate `True-Client-IP` / `X-Real-IP` / `X-Forwarded-For` (previously honoured from any client) [#3268](https://github.com/juanfont/headscale/pull/3268)

#### Debug

- Add node connectivity ping page for verifying control-plane reachability [#3183](https://github.com/juanfont/headscale/pull/3183)
- Omit secret fields (`Pass`, `ClientSecret`, `APIKey`) from `/debug/config` JSON output [#3180](https://github.com/juanfont/headscale/pull/3180)
- Route `statsviz` through `tsweb.Protected` [#3180](https://github.com/juanfont/headscale/pull/3180)

#### Other

- Remove old migrations for the debian package [#3185](https://github.com/juanfont/headscale/pull/3185)
- Install `config-example.yaml` as example for the debian package [#3186](https://github.com/juanfont/headscale/pull/3186)
- Fix user-owned re-registration with zero client expiry and no default storing `0001-01-01 00:00:00` in the database instead of `NULL` [#3199](https://github.com/juanfont/headscale/pull/3199)
- Fix `tailscaled` restart on a node with no expiry resetting `NULL` to `0001-01-01 00:00:00` in the database, affecting both tagged and untagged nodes [#3197](https://github.com/juanfont/headscale/pull/3197)
- Backfill `nodes.expiry` rows persisted by older versions as `0001-01-01 00:00:00` to `NULL`, so nodes upgraded from \<0.28 stop reporting as expired [#3284](https://github.com/juanfont/headscale/issues/3284)
- Update reverse proxy documentation for `trusted_proxies` configuration option [#3292](https://github.com/juanfont/headscale/pull/3292)

## 0.28.0 (2026-02-04)

**Minimum supported Tailscale client version: v1.74.0**

### Tags as identity

Tags are now implemented following the Tailscale model where tags and user ownership are mutually exclusive. Devices can be either
user-owned (authenticated via web/OIDC) or tagged (authenticated via tagged PreAuthKeys). Tagged devices receive their identity from
tags rather than users, making them suitable for servers and infrastructure. Applying a tag to a device removes user-based
ownership. See the [Tailscale tags documentation](https://tailscale.com/docs/features/tags) for details on how tags work.

User-owned nodes can now request tags during registration using `--advertise-tags`. Tags are validated against the `tagOwners` policy
and applied at registration time. Tags can be managed via the CLI or API after registration. Tagged nodes can return to user-owned
by re-authenticating with `tailscale up --advertise-tags= --force-reauth`.

A one-time migration will validate and migrate any `RequestTags` (stored in hostinfo) to the tags column. Tags are validated against
your policy's `tagOwners` rules during migration. [#3011](https://github.com/juanfont/headscale/pull/3011)

### Smarter map updates

The map update system has been rewritten to send smaller, partial updates instead of full network maps whenever possible. This reduces bandwidth usage and improves performance, especially for large networks. The system now properly tracks peer
changes and can send removal notifications when nodes are removed due to policy changes.
[#2856](https://github.com/juanfont/headscale/pull/2856) [#2961](https://github.com/juanfont/headscale/pull/2961)

### Pre-authentication key security improvements

Pre-authentication keys now use bcrypt hashing for improved security [#2853](https://github.com/juanfont/headscale/pull/2853). Keys
are stored as a prefix and bcrypt hash instead of plaintext. The full key is only displayed once at creation time. When listing keys,
only the prefix is shown (e.g., `hskey-auth-{prefix}-***`). All new keys use the format `hskey-auth-{prefix}-{secret}`. Legacy plaintext keys in the format `{secret}` will continue to work for backwards compatibility.

### Web registration templates redesign

The OIDC callback and device registration web pages have been updated to use the Material for MkDocs design system from the official
documentation. The templates now use consistent typography, spacing, and colours across all registration flows.

### Database migration support removed for pre-0.25.0 databases

Slopscale no longer supports direct upgrades from databases created before version 0.25.0. Users on older versions must upgrade
sequentially through each stable release, selecting the latest patch version available for each minor release.

### BREAKING

- **API**: The Node message in the gRPC/REST API has been simplified - the `ForcedTags`, `InvalidTags`, and `ValidTags` fields have been removed and replaced with a single `Tags` field that contains the node's applied tags [#2993](https://github.com/juanfont/headscale/pull/2993)

  - API clients should use the `Tags` field instead of `ValidTags`
  - The `slopscale nodes list` CLI command now always shows a Tags column and the `--tags` flag has been removed

- **PreAuthKey CLI**: Commands now use ID-based operations instead of user+key combinations [#2992](https://github.com/juanfont/headscale/pull/2992)

  - `slopscale preauthkeys create` no longer requires `--user` flag (optional for tracking creation)
  - `slopscale preauthkeys list` lists all keys (no longer filtered by user)
  - `slopscale preauthkeys expire --id <ID>` replaces `--user <USER> <KEY>`
  - `slopscale preauthkeys delete --id <ID>` replaces `--user <USER> <KEY>`

  **Before:**

  ```bash
  slopscale preauthkeys create --user 1 --reusable --tags tag:server
  slopscale preauthkeys list --user 1
  slopscale preauthkeys expire --user 1 <KEY>
  slopscale preauthkeys delete --user 1 <KEY>
  ```

  **After:**

  ```bash
  slopscale preauthkeys create --reusable --tags tag:server
  slopscale preauthkeys list
  slopscale preauthkeys expire --id 123
  slopscale preauthkeys delete --id 123
  ```

- **Tags**: The gRPC `SetTags` endpoint now allows converting user-owned nodes to tagged nodes by setting tags. [#2885](https://github.com/juanfont/headscale/pull/2885)

- **Tags**: Tags are now resolved from the node's stored Tags field only [#2931](https://github.com/juanfont/headscale/pull/2931)

  - `--advertise-tags` is processed during registration, not on every policy evaluation
  - PreAuthKey tagged devices ignore `--advertise-tags` from clients
  - User-owned nodes can use `--advertise-tags` if authorized by `tagOwners` policy
  - Tags can be managed via CLI (`slopscale nodes tag`) or the SetTags API after registration

- Database migration support removed for pre-0.25.0 databases [#2883](https://github.com/juanfont/headscale/pull/2883)

  - If you are running a version older than 0.25.0, you must upgrade to 0.25.1 first, then upgrade to this release
  - See the [upgrade path documentation](https://aislopware.github.io/slopscale/about/faq/#what-is-the-recommended-update-path-can-i-skip-multiple-versions-while-updating) for detailed guidance
  - In version 0.29, all migrations before 0.28.0 will also be removed

- Remove ability to move nodes between users [#2922](https://github.com/juanfont/headscale/pull/2922)

  - The `slopscale nodes move` CLI command has been removed
  - The `MoveNode` API endpoint has been removed
  - Nodes are permanently associated with their user or tag at registration time

- Add `oidc.email_verified_required` config option to control email verification requirement [#2860](https://github.com/juanfont/headscale/pull/2860)

  - When `true` (default), only verified emails can authenticate via OIDC in conjunction with `oidc.allowed_domains` or
    `oidc.allowed_users`. Previous versions allowed to authenticate with an unverified email but did not store the email
    address in the user profile. This is now rejected during authentication with an `unverified email` error.
  - When `false`, unverified emails are allowed for OIDC authentication and the email address is stored in the user
    profile regardless of its verification state.

- **SSH Policy**: Wildcard (`*`) is no longer supported as an SSH destination [#3009](https://github.com/juanfont/headscale/issues/3009)

  - Use `autogroup:member` for user-owned devices
  - Use `autogroup:tagged` for tagged devices
  - Use specific tags (e.g., `tag:server`) for targeted access

  **Before:**

  ```json
  {
    "action": "accept",
    "src": ["group:admins"],
    "dst": ["*"],
    "users": ["root"]
  }
  ```

  **After:**

  ```json
  {
    "action": "accept",
    "src": ["group:admins"],
    "dst": ["autogroup:member", "autogroup:tagged"],
    "users": ["root"]
  }
  ```

- **SSH Policy**: SSH source/destination validation now enforces Tailscale's security model [#3010](https://github.com/juanfont/headscale/issues/3010)

  Per [Tailscale SSH documentation](https://tailscale.com/docs/features/tailscale-ssh), the following rules are now enforced:

  1. **Tags cannot SSH to user-owned devices**: SSH rules with `tag:*` or `autogroup:tagged` as source cannot have username destinations (e.g., `alice@`) or `autogroup:member`/`autogroup:self` as destination
  1. **Username destinations require same-user source**: If destination is a specific username (e.g., `alice@`), the source must be that exact same user only. Use `autogroup:self` for same-user SSH access instead

  **Invalid policies now rejected at load time:**

  ```json
  // INVALID: tag source to user destination
  {"src": ["tag:server"], "dst": ["alice@"], ...}

  // INVALID: autogroup:tagged to autogroup:member
  {"src": ["autogroup:tagged"], "dst": ["autogroup:member"], ...}

  // INVALID: group to specific user (use autogroup:self instead)
  {"src": ["group:admins"], "dst": ["alice@"], ...}
  ```

  **Valid patterns:**

  ```json
  // Users/groups can SSH to their own devices via autogroup:self
  {"src": ["group:admins"], "dst": ["autogroup:self"], ...}

  // Users/groups can SSH to tagged devices
  {"src": ["group:admins"], "dst": ["autogroup:tagged"], ...}

  // Tagged devices can SSH to other tagged devices
  {"src": ["autogroup:tagged"], "dst": ["autogroup:tagged"], ...}

  // Same user can SSH to their own devices
  {"src": ["alice@"], "dst": ["alice@"], ...}
  ```

### Changes

- Smarter change notifications send partial map updates and node removals instead of full maps [#2961](https://github.com/juanfont/headscale/pull/2961)
  - Send lightweight endpoint and DERP region updates instead of full maps [#2856](https://github.com/juanfont/headscale/pull/2856)
- Add NixOS module in repository for faster iteration [#2857](https://github.com/juanfont/headscale/pull/2857)
- Add favicon to webpages [#2858](https://github.com/juanfont/headscale/pull/2858)
- Redesign OIDC callback and registration web templates [#2832](https://github.com/juanfont/headscale/pull/2832)
- Reclaim IPs from the IP allocator when nodes are deleted [#2831](https://github.com/juanfont/headscale/pull/2831)
- Add bcrypt hashing for pre-authentication keys [#2853](https://github.com/juanfont/headscale/pull/2853)
- Add prefix to API keys (`hskey-api-{prefix}-{secret}`) [#2853](https://github.com/juanfont/headscale/pull/2853)
- Add prefix to registration keys for web authentication tracking (`hskey-reg-{random}`) [#2853](https://github.com/juanfont/headscale/pull/2853)
- Tags can now be tagOwner of other tags [#2930](https://github.com/juanfont/headscale/pull/2930)
- Add `taildrop.enabled` configuration option to enable/disable Taildrop file sharing [#2955](https://github.com/juanfont/headscale/pull/2955)
- Allow disabling the metrics server by setting empty `metrics_listen_addr` [#2914](https://github.com/juanfont/headscale/pull/2914)
- Log ACME/autocert errors for easier debugging [#2933](https://github.com/juanfont/headscale/pull/2933)
- Improve CLI list output formatting [#2951](https://github.com/juanfont/headscale/pull/2951)
- Use Debian 13 distroless base images for containers [#2944](https://github.com/juanfont/headscale/pull/2944)
- Fix ACL policy not applied to new OIDC nodes until client restart [#2890](https://github.com/juanfont/headscale/pull/2890)
- Fix autogroup:self preventing visibility of nodes matched by other ACL rules [#2882](https://github.com/juanfont/headscale/pull/2882)
- Fix nodes being rejected after pre-authentication key expiration [#2917](https://github.com/juanfont/headscale/pull/2917)
- Fix list-routes command respecting identifier filter with JSON output [#2927](https://github.com/juanfont/headscale/pull/2927)
- Add `--id` flag to expire/delete commands as alternative to `--prefix` for API Keys [#3016](https://github.com/juanfont/headscale/pull/3016)

## 0.27.1 (2025-11-11)

**Minimum supported Tailscale client version: v1.64.0**

### Changes

- Expire nodes with a custom timestamp [#2828](https://github.com/juanfont/headscale/pull/2828)
- Fix issue where node expiry was reset when tailscaled restarts [#2875](https://github.com/juanfont/headscale/pull/2875)
- Fix OIDC authentication when multiple login URLs are opened [#2861](https://github.com/juanfont/headscale/pull/2861)
- Fix node re-registration failing with expired auth keys [#2859](https://github.com/juanfont/headscale/pull/2859)
- Remove old unused database tables and indices [#2844](https://github.com/juanfont/headscale/pull/2844) [#2872](https://github.com/juanfont/headscale/pull/2872)
- Ignore litestream tables during database validation [#2843](https://github.com/juanfont/headscale/pull/2843)
- Fix exit node visibility to respect ACL rules [#2855](https://github.com/juanfont/headscale/pull/2855)
- Fix SSH policy becoming empty when unknown user is referenced [#2874](https://github.com/juanfont/headscale/pull/2874)
- Fix policy validation when using bypass-grpc mode [#2854](https://github.com/juanfont/headscale/pull/2854)
- Fix autogroup:self interaction with other ACL rules [#2842](https://github.com/juanfont/headscale/pull/2842)
- Fix flaky DERP map shuffle test [#2848](https://github.com/juanfont/headscale/pull/2848)
- Use current stable base images for Debian and Alpine containers [#2827](https://github.com/juanfont/headscale/pull/2827)

## 0.27.0 (2025-10-27)

**Minimum supported Tailscale client version: v1.64.0**

### Database integrity improvements

This release includes a significant database migration that addresses
longstanding issues with the database schema and data integrity that has
accumulated over the years. The migration introduces a `schema.sql` file as the
source of truth for the expected database schema to ensure new migrations that
will cause divergence does not occur again.

These issues arose from a combination of factors discovered over time: SQLite
foreign keys not being enforced for many early versions, all migrations being
run in one large function until version 0.23.0, and inconsistent use of GORM's
AutoMigrate feature. Moving forward, all new migrations will be explicit SQL
operations rather than relying on GORM AutoMigrate, and foreign keys will be
enforced throughout the migration process.

We are only improving SQLite databases with this change - PostgreSQL databases
are not affected.

Please read the
[PR description](https://github.com/juanfont/headscale/pull/2617) for more
technical details about the issues and solutions.

**SQLite Database Backup Example:**

```bash
# Stop slopscale
systemctl stop slopscale

# Backup sqlite database
cp /var/lib/slopscale/db.sqlite /var/lib/slopscale/db.sqlite.backup

# Backup sqlite WAL/SHM files (if they exist)
cp /var/lib/slopscale/db.sqlite-wal /var/lib/slopscale/db.sqlite-wal.backup
cp /var/lib/slopscale/db.sqlite-shm /var/lib/slopscale/db.sqlite-shm.backup

# Start slopscale (migration will run automatically)
systemctl start slopscale
```

### DERPMap update frequency

The default DERPMap update frequency has been changed from 24 hours to 3 hours.
If you set the `derp.update_frequency` configuration option, it is recommended
to change it to `3h` to ensure that the slopscale instance gets the latest
DERPMap updates when upstream is changed.

### Autogroups

This release adds support for the three missing autogroups: `self`
(experimental), `member`, and `tagged`. Please refer to the
[documentation](https://tailscale.com/docs/reference/targets-and-selectors#autogroups)
for a detailed explanation.

`autogroup:self` is marked as experimental and should be used with caution, but
we need help testing it. Experimental here means two things; first, generating
the packet filter from policies that use `autogroup:self` is very expensive, and
it might perform, or straight up not work on Slopscale installations with a
large number of nodes. Second, the implementation might have bugs or edge cases
we are not aware of, meaning that nodes or users might gain _more_ access than
expected. Please report bugs.

### Node store (in memory database)

Under the hood, we have added a new datastructure to store nodes in memory. This
datastructure is called `NodeStore` and aims to reduce the reading and writing
of nodes to the database layer. We have not benchmarked it, but expect it to
improve performance for read heavy workloads. We think of it as, "worst case" we
have moved the bottle neck somewhere else, and "best case" we should see a good
improvement in compute resource usage at the expense of memory usage. We are
quite excited for this change and think it will make it easier for us to improve
the code base over time and make it more correct and efficient.

### BREAKING

- Remove support for 32-bit binaries [#2692](https://github.com/juanfont/headscale/pull/2692)
- Policy: Zero or empty destination port is no longer allowed [#2606](https://github.com/juanfont/headscale/pull/2606)
- Stricter hostname validation [#2383](https://github.com/juanfont/headscale/pull/2383)
  - Hostnames must be valid DNS labels (2-63 characters, alphanumeric and
    hyphens only, cannot start/end with hyphen)
  - **Client Registration (New Nodes)**: Invalid hostnames are automatically
    renamed to `invalid-XXXXXX` format
    - `my-laptop` → accepted as-is
    - `My-Laptop` → `my-laptop` (lowercased)
    - `my_laptop` → `invalid-a1b2c3` (underscore not allowed)
    - `test@host` → `invalid-d4e5f6` (@ not allowed)
    - `laptop-🚀` → `invalid-j1k2l3` (emoji not allowed)
  - **Hostinfo Updates / CLI**: Invalid hostnames are rejected with an error
    - Valid names are accepted or lowercased
    - Names with invalid characters, too short (\<2), too long (>63), or
      starting/ending with hyphen are rejected

### Changes

- **Database schema migration improvements for SQLite** [#2617](https://github.com/juanfont/headscale/pull/2617)
  - **IMPORTANT: Backup your SQLite database before upgrading**
  - Introduces safer table renaming migration strategy
  - Addresses longstanding database integrity issues
- Add flag to directly manipulate the policy in the database [#2765](https://github.com/juanfont/headscale/pull/2765)
- DERPmap update frequency default changed from 24h to 3h [#2741](https://github.com/juanfont/headscale/pull/2741)
- DERPmap update mechanism has been improved with retry, and is now failing
  conservatively, preserving the old map upon failure.
  [#2741](https://github.com/juanfont/headscale/pull/2741)
- Add support for `autogroup:member`, `autogroup:tagged` [#2572](https://github.com/juanfont/headscale/pull/2572)
- Fix bug where return routes were being removed by policy [#2767](https://github.com/juanfont/headscale/pull/2767)
- Remove policy v1 code [#2600](https://github.com/juanfont/headscale/pull/2600)
- Refactor Debian/Ubuntu packaging and drop support for Ubuntu 20.04. [#2614](https://github.com/juanfont/headscale/pull/2614)
- Remove redundant check regarding `noise` config [#2658](https://github.com/juanfont/headscale/pull/2658)
- Refactor OpenID Connect documentation [#2625](https://github.com/juanfont/headscale/pull/2625)
- Don't crash if config file is missing [#2656](https://github.com/juanfont/headscale/pull/2656)
- Adds `/robots.txt` endpoint to avoid crawlers [#2643](https://github.com/juanfont/headscale/pull/2643)
- OIDC: Use group claim from UserInfo [#2663](https://github.com/juanfont/headscale/pull/2663)
- OIDC: Update user with claims from UserInfo _before_ comparing with allowed
  groups, email and domain
  [#2663](https://github.com/juanfont/headscale/pull/2663)
- Policy will now reject invalid fields, making it easier to spot spelling
  errors [#2764](https://github.com/juanfont/headscale/pull/2764)
- Add FAQ entry on how to recover from an invalid policy in the database [#2776](https://github.com/juanfont/headscale/pull/2776)
- EXPERIMENTAL: Add support for `autogroup:self` [#2789](https://github.com/juanfont/headscale/pull/2789)
- Add healthcheck command [#2659](https://github.com/juanfont/headscale/pull/2659)

## 0.26.1 (2025-06-06)

### Changes

- Ensure nodes are matching both node key and machine key when connecting. [#2642](https://github.com/juanfont/headscale/pull/2642)

## 0.26.0 (2025-05-14)

### BREAKING

#### Routes

Route internals have been rewritten, removing the dedicated route table in the
database. This was done to simplify the codebase, which had grown unnecessarily
complex after the routes were split into separate tables. The overhead of having
to go via the database and keeping the state in sync made the code very hard to
reason about and prone to errors. The majority of the route state is only
relevant when slopscale is running, and is now only kept in memory. As part of
this, the CLI and API has been simplified to reflect the changes;

```console
$ slopscale nodes list-routes
ID | Hostname           | Approved | Available       | Serving (Primary)
1  | ts-head-ruqsg8     |          | 0.0.0.0/0, ::/0 |
2  | ts-unstable-fq7ob4 |          | 0.0.0.0/0, ::/0 |

$ slopscale nodes approve-routes --identifier 1 --routes 0.0.0.0/0,::/0
Node updated

$ slopscale nodes list-routes
ID | Hostname           | Approved        | Available       | Serving (Primary)
1  | ts-head-ruqsg8     | 0.0.0.0/0, ::/0 | 0.0.0.0/0, ::/0 | 0.0.0.0/0, ::/0
2  | ts-unstable-fq7ob4 |                 | 0.0.0.0/0, ::/0 |
```

Note that if an exit route is approved (0.0.0.0/0 or ::/0), both IPv4 and IPv6
will be approved.

- Route API and CLI has been removed [#2422](https://github.com/juanfont/headscale/pull/2422)
- Routes are now managed via the Node API [#2422](https://github.com/juanfont/headscale/pull/2422)
- Only routes accessible to the node will be sent to the node [#2561](https://github.com/juanfont/headscale/pull/2561)

#### Policy v2

This release introduces a new policy implementation. The new policy is a
complete rewrite, and it introduces some significant quality and consistency
improvements. In principle, there are not really any new features, but some long
standing bugs should have been resolved, or be easier to fix in the future. The
new policy code passes all of our tests.

**Changes**

- The policy is validated and "resolved" when loading, providing errors for
  invalid rules and conditions.
  - Previously this was done as a mix between load and runtime (when it was
    applied to a node).
  - This means that when you convert the first time, what was previously a
    policy that loaded, but failed at runtime, will now fail at load time.
- Error messages should be more descriptive and informative.
  - There is still work to be here, but it is already improved with "typing"
    (e.g. only Users can be put in Groups)
- All users in the policy must contain an `@` character.
  - If your user naturally contains and `@`, like an email, this will just work.
  - If its based on usernames, or other identifiers not containing an `@`, an
    `@` should be appended at the end. For example, if your user is `john`, it
    must be written as `john@` in the policy.

<details>

<summary>Migration notes when the policy is stored in the database.</summary>

This section **only** applies if the policy is stored in the database and
Slopscale 0.26 doesn't start due to a policy error
(`failed to load ACL policy`).

- Start Slopscale 0.26 with the environment variable `SLOPSCALE_POLICY_V1=1`
  set. You can check that Slopscale picked up the environment variable by
  observing this message during startup: `Using policy manager version: 1`
- Dump the policy to a file: `slopscale policy get > policy.json`
- Edit `policy.json` and migrate to policy V2. Use the command
  `slopscale policy check --file policy.json` to check for policy errors.
- Load the modified policy: `slopscale policy set --file policy.json`
- Restart Slopscale **without** the environment variable `SLOPSCALE_POLICY_V1`.
  Slopscale should now print the message `Using policy manager version: 2` and
  startup successfully.

</details>

**SSH**

The SSH policy has been reworked to be more consistent with the rest of the
policy. In addition, several inconsistencies between our implementation and
Tailscale's upstream has been closed and this might be a breaking change for
some users. Please refer to the
[upstream documentation](https://tailscale.com/docs/reference/syntax/policy-file#tailscale-ssh)
for more information on which types are allowed in `src`, `dst` and `users`.

There is one large inconsistency left, we allow `*` as a destination as we
currently do not support `autogroup:self`, `autogroup:member` and
`autogroup:tagged`. The support for `*` will be removed when we have support for
the autogroups.

**Current state**

The new policy is passing all tests, both integration and unit tests. This does
not mean it is perfect, but it is a good start. Corner cases that is currently
working in v1 and not tested might be broken in v2 (and vice versa).

**We do need help testing this code**

#### Other breaking changes

- Disallow `server_url` and `base_domain` to be equal [#2544](https://github.com/juanfont/headscale/pull/2544)
- Return full user in API for pre auth keys instead of string [#2542](https://github.com/juanfont/headscale/pull/2542)
- Pre auth key API/CLI now uses ID over username [#2542](https://github.com/juanfont/headscale/pull/2542)
- A non-empty list of global nameservers needs to be specified via
  `dns.nameservers.global` if the configuration option `dns.override_local_dns`
  is enabled or is not specified in the configuration file. This aligns with
  behaviour of tailscale.com.
  [#2438](https://github.com/juanfont/headscale/pull/2438)

### Changes

- Use Go 1.24 [#2427](https://github.com/juanfont/headscale/pull/2427)
- Add `slopscale policy check` command to check policy [#2553](https://github.com/juanfont/headscale/pull/2553)
- `oidc.map_legacy_users` and `oidc.strip_email_domain` has been removed [#2411](https://github.com/juanfont/headscale/pull/2411)
- Add more information to `/debug` endpoint [#2420](https://github.com/juanfont/headscale/pull/2420)
  - It is now possible to inspect running goroutines and take profiles
  - View of config, policy, filter, ssh policy per node, connected nodes and
    DERPmap
- OIDC: Fetch UserInfo to get EmailVerified if necessary [#2493](https://github.com/juanfont/headscale/pull/2493)
  - If a OIDC provider doesn't include the `email_verified` claim in its ID
    tokens, Slopscale will attempt to get it from the UserInfo endpoint.
- OIDC: Try to populate name, email and username from UserInfo [#2545](https://github.com/juanfont/headscale/pull/2545)
- Improve performance by only querying relevant nodes from the database for node
  updates [#2509](https://github.com/juanfont/headscale/pull/2509)
- node FQDNs in the netmap will now contain a dot (".") at the end. This aligns
  with behaviour of tailscale.com
  [#2503](https://github.com/juanfont/headscale/pull/2503)
- Restore support for "Override local DNS" [#2438](https://github.com/juanfont/headscale/pull/2438)
- Add documentation for routes [#2496](https://github.com/juanfont/headscale/pull/2496)

## 0.25.1 (2025-02-25)

### Changes

- Fix issue where registration errors are sent correctly [#2435](https://github.com/juanfont/headscale/pull/2435)
- Fix issue where routes passed on registration were not saved [#2444](https://github.com/juanfont/headscale/pull/2444)
- Fix issue where registration page was displayed twice [#2445](https://github.com/juanfont/headscale/pull/2445)

## 0.25.0 (2025-02-11)

### BREAKING

- Authentication flow has been rewritten [#2374](https://github.com/juanfont/headscale/pull/2374) This change should be
  transparent to users with the exception of some buxfixes that has been
  discovered and was fixed as part of the rewrite.
  - When a node is registered with _a new user_, it will be registered as a new
    node ([#2327](https://github.com/juanfont/headscale/issues/2327) and
    [#1310](https://github.com/juanfont/headscale/issues/1310)).
  - A logged out node logging in with the same user will replace the existing
    node.
- Remove support for Tailscale clients older than 1.62 (Capability version 87) [#2405](https://github.com/juanfont/headscale/pull/2405)

### Changes

- `oidc.map_legacy_users` is now `false` by default [#2350](https://github.com/juanfont/headscale/pull/2350)
- Print Tailscale version instead of capability versions for outdated nodes [#2391](https://github.com/juanfont/headscale/pull/2391)
- Do not allow renaming of users from OIDC [#2393](https://github.com/juanfont/headscale/pull/2393)
- Change minimum hostname length to 2 [#2393](https://github.com/juanfont/headscale/pull/2393)
- Fix migration error caused by nodes having invalid auth keys [#2412](https://github.com/juanfont/headscale/pull/2412)
- Pre auth keys belonging to a user are no longer deleted with the user [#2396](https://github.com/juanfont/headscale/pull/2396)
- Pre auth keys that are used by a node can no longer be deleted [#2396](https://github.com/juanfont/headscale/pull/2396)
- Rehaul HTTP errors, return better status code and errors to users [#2398](https://github.com/juanfont/headscale/pull/2398)
- Print slopscale version and commit on server startup [#2415](https://github.com/juanfont/headscale/pull/2415)

## 0.24.3 (2025-02-07)

### Changes

- Fix migration error caused by nodes having invalid auth keys [#2412](https://github.com/juanfont/headscale/pull/2412)
- Pre auth keys belonging to a user are no longer deleted with the user [#2396](https://github.com/juanfont/headscale/pull/2396)
- Pre auth keys that are used by a node can no longer be deleted [#2396](https://github.com/juanfont/headscale/pull/2396)

## 0.24.2 (2025-01-30)

### Changes

- Fix issue where email and username being equal fails to match in Policy [#2388](https://github.com/juanfont/headscale/pull/2388)
- Delete invalid routes before adding a NOT NULL constraint on node_id [#2386](https://github.com/juanfont/headscale/pull/2386)

## 0.24.1 (2025-01-23)

### Changes

- Fix migration issue with user table for PostgreSQL [#2367](https://github.com/juanfont/headscale/pull/2367)
- Relax username validation to allow emails [#2364](https://github.com/juanfont/headscale/pull/2364)
- Remove invalid routes and add stronger constraints for routes to avoid API
  panic [#2371](https://github.com/juanfont/headscale/pull/2371)
- Fix panic when `derp.update_frequency` is 0 [#2368](https://github.com/juanfont/headscale/pull/2368)

## 0.24.0 (2025-01-17)

### Security fix: OIDC changes in Slopscale 0.24.0

The following issue _only_ affects Slopscale installations which authenticate
with OIDC.

_Slopscale v0.23.0 and earlier_ identified OIDC users by the "username" part of
their email address (when `strip_email_domain: true`, the default) or whole
email address (when `strip_email_domain: false`).

Depending on how Slopscale and your Identity Provider (IdP) were configured,
only using the `email` claim could allow a malicious user with an IdP account to
take over another Slopscale user's account, even when
`strip_email_domain: false`.

This would also cause a user to lose access to their Slopscale account if they
changed their email address.

_Slopscale v0.24.0_ now identifies OIDC users by the `iss` and `sub` claims.
[These are guaranteed by the OIDC specification to be stable and unique](https://openid.net/specs/openid-connect-core-1_0.html#ClaimStability),
even if a user changes email address. A well-designed IdP will typically set
`sub` to an opaque identifier like a UUID or numeric ID, which has no relation
to the user's name or email address.

Slopscale v0.24.0 and later will also automatically update profile fields with
OIDC data on login. This means that users can change those details in your IdP,
and have it populate to Slopscale automatically the next time they log in.
However, this may affect the way you reference users in policies.

Slopscale v0.23.0 and earlier never recorded the `iss` and `sub` fields, so all
legacy (existing) OIDC accounts _need to be migrated_ to be properly secured.

#### What do I need to do to migrate?

Slopscale v0.24.0 has an automatic migration feature, which is enabled by
default (`map_legacy_users: true`). **This will be disabled by default in a
future version of Slopscale – any unmigrated users will get new accounts.**

The migration will mostly be done automatically, with one exception. If your
OIDC does not provide an `email_verified` claim, Slopscale will ignore the
`email`. This means that either the administrator will have to mark the user
emails as verified, or ensure the users verify their emails. Any unverified
emails will be ignored, meaning that the users will get new accounts instead of
being migrated.

After this exception is ensured, make all users log into Slopscale with their
account, and Slopscale will automatically update the account record. This will
be transparent to the users.

When all users have logged in, you can disable the automatic migration by
setting `map_legacy_users: false` in your configuration file.

Please note that `map_legacy_users` will be set to `false` by default in v0.25.0
and the migration mechanism will be removed in v0.26.0.

<details>

<summary>What does automatic migration do?</summary>

##### What does automatic migration do?

When automatic migration is enabled (`map_legacy_users: true`), Slopscale will
first match an OIDC account to a Slopscale account by `iss` and `sub`, and then
fall back to matching OIDC users similarly to how Slopscale v0.23.0 did:

- If `strip_email_domain: true` (the default): the Slopscale username matches
  the "username" part of their email address.
- If `strip_email_domain: false`: the Slopscale username matches the _whole_
  email address.

On migration, Slopscale will change the account's username to their
`preferred_username`. **This could break any ACLs or policies which are
configured to match by username.**

Like with Slopscale v0.23.0 and earlier, this migration only works for users who
haven't changed their email address since their last Slopscale login.

A _successful_ automated migration should otherwise be transparent to users.

Once a Slopscale account has been migrated, it will be _unavailable_ to be
matched by the legacy process. An OIDC login with a matching username, but
_non-matching_ `iss` and `sub` will instead get a _new_ Slopscale account.

Because of the way OIDC works, Slopscale's automated migration process can
_only_ work when a user tries to log in after the update.

Legacy account migration should have no effect on new installations where all
users have a recorded `sub` and `iss`.

</details>

<details>

<summary>What happens when automatic migration is disabled?</summary>

##### What happens when automatic migration is disabled?

When automatic migration is disabled (`map_legacy_users: false`), Slopscale will
only try to match an OIDC account to a Slopscale account by `iss` and `sub`.

If there is no match, it will get a _new_ Slopscale account – even if there was
a legacy account which _could_ have matched and migrated.

We recommend new Slopscale users explicitly disable automatic migration – but it
should otherwise have no effect if every account has a recorded `iss` and `sub`.

When automatic migration is disabled, the `strip_email_domain` setting will have
no effect.

</details>

Special thanks to @micolous for reviewing, proposing and working with us on
these changes.

#### Other OIDC changes

Slopscale now uses
[the standard OIDC claims](https://openid.net/specs/openid-connect-core-1_0.html#StandardClaims)
to populate and update user information every time they log in:

| Slopscale profile field | OIDC claim           | Notes / examples                                                                                          |
| ----------------------- | -------------------- | --------------------------------------------------------------------------------------------------------- |
| email address           | `email`              | Only used when `"email_verified": true`                                                                   |
| display name            | `name`               | eg: `Sam Smith`                                                                                           |
| username                | `preferred_username` | Varies depending on IdP and configuration, eg: `ssmith`, `ssmith@idp.example.com`, `\\example.com\ssmith` |
| profile picture         | `picture`            | URL to a profile picture or avatar                                                                        |

These should show up nicely in the Tailscale client.

This will also affect the way you
[reference users in policies](https://github.com/juanfont/headscale/pull/2205).

### BREAKING

- Remove `dns.use_username_in_magic_dns` configuration option [#2020](https://github.com/juanfont/headscale/pull/2020),
  [#2279](https://github.com/juanfont/headscale/pull/2279)
  - Having usernames in magic DNS is no longer possible.
- Remove versions older than 1.56 [#2149](https://github.com/juanfont/headscale/pull/2149)
  - Clean up old code required by old versions
- User gRPC/API [#2261](https://github.com/juanfont/headscale/pull/2261):
  - If you depend on a Slopscale Web UI, you should wait with this update until
    the UI have been updated to match the new API.
  - `GET /api/v1/user/{name}` and `GetUser` have been removed in favour of
    `ListUsers` with an ID parameter
  - `RenameUser` and `DeleteUser` now require an ID instead of a name.

### Changes

- Improved compatibility of built-in DERP server with clients connecting over
  WebSocket [#2132](https://github.com/juanfont/headscale/pull/2132)
- Allow nodes to use SSH agent forwarding [#2145](https://github.com/juanfont/headscale/pull/2145)
- Fixed processing of fields in post request in MoveNode rpc [#2179](https://github.com/juanfont/headscale/pull/2179)
- Added conversion of 'Hostname' to 'givenName' in a node with FQDN rules
  applied [#2198](https://github.com/juanfont/headscale/pull/2198)
- Fixed updating of hostname and givenName when it is updated in HostInfo [#2199](https://github.com/juanfont/headscale/pull/2199)
- Fixed missing `stable-debug` container tag [#2232](https://github.com/juanfont/headscale/pull/2232)
- Loosened up `server_url` and `base_domain` check. It was overly strict in some
  cases. [#2248](https://github.com/juanfont/headscale/pull/2248)
- CLI for managing users now accepts `--identifier` in addition to `--name`,
  usage of `--identifier` is recommended
  [#2261](https://github.com/juanfont/headscale/pull/2261)
- Add `dns.extra_records_path` configuration option [#2262](https://github.com/juanfont/headscale/issues/2262)
- Support client verify for DERP [#2046](https://github.com/juanfont/headscale/pull/2046)
- Add PKCE Verifier for OIDC [#2314](https://github.com/juanfont/headscale/pull/2314)

## 0.23.0 (2024-09-18)

This release was intended to be mainly a code reorganisation and refactoring,
significantly improving the maintainability of the codebase. This should allow
us to improve further and make it easier for the maintainers to keep on top of
the project. However, as you all have noticed, it turned out to become a much
larger, much longer release cycle than anticipated. It has ended up to be a
release with a lot of rewrites and changes to the code base and functionality of
Slopscale, cleaning up a lot of technical debt and introducing a lot of
improvements. This does come with some breaking changes,

**Please remember to always back up your database between versions**

#### Here is a short summary of the broad topics of changes:

Code has been organised into modules, reducing use of global variables/objects,
isolating concerns and “putting the right things in the logical place”.

The new
[policy](https://github.com/aislopware/slopscale/tree/main/hscontrol/policy) and
[mapper](https://github.com/aislopware/slopscale/tree/main/hscontrol/mapper)
package, containing the ACL/Policy logic and the logic for creating the data
served to clients (the network “map”) has been rewritten and improved. This
change has allowed us to finish SSH support and add additional tests throughout
the code to ensure correctness.

The
[“poller”, or streaming logic](https://github.com/aislopware/slopscale/blob/main/hscontrol/poll.go)
has been rewritten and instead of keeping track of the latest updates, checking
at a fixed interval, it now uses go channels, implemented in our new
[notifier](https://github.com/aislopware/slopscale/tree/main/hscontrol/notifier)
package and it allows us to send updates to connected clients immediately. This
should both improve performance and potential latency before a client picks up
an update.

Slopscale now supports sending “delta” updates, thanks to the new mapper and
poller logic, allowing us to only inform nodes about new nodes, changed nodes
and removed nodes. Previously we sent the entire state of the network every time
an update was due.

While we have a pretty good
[test harness](https://github.com/search?q=repo%3Ajuanfont%2Fslopscale+path%3A_test.go&type=code)
for validating our changes, the changes came down to
[284 changed files with 32,316 additions and 24,245 deletions](https://github.com/aislopware/slopscale/compare/b01f1f1867136d9b2d7b1392776eb363b482c525...ed78ecd)
and bugs are expected. We need help testing this release. In addition, while we
think the performance should in general be better, there might be regressions in
parts of the platform, particularly where we prioritised correctness over speed.

There are also several bugfixes that has been encountered and fixed as part of
implementing these changes, particularly after improving the test harness as
part of adopting [#1460](https://github.com/juanfont/headscale/pull/1460).

### BREAKING

- Code reorganisation, a lot of code has moved, please review the following PRs
  accordingly [#1473](https://github.com/juanfont/headscale/pull/1473)
- Change the structure of database configuration, see
  [config-example.yaml](./config-example.yaml) for the new structure.
  [#1700](https://github.com/juanfont/headscale/pull/1700)
  - Old structure has been remove and the configuration _must_ be converted.
  - Adds additional configuration for PostgreSQL for setting max open, idle
    connection and idle connection lifetime.
- API: Machine is now Node [#1553](https://github.com/juanfont/headscale/pull/1553)
- Remove support for older Tailscale clients [#1611](https://github.com/juanfont/headscale/pull/1611)
  - The oldest supported client is 1.42
- Slopscale checks that _at least_ one DERP is defined at start [#1564](https://github.com/juanfont/headscale/pull/1564)
  - If no DERP is configured, the server will fail to start, this can be because
    it cannot load the DERPMap from file or url.
- Embedded DERP server requires a private key [#1611](https://github.com/juanfont/headscale/pull/1611)
  - Add a filepath entry to
    [`derp.server.private_key_path`](https://github.com/aislopware/slopscale/blob/b35993981297e18393706b2c963d6db882bba6aa/config-example.yaml#L95)
- Docker images are now built with goreleaser (ko) [#1716](https://github.com/juanfont/headscale/pull/1716)
  [#1763](https://github.com/juanfont/headscale/pull/1763)
  - Entrypoint of container image has changed from shell to slopscale, require
    change from `slopscale serve` to `serve`
  - `/var/lib/slopscale` and `/var/run/slopscale` is no longer created
    automatically, see [container docs](./docs/setup/install/container.md)
- Prefixes are now defined per v4 and v6 range. [#1756](https://github.com/juanfont/headscale/pull/1756)
  - `ip_prefixes` option is now `prefixes.v4` and `prefixes.v6`
  - `prefixes.allocation` can be set to assign IPs at `sequential` or `random`.
    [#1869](https://github.com/juanfont/headscale/pull/1869)
- MagicDNS domains no longer contain usernames
  - This is in preparation to fix Slopscales implementation of tags which
    currently does not correctly remove the link between a tagged device and a
    user. As tagged devices will not have a user, this will require a change to
    the DNS generation, removing the username, see
    [#1369](https://github.com/juanfont/headscale/issues/1369) for more
    information.
  - `use_username_in_magic_dns` can be used to turn this behaviour on again, but
    note that this option _will be removed_ when tags are fixed.
    - dns.base_domain can no longer be the same as (or part of) server_url.
    - This option brings Slopscales behaviour in line with Tailscale.
- YAML files are no longer supported for slopscale policy. [#1792](https://github.com/juanfont/headscale/pull/1792)
  - HuJSON is now the only supported format for policy.
- DNS configuration has been restructured [#2034](https://github.com/juanfont/headscale/pull/2034)
  - Please review the new [config-example.yaml](./config-example.yaml) for the
    new structure.

### Changes

- Use versioned migrations [#1644](https://github.com/juanfont/headscale/pull/1644)
- Make the OIDC callback page better [#1484](https://github.com/juanfont/headscale/pull/1484)
- SSH support [#1487](https://github.com/juanfont/headscale/pull/1487)
- State management has been improved [#1492](https://github.com/juanfont/headscale/pull/1492)
- Use error group handling to ensure tests actually pass [#1535](https://github.com/juanfont/headscale/pull/1535) based on
  [#1460](https://github.com/juanfont/headscale/pull/1460)
- Fix hang on SIGTERM [#1492](https://github.com/juanfont/headscale/pull/1492)
  taken from [#1480](https://github.com/juanfont/headscale/pull/1480)
- Send logs to stderr by default [#1524](https://github.com/juanfont/headscale/pull/1524)
- Fix [TS-2023-006](https://tailscale.com/security-bulletins/#ts-2023-006)
  security UPnP issue [#1563](https://github.com/juanfont/headscale/pull/1563)
- Turn off gRPC logging [#1640](https://github.com/juanfont/headscale/pull/1640)
  fixes [#1259](https://github.com/juanfont/headscale/issues/1259)
- Added the possibility to manually create a DERP-map entry which can be
  customized, instead of automatically creating it.
  [#1565](https://github.com/juanfont/headscale/pull/1565)
- Add support for deleting api keys [#1702](https://github.com/juanfont/headscale/pull/1702)
- Add command to backfill IP addresses for nodes missing IPs from configured
  prefixes. [#1869](https://github.com/juanfont/headscale/pull/1869)
- Log available update as warning [#1877](https://github.com/juanfont/headscale/pull/1877)
- Add `autogroup:internet` to Policy [#1917](https://github.com/juanfont/headscale/pull/1917)
- Restore foreign keys and add constraints [#1562](https://github.com/juanfont/headscale/pull/1562)
- Make registration page easier to use on mobile devices
- Make write-ahead-log default on and configurable for SQLite [#1985](https://github.com/juanfont/headscale/pull/1985)
- Add APIs for managing slopscale policy. [#1792](https://github.com/juanfont/headscale/pull/1792)
- Fix for registering nodes using preauthkeys when running on a postgres
  database in a non-UTC timezone.
  [#764](https://github.com/juanfont/headscale/issues/764)
- Make sure integration tests cover postgres for all scenarios
- CLI commands (all except `serve`) only requires minimal configuration, no more
  errors or warnings from unset settings
  [#2109](https://github.com/juanfont/headscale/pull/2109)
- CLI results are now concistently sent to stdout and errors to stderr [#2109](https://github.com/juanfont/headscale/pull/2109)
- Fix issue where shutting down slopscale would hang [#2113](https://github.com/juanfont/headscale/pull/2113)

## 0.22.3 (2023-05-12)

### Changes

- Added missing ca-certificates in Docker image [#1463](https://github.com/juanfont/headscale/pull/1463)

## 0.22.2 (2023-05-10)

### Changes

- Add environment flags to enable pprof (profiling) [#1382](https://github.com/juanfont/headscale/pull/1382)
  - Profiles are continuously generated in our integration tests.
- Fix systemd service file location in `.deb` packages [#1391](https://github.com/juanfont/headscale/pull/1391)
- Improvements on Noise implementation [#1379](https://github.com/juanfont/headscale/pull/1379)
- Replace node filter logic, ensuring nodes with access can see each other [#1381](https://github.com/juanfont/headscale/pull/1381)
- Disable (or delete) both exit routes at the same time [#1428](https://github.com/juanfont/headscale/pull/1428)
- Ditch distroless for Docker image, create default socket dir in
  `/var/run/slopscale` [#1450](https://github.com/juanfont/headscale/pull/1450)

## 0.22.1 (2023-04-20)

### Changes

- Fix issue where systemd could not bind to port 80 [#1365](https://github.com/juanfont/headscale/pull/1365)

## 0.22.0 (2023-04-20)

### Changes

- Add `.deb` packages to release process [#1297](https://github.com/juanfont/headscale/pull/1297)
- Update and simplify the documentation to use new `.deb` packages [#1349](https://github.com/juanfont/headscale/pull/1349)
- Add 32-bit Arm platforms to release process [#1297](https://github.com/juanfont/headscale/pull/1297)
- Fix longstanding bug that would prevent "\*" from working properly in ACLs
  (issue [#699](https://github.com/juanfont/headscale/issues/699))
  [#1279](https://github.com/juanfont/headscale/pull/1279)
- Fix issue where IPv6 could not be used in, or while using ACLs (part of [#809](https://github.com/juanfont/headscale/issues/809))
  [#1339](https://github.com/juanfont/headscale/pull/1339)
- Target Go 1.20 and Tailscale 1.38 for Slopscale [#1323](https://github.com/juanfont/headscale/pull/1323)

## 0.21.0 (2023-03-20)

### Changes

- Adding "configtest" CLI command. [#1230](https://github.com/juanfont/headscale/pull/1230)
- Add documentation on connecting with iOS to `/apple` [#1261](https://github.com/juanfont/headscale/pull/1261)
- Update iOS compatibility and added documentation for iOS [#1264](https://github.com/juanfont/headscale/pull/1264)
- Allow to delete routes [#1244](https://github.com/juanfont/headscale/pull/1244)

## 0.20.0 (2023-02-03)

### Changes

- Fix wrong behaviour in exit nodes [#1159](https://github.com/juanfont/headscale/pull/1159)
- Align behaviour of `dns_config.restricted_nameservers` to tailscale [#1162](https://github.com/juanfont/headscale/pull/1162)
- Make OpenID Connect authenticated client expiry time configurable [#1191](https://github.com/juanfont/headscale/pull/1191)
  - defaults to 180 days like Tailscale SaaS
  - adds option to use the expiry time from the OpenID token for the node (see
    config-example.yaml)
- Set ControlTime in Map info sent to nodes [#1195](https://github.com/juanfont/headscale/pull/1195)
- Populate Tags field on Node updates sent [#1195](https://github.com/juanfont/headscale/pull/1195)

## 0.19.0 (2023-01-29)

### BREAKING

- Rename Namespace to User [#1144](https://github.com/juanfont/headscale/pull/1144)
  - **BACKUP your database before upgrading**
- Command line flags previously taking `--namespace` or `-n` will now require
  `--user` or `-u`

## 0.18.0 (2023-01-14)

### Changes

- Reworked routing and added support for subnet router failover [#1024](https://github.com/juanfont/headscale/pull/1024)
- Added an OIDC AllowGroups Configuration options and authorization check [#1041](https://github.com/juanfont/headscale/pull/1041)
- Set `db_ssl` to false by default [#1052](https://github.com/juanfont/headscale/pull/1052)
- Fix duplicate nodes due to incorrect implementation of the protocol [#1058](https://github.com/juanfont/headscale/pull/1058)
- Report if a machine is online in CLI more accurately [#1062](https://github.com/juanfont/headscale/pull/1062)
- Added config option for custom DNS records [#1035](https://github.com/juanfont/headscale/pull/1035)
- Expire nodes based on OIDC token expiry [#1067](https://github.com/juanfont/headscale/pull/1067)
- Remove ephemeral nodes on logout [#1098](https://github.com/juanfont/headscale/pull/1098)
- Performance improvements in ACLs [#1129](https://github.com/juanfont/headscale/pull/1129)
- OIDC client secret can be passed via a file [#1127](https://github.com/juanfont/headscale/pull/1127)

## 0.17.1 (2022-12-05)

### Changes

- Correct typo on macOS standalone profile link [#1028](https://github.com/juanfont/headscale/pull/1028)
- Update platform docs with Fast User Switching [#1016](https://github.com/juanfont/headscale/pull/1016)

## 0.17.0 (2022-11-26)

### BREAKING

- `noise.private_key_path` has been added and is required for the new noise
  protocol.
- Log level option `log_level` was moved to a distinct `log` config section and
  renamed to `level` [#768](https://github.com/juanfont/headscale/pull/768)
- Removed Alpine Linux container image [#962](https://github.com/juanfont/headscale/pull/962)

### Important Changes

- Added support for Tailscale TS2021 protocol [#738](https://github.com/juanfont/headscale/pull/738)
- Add experimental support for
  [SSH ACL](https://tailscale.com/docs/reference/syntax/policy-file#tailscale-ssh) (see docs for
  limitations) [#847](https://github.com/juanfont/headscale/pull/847)
  - Please note that this support should be considered _partially_ implemented
  - SSH ACLs status:
    - Support `accept` and `check` (SSH can be enabled and used for connecting
      and authentication)
    - Rejecting connections **are not supported**, meaning that if you enable
      SSH, then assume that _all_ `ssh` connections **will be allowed**.
    - If you decided to try this feature, please carefully managed permissions
      by blocking port `22` with regular ACLs or do _not_ set `--ssh` on your
      clients.
    - We are currently improving our testing of the SSH ACLs, help us get an
      overview by testing and giving feedback.
  - This feature should be considered dangerous and it is disabled by default.
    Enable by setting `SLOPSCALE_EXPERIMENTAL_FEATURE_SSH=1`.

### Changes

- Add ability to specify config location via env var `SLOPSCALE_CONFIG` [#674](https://github.com/juanfont/headscale/issues/674)
- Target Go 1.19 for Slopscale [#778](https://github.com/juanfont/headscale/pull/778)
- Target Tailscale v1.30.0 to build Slopscale [#780](https://github.com/juanfont/headscale/pull/780)
- Give a warning when running Slopscale with reverse proxy improperly configured
  for WebSockets [#788](https://github.com/juanfont/headscale/pull/788)
- Fix subnet routers with Primary Routes [#811](https://github.com/juanfont/headscale/pull/811)
- Added support for JSON logs [#653](https://github.com/juanfont/headscale/issues/653)
- Sanitise the node key passed to registration url [#823](https://github.com/juanfont/headscale/pull/823)
- Add support for generating pre-auth keys with tags [#767](https://github.com/juanfont/headscale/pull/767)
- Add support for evaluating `autoApprovers` ACL entries when a machine is
  registered [#763](https://github.com/juanfont/headscale/pull/763)
- Add config flag to allow Slopscale to start if OIDC provider is down [#829](https://github.com/juanfont/headscale/pull/829)
- Fix prefix length comparison bug in AutoApprovers route evaluation [#862](https://github.com/juanfont/headscale/pull/862)
- Random node DNS suffix only applied if names collide in namespace. [#766](https://github.com/juanfont/headscale/issues/766)
- Remove `ip_prefix` configuration option and warning [#899](https://github.com/juanfont/headscale/pull/899)
- Add `dns_config.override_local_dns` option [#905](https://github.com/juanfont/headscale/pull/905)
- Fix some DNS config issues [#660](https://github.com/juanfont/headscale/issues/660)
- Make it possible to disable TS2019 with build flag [#928](https://github.com/juanfont/headscale/pull/928)
- Fix OIDC registration issues [#960](https://github.com/juanfont/headscale/pull/960) and
  [#971](https://github.com/juanfont/headscale/pull/971)
- Add support for specifying NextDNS DNS-over-HTTPS resolver [#940](https://github.com/juanfont/headscale/pull/940)
- Make more sslmode available for postgresql connection [#927](https://github.com/juanfont/headscale/pull/927)

## 0.16.4 (2022-08-21)

### Changes

- Add ability to connect to PostgreSQL over TLS/SSL [#745](https://github.com/juanfont/headscale/pull/745)
- Fix CLI registration of expired machines [#754](https://github.com/juanfont/headscale/pull/754)

## 0.16.3 (2022-08-17)

### Changes

- Fix issue with OIDC authentication [#747](https://github.com/juanfont/headscale/pull/747)

## 0.16.2 (2022-08-14)

### Changes

- Fixed bugs in the client registration process after migration to NodeKey [#735](https://github.com/juanfont/headscale/pull/735)

## 0.16.1 (2022-08-12)

### Changes

- Updated dependencies (including the library that lacked armhf support) [#722](https://github.com/juanfont/headscale/pull/722)
- Fix missing group expansion in function `excludeCorrectlyTaggedNodes` [#563](https://github.com/juanfont/headscale/issues/563)
- Improve registration protocol implementation and switch to NodeKey as main
  identifier [#725](https://github.com/juanfont/headscale/pull/725)
- Add ability to connect to PostgreSQL via unix socket [#734](https://github.com/juanfont/headscale/pull/734)

## 0.16.0 (2022-07-25)

**Note:** Take a backup of your database before upgrading.

### BREAKING

- Old ACL syntax is no longer supported ("users" & "ports" -> "src" & "dst").
  Please check [the new syntax](https://tailscale.com/docs/features/access-control/acls).

### Changes

- **Drop** armhf (32-bit ARM) support. [#609](https://github.com/juanfont/headscale/pull/609)
- Slopscale fails to serve if the ACL policy file cannot be parsed [#537](https://github.com/juanfont/headscale/pull/537)
- Fix labels cardinality error when registering unknown pre-auth key [#519](https://github.com/juanfont/headscale/pull/519)
- Fix send on closed channel crash in polling [#542](https://github.com/juanfont/headscale/pull/542)
- Fixed spurious calls to setLastStateChangeToNow from ephemeral nodes [#566](https://github.com/juanfont/headscale/pull/566)
- Add command for moving nodes between namespaces [#362](https://github.com/juanfont/headscale/issues/362)
- Added more configuration parameters for OpenID Connect (scopes, free-form
  parameters, domain and user allowlist)
- Add command to set tags on a node [#525](https://github.com/juanfont/headscale/issues/525)
- Add command to view tags of nodes [#356](https://github.com/juanfont/headscale/issues/356)
- Add --all (-a) flag to enable routes command [#360](https://github.com/juanfont/headscale/issues/360)
- Fix issue where nodes was not updated across namespaces [#560](https://github.com/juanfont/headscale/pull/560)
- Add the ability to rename a nodes name [#560](https://github.com/juanfont/headscale/pull/560)
  - Node DNS names are now unique, a random suffix will be added when a node
    joins
  - This change contains database changes, remember to **backup** your database
    before upgrading
- Add option to enable/disable logtail (Tailscale's logging infrastructure) [#596](https://github.com/juanfont/headscale/pull/596)
  - This change disables the logs by default
- Use [Prometheus]'s duration parser, supporting days (`d`), weeks (`w`) and
  years (`y`) [#598](https://github.com/juanfont/headscale/pull/598)
- Add support for reloading ACLs with SIGHUP [#601](https://github.com/juanfont/headscale/pull/601)
- Use new ACL syntax [#618](https://github.com/juanfont/headscale/pull/618)
- Add -c option to specify config file from command line [#285](https://github.com/juanfont/headscale/issues/285)
  [#612](https://github.com/juanfont/headscale/pull/601)
- Add configuration option to allow Tailscale clients to use a random WireGuard
  port. [Tailscale docs](https://tailscale.com/docs/reference/syntax/policy-file#randomizeclientport)
  [#624](https://github.com/juanfont/headscale/pull/624)
- Improve obtuse UX regarding missing configuration
  (`ephemeral_node_inactivity_timeout` not set)
  [#639](https://github.com/juanfont/headscale/pull/639)
- Fix nodes being shown as 'offline' in `tailscale status` [#648](https://github.com/juanfont/headscale/pull/648)
- Improve shutdown behaviour [#651](https://github.com/juanfont/headscale/pull/651)
- Drop Gin as web framework in Slopscale
  [648](https://github.com/juanfont/headscale/pull/648)
  [677](https://github.com/juanfont/headscale/pull/677)
- Make tailnet node updates check interval configurable [#675](https://github.com/juanfont/headscale/pull/675)
- Fix regression with HTTP API [#684](https://github.com/juanfont/headscale/pull/684)
- nodes ls now print both Hostname and Name(Issue [#647](https://github.com/juanfont/headscale/issues/647) PR
  [#687](https://github.com/juanfont/headscale/pull/687))

## 0.15.0 (2022-03-20)

**Note:** Take a backup of your database before upgrading.

### BREAKING

- Boundaries between Namespaces has been removed and all nodes can communicate
  by default [#357](https://github.com/juanfont/headscale/pull/357)
  - To limit access between nodes, use [ACLs](./docs/ref/acls.md).
- `/metrics` is now a configurable host:port endpoint: [#344](https://github.com/juanfont/headscale/pull/344). You must update your
  `config.yaml` file to include:
  ```yaml
  metrics_listen_addr: 127.0.0.1:9090
  ```

### Features

- Add support for writing ACL files with YAML [#359](https://github.com/juanfont/headscale/pull/359)
- Users can now use emails in ACL's groups [#372](https://github.com/juanfont/headscale/issues/372)
- Add shorthand aliases for commands and subcommands [#376](https://github.com/juanfont/headscale/pull/376)
- Add `/windows` endpoint for Windows configuration instructions + registry file
  download [#392](https://github.com/juanfont/headscale/pull/392)
- Added embedded DERP (and STUN) server into Slopscale [#388](https://github.com/juanfont/headscale/pull/388)

### Changes

- Fix a bug were the same IP could be assigned to multiple hosts if joined in
  quick succession [#346](https://github.com/juanfont/headscale/pull/346)
- Simplify the code behind registration of machines [#366](https://github.com/juanfont/headscale/pull/366)
  - Nodes are now only written to database if they are registered successfully
- Fix a limitation in the ACLs that prevented users to write rules with `*` as
  source [#374](https://github.com/juanfont/headscale/issues/374)
- Reduce the overhead of marshal/unmarshal for Hostinfo, routes and endpoints by
  using specific types in Machine
  [#371](https://github.com/juanfont/headscale/pull/371)
- Apply normalization function to FQDN on hostnames when hosts registers and
  retrieve information [#363](https://github.com/juanfont/headscale/issues/363)
- Fix a bug that prevented the use of `tailscale logout` with OIDC [#508](https://github.com/juanfont/headscale/issues/508)
- Added Tailscale repo HEAD and unstable releases channel to the integration
  tests targets [#513](https://github.com/juanfont/headscale/pull/513)

## 0.14.0 (2022-02-24)

\*\*UPCOMING ### BREAKING From the \*\*next\*\* version (`0.15.0`), all machines
will be able to communicate regardless of if they are in the same namespace.
This means that the behaviour currently limited to ACLs will become default.
From version `0.15.0`, all limitation of communications must be done with ACLs.

This is a part of aligning `slopscale`'s behaviour with Tailscale's upstream
behaviour.

### BREAKING

- ACLs have been rewritten to align with the bevaviour Tailscale Control Panel
  provides. **NOTE:** This is only active if you use ACLs
  - Namespaces are now treated as Users
  - All machines can communicate with all machines by default
  - Tags should now work correctly and adding a host to Slopscale should now
    reload the rules.
  - The documentation have a [fictional example](./docs/ref/acls.md) that should
    cover some use cases of the ACLs features

### Features

- Add support for configurable mTLS [docs](./docs/ref/tls.md) [#297](https://github.com/juanfont/headscale/pull/297)

### Changes

- Remove dependency on CGO (switch from CGO SQLite to pure Go) [#346](https://github.com/juanfont/headscale/pull/346)

**0.13.0 (2022-02-18):**

### Features

- Add IPv6 support to the prefix assigned to namespaces
- Add API Key support
  - Enable remote control of `slopscale` via CLI
    [docs](./docs/ref/api.md#grpc)
  - Enable HTTP API (beta, subject to change)
- OpenID Connect users will be mapped per namespaces
  - Each user will get its own namespace, created if it does not exist
  - `oidc.domain_map` option has been removed
  - `strip_email_domain` option has been added (see
    [config-example.yaml](./config-example.yaml))

### Changes

- `ip_prefix` is now superseded by `ip_prefixes` in the configuration [#208](https://github.com/juanfont/headscale/pull/208)
- Upgrade `tailscale` (1.20.4) and other dependencies to latest [#314](https://github.com/juanfont/headscale/pull/314)
- fix swapped machine\<->namespace labels in `/metrics` [#312](https://github.com/juanfont/headscale/pull/312)
- remove key-value based update mechanism for namespace changes [#316](https://github.com/juanfont/headscale/pull/316)

**0.12.4 (2022-01-29):**

### Changes

- Make gRPC Unix Socket permissions configurable [#292](https://github.com/juanfont/headscale/pull/292)
- Trim whitespace before reading Private Key from file [#289](https://github.com/juanfont/headscale/pull/289)
- Add new command to generate a private key for `slopscale` [#290](https://github.com/juanfont/headscale/pull/290)
- Fixed issue where hosts deleted from control server may be written back to the
  database, as long as they are connected to the control server
  [#278](https://github.com/juanfont/headscale/pull/278)

## 0.12.3 (2022-01-13)

### Changes

- Added Alpine container [#270](https://github.com/juanfont/headscale/pull/270)
- Minor updates in dependencies [#271](https://github.com/juanfont/headscale/pull/271)

## 0.12.2 (2022-01-11)

Happy New Year!

### Changes

- Fix Docker release [#258](https://github.com/juanfont/headscale/pull/258)
- Rewrite main docs [#262](https://github.com/juanfont/headscale/pull/262)
- Improve Docker docs [#263](https://github.com/juanfont/headscale/pull/263)

## 0.12.1 (2021-12-24)

(We are skipping 0.12.0 to correct a mishap done weeks ago with the version
tagging)

### BREAKING

- Upgrade to Tailscale 1.18 [#229](https://github.com/juanfont/headscale/pull/229)
  - This change requires a new format for private key, private keys are now
    generated automatically:
    1. Delete your current key
    1. Restart `slopscale`, a new key will be generated.
    1. Restart all Tailscale clients to fetch the new key

### Changes

- Unify configuration example [#197](https://github.com/juanfont/headscale/pull/197)
- Add stricter linting and formatting [#223](https://github.com/juanfont/headscale/pull/223)

### Features

- Add gRPC and HTTP API (HTTP API is currently disabled) [#204](https://github.com/juanfont/headscale/pull/204)
- Use gRPC between the CLI and the server [#206](https://github.com/juanfont/headscale/pull/206),
  [#212](https://github.com/juanfont/headscale/pull/212)
- Beta OpenID Connect support [#126](https://github.com/juanfont/headscale/pull/126),
  [#227](https://github.com/juanfont/headscale/pull/227)

## 0.11.0 (2021-10-25)

### BREAKING

- Make slopscale fetch DERP map from URL and file [#196](https://github.com/juanfont/headscale/pull/196)
