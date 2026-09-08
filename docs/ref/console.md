# Admin console

Headscale ships a web console at `/admin/` on the server's own address. It
lists machines, users and keys, edits the policy, approves devices and users,
shares nodes and marks the global exit node — the same things the CLI and the
API do — from a browser.

The console is a static, client-rendered application embedded in the
`headscale` binary. It keeps no state on the server and needs no extra process:
open `https://<your server>/admin/` and it loads.

## Signing in

The console signs in only through the [identity provider](oidc.md): the
sign-in page has one button, *Continue with Google* (or the provider's name),
and nothing else. The browser is sent through the provider and comes back
signed in as the matching headscale user, holding a session cookie that lasts
seven days; *Sign out* in the account menu ends it. The provider's redirect
URI is the same `/oidc/callback` as for device logins, so nothing more has to
be registered.

The client ID and secret go either in the configuration file or in the
environment, whichever suits the deployment:

```yaml
oidc:
  issuer: https://accounts.google.com
  client_id: 1234567890-abc.apps.googleusercontent.com
  client_secret: GOCSPX-...
```

```console
$ export HEADSCALE_OIDC_CLIENT_ID=1234567890-abc.apps.googleusercontent.com
$ export HEADSCALE_OIDC_CLIENT_SECRET=GOCSPX-...
```

Every key of the configuration can be set this way: `HEADSCALE_` followed by
the key path with dots replaced by underscores.

A user who signs in for the first time is created the same way as on a device
login, including [user approval](approval.md) when it is on: until an
administrator approves them, the sign-in page says so and opens no session.
The first user of an empty server becomes its owner. Anyone else who should
administer the server from day one is named in `oidc.admin_users`: an
address on that list is made an admin the moment it signs in, so nobody has
to hand out roles over the CLI first.

```yaml
oidc:
  admin_users:
    - alice@example.com
```

```console
$ export HEADSCALE_OIDC_ADMIN_USERS="alice@example.com bob@example.com"
```

The list is checked on every sign-in and only ever promotes a member; the
owner and users who already hold a role keep it, and removing an address
does not demote anyone (use `headscale users set-role` for that). The
promotion is written to the audit log as a system `user.role.set`.

Without an identity provider the console cannot sign anyone in and says so;
the CLI and the API keep working with API keys.

What the console can show and change is decided by the signed-in user's
current [role](roles.md), read on every request:

- A role change takes effect at once, and deleting the user ends their
  sessions.
- An auditor sees everything and can change nothing, an `it-admin` cannot
  approve routes, and so on. Pages the user cannot read are hidden; actions
  they cannot take are disabled.
- A member sees only the overview, their own machines and their own API keys.

Everything the console changes is written to the [audit log](audit.md) with
the signed-in user as the actor.

## Getting around

The sidebar has two groups. *Network* holds machines, users and keys, and
*Control* holds access controls, settings and the audit log. A count next
to _Machines_ and _Users_ says how many are waiting for approval. The arrow
in the sidebar footer collapses it to an icon rail. *Quick search*, or
++cmd+k++ / ++ctrl+k++, jumps to any page, machine or user by name. The
top bar shows breadcrumbs for where you are, the light/dark switch and the
account menu with *Sign out*.

## Pages

- **Overview**: counts, machines and users waiting for approval (approve them
  in place), the machines seen most recently, and a getting-started
  checklist while the tailnet is empty.
- **Machines**: every node with its owner or tags, addresses, status and
  routes. Search by name, address, user or tag and filter by status or user.
  Each machine has a detail page with its keys, routes (approve with a switch),
  sharing and the global exit node switch, plus rename, tag, expire and remove.
  _Add machine_ mints a pre-auth key and hands over the join command for
  Linux, macOS, Windows and Docker, next to a QR code carrying the same line
  so a phone or a machine without a shared clipboard can pick it up; the
  iOS and Android tab gives the server address and where the app takes it.
  _Register a machine_ (`/admin/machines/register`) completes an interactive
  login instead: a machine that signed in without a key or an identity
  provider shows a registration key, and the server's registration page links
  here with it filled in; see [Registration](registration.md).
  _Authentication check_ (`/admin/machines/auth-check`) approves or rejects an
  SSH session held by a check-mode rule, linked from the page the SSH client
  opens.
- **Users**: create, rename, approve, change the [role](roles.md) and delete
  users.
- **Keys**: pre-auth keys (create with reusable, ephemeral, pre-authorized and
  tags; expire; delete), API keys and OAuth clients for the v2 API (create
  with scopes and tags; revoke). New keys and client secrets are shown once,
  with a copy button.
- **Access controls**: [groups and access rules](access-control.md), and the
  [policy](policy.md) file in an editor with syntax highlighting. *Check*
  validates the draft against the server without saving; *Save* applies it.
  Leaving the page with unsaved changes asks first.
- **Networks**: [networks](networks.md) that hand subnets and exit nodes to
  groups, and a list of every route any machine advertises, with approval for
  the ones no network owns.
- **DNS**: nameservers, split DNS, search domains and extra records, changed
  at runtime; see [DNS](dns.md).
- **Webhooks**: endpoints that receive signed event notifications, with
  their subscriptions and last delivery; create, edit, test, rotate the
  secret and delete; see [Webhooks](webhooks.md).
- **Audit log**: who changed what, newest first, with filters by action, user
  and time; see [Audit log](audit.md).
- **Settings**: the [device and user approval](approval.md) switches, the
  key expiry cap, a _Maintenance_ section with the IP address backfill
  (`headscale nodes backfillips`), the signed-in credential's role and scopes,
  and the server's build, addresses, DERP regions and config file values.

## Building from source

Release binaries and container images include the console. When building from
source, build it first so the binary embeds it:

```console
$ make web
$ make build
```

`make web` needs [bun](https://bun.sh) (the Nix development shell provides it).
A binary built without it still serves `/admin/`, with a page saying the
console is missing, and the API works as usual.

The console lives in `web/`: React with the TanStack router, query and table
libraries and Cloudflare's [Kumo](https://kumo-ui.com) design system (Base UI
components and Tailwind CSS), checked
by TypeScript, oxlint and oxfmt. Its API types are generated from the server's
OpenAPI document by `make web-generate` and committed. `bun run dev` in `web/`
starts a development server that proxies `/api` and `/oidc` to a headscale on
`http://127.0.0.1:8080` (set `HEADSCALE_URL` to point elsewhere).

### Signing in without Google

The console only signs in through an identity provider, so development and
tests need one that asks no questions. `go run ./cmd/dev` starts a headscale
with a mock OpenID Connect provider running inside the same process: every
sign-in comes back as `jane.doe@example.com`, who is listed in that server's
`oidc.admin_users` and therefore opens the console as an admin.

```console
$ go run ./cmd/dev                       # server on :8080, provider on :9100
$ open http://127.0.0.1:8080/admin/      # Continue with single sign-on
```

To sign in from the Vite development server instead, start headscale with
its public URL set to Vite's origin, so the provider sends the browser back
there: `go run ./cmd/dev -server-url http://localhost:5173`, then
`bun run dev` in `web/` and open `http://localhost:5173/admin/`.

`make test-e2e` runs the same flow in a browser: it builds the console,
starts `cmd/dev`, signs in through the mock provider, checks the audit log
and signs out (Playwright, `web/e2e/`). The Go side is covered by the
`TestConsoleLogin*` tests in `hscontrol/servertest`, which drive a mock
provider without a browser.

## Serving behind a reverse proxy

The console lives under `/admin/` on the same origin as the API, so a
[reverse proxy](integration/reverse-proxy.md) that forwards the whole host
needs no extra rules. It sends a `Content-Security-Policy` that only allows its
own origin; do not embed it in another site's frame.

If you prefer the console not to be reachable, block `/admin/` at the proxy.
Everything it does is also available through the API with the same key, so
this only removes the page, not the capability.
