# Admin console

Headscale ships a web console at `/admin/` on the server's own address. It
lists machines, users and keys, edits the policy, approves devices and users,
shares nodes and marks the global exit node — the same things the CLI and the
API do — from a browser.

The console is a static, client-rendered application embedded in the
`headscale` binary. It keeps no state on the server and needs no extra process:
open `https://<your server>/admin/` and it loads.

## Signing in

The console authenticates with an API key. Create one:

```console
$ headscale apikeys create --expiration 90d
```

Paste it on the sign-in page. The key stays in that browser's local storage
and is sent as a bearer token to `/api/v1`; sign out from the account menu to
forget it.

What the console can show and change is decided by the key, not the console:

- A key without a user (the command above) is all-access.
- A key created with `--user` is bounded by the user's current
  [role](roles.md). An auditor sees everything and can change nothing, an
  `it-admin` cannot approve routes, and so on. Pages the key cannot read are
  hidden; actions it cannot take are disabled.
- A member's key shows only the overview and their own API keys.

## Pages

- **Overview**: counts, machines and users waiting for approval (approve them
  in place), recently active machines and the approval settings.
- **Machines**: every node with its owner or tags, addresses, status and
  routes. Search by name, address, user or tag and filter by status or user.
  Each machine has a detail page with its keys, routes (approve with a switch),
  sharing and the global exit node switch, plus rename, tag, expire and remove.
- **Users**: create, rename, approve, change the [role](roles.md) and delete
  users.
- **Keys**: pre-auth keys (create with reusable, ephemeral, pre-authorized and
  tags; expire; delete) and API keys. New keys are shown once, with a copy
  button.
- **Access controls**: the [policy](policy.md) in an editor with syntax
  highlighting. *Check* validates the draft against the server without saving;
  *Save* applies it. Leaving the page with unsaved changes asks first.
- **Settings**: the [device and user approval](approval.md) switches, the
  signed-in key's role and scopes, and the server's database health.

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
libraries, [Base UI](https://base-ui.com) components and Tailwind CSS, checked
by TypeScript, oxlint and oxfmt. Its API types are generated from the server's
OpenAPI document by `make web-generate` and committed. `bun run dev` in `web/`
starts a development server that proxies `/api` to a headscale on
`http://127.0.0.1:8080` (set `HEADSCALE_URL` to point elsewhere).

## Serving behind a reverse proxy

The console lives under `/admin/` on the same origin as the API, so a
[reverse proxy](integration/reverse-proxy.md) that forwards the whole host
needs no extra rules. It sends a `Content-Security-Policy` that only allows its
own origin; do not embed it in another site's frame.

If you prefer the console not to be reachable, block `/admin/` at the proxy.
Everything it does is also available through the API with the same key, so
this only removes the page, not the capability.
