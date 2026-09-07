# User roles

Every user has a role that bounds what the user may do through the admin API
and the management console. The vocabulary follows
[Tailscale's user roles](https://tailscale.com/docs/features/user-roles) minus
billing admin, which has no meaning on a self-hosted server, so the
Tailscale-compatible v2 API reports the same values the Tailscale ecosystem
expects.

A role never decides what a user's devices can reach. That remains the job of
the [policy](policy.md), which may address roles through
`autogroup:owner`, `autogroup:admin`, `autogroup:network-admin`,
`autogroup:it-admin` and `autogroup:auditor`.

## The roles

| Role            | Users, devices, keys, settings | Policy and routes | Webhooks, posture | Reads everything | Assigns roles |
| --------------- | ------------------------------ | ----------------- | ----------------- | ---------------- | ------------- |
| `owner`         | write                          | write             | write             | yes              | yes           |
| `admin`         | write                          | write             | write             | yes              | yes           |
| `network-admin` | read                           | write             | write             | yes              | no            |
| `it-admin`      | write                          | read              | write             | yes              | no            |
| `auditor`       | read                           | read              | read              | yes              | no            |
| `member`        | none                           | none              | none              | no               | no            |

"Users, devices, keys, settings" are the `users`, `devices:core`, `auth_keys`,
`oauth_keys` and `feature_settings` scopes of the v2 API; "policy and routes"
are `policy_file`, `devices:routes` and `dns`; webhooks and posture are
`webhooks` and `devices:posture_attributes`. The v1 API declares the same scopes on
its operations, so a credential means the same thing on both APIs. Run
`GET /api/v1/whoami` to see the role and permissions a credential carries.

## The owner

The tailnet has exactly one owner.

- The first user created on a fresh server becomes the owner, whether it is
  created with `headscale users create` or by the first OpenID Connect login.
- The owner's role changes only by transferring ownership: assigning `owner`
  to another user makes that user the owner and turns the previous owner into
  an admin.
- Only the owner can transfer ownership. Nobody can demote or delete the
  owner.

Servers upgraded from a version without roles have no owner: every existing
user is a `member`. Pick one with the CLI, which is bound only by the rules
above:

```console
headscale users set-role --name alice --role owner
```

## Assigning roles

Only the owner or an admin may assign roles, and nobody may change their own.

```console
headscale users set-role --name bob --role network-admin
headscale users list
```

Through the API, `POST /api/v1/user/{id}/role` with `{"role": "auditor"}`.
Roles show in `headscale users list`, in the v1 user object and in the v2
user object's `role` field, which `GET /api/v2/tailnet/-/users?role=admin`
filters on.

## API keys and roles

An API key may belong to a user, in which case the key is bounded by that
user's current role: demoting the user demotes every key the user holds.

```console
headscale apikeys create --user 3
```

- Any authenticated caller may mint a key for itself.
- Only the owner, an admin or the CLI over the socket may mint keys for
  other users or keys without a user.
- A key without a user is the historical all-access admin key. Keys created
  before roles existed keep working unchanged.
- A role-bounded caller lists, expires and deletes only its own keys.

OAuth clients created through the v2 API are narrowed the same way: a client
may not be granted a scope its creator lacks.

## Roles in the policy

The role autogroups select the personal (untagged) devices of every user
holding the role, and behave like `autogroup:member` wherever the policy
reasons about users. They work as sources, destinations, SSH sources and
destinations, and `nodeAttrs` targets.

```json title="policy.json"
{
  "grants": [
    {
      "src": ["autogroup:admin"],
      "dst": ["tag:prod-app-servers"],
      "ip": ["22"]
    }
  ],
  "ssh": [
    {
      "action": "accept",
      "src": ["autogroup:owner", "autogroup:network-admin"],
      "dst": ["autogroup:tagged"],
      "users": ["root"]
    }
  ]
}
```

Devices of the owner and admins additionally receive Tailscale's `is-admin`
capability, and the owner's devices `is-owner`, which clients use for the
admin affordances in their UI. Tagged devices carry neither.
