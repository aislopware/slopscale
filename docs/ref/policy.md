# Policy

Headscale implements a large portion of Tailscale's [policy
features](https://tailscale.com/docs/features/tailnet-policy-file), most notably access control based on
[ACLs](https://tailscale.com/docs/features/access-control/acls) and
[Grants](https://tailscale.com/docs/features/access-control/grants) or [Tailscale
SSH](https://tailscale.com/docs/features/tailscale-ssh). See [limitations](#limitations) to learn about missing features
and notable implementation differences between Headscale and Tailscale.

Headscale uses the same [huJSON](https://github.com/tailscale/hujson) based file format as Tailscale. By default, no
policy is loaded which means that Headscale allows all traffic between nodes. To start using a policy file[^1], specify
its path in the `policy.path` key in the [configuration file](configuration.md).

Headscale needs to be reloaded to pick up changes to the policy file. Either reload Headscale via its systemd service
(`sudo systemctl reload headscale`) or by sending a SIGHUP signal (`sudo kill -HUP $(pidof headscale)`) to the main
process. Headscale logs the result of policy processing after each reload.

Please have a look at Tailscale's policy related documentation to learn more:

- [Tailscale policy file](https://tailscale.com/docs/features/tailnet-policy-file): A description of supported sections
  within the policy file along with links to syntax references for each section.
- [ACLs](https://tailscale.com/docs/features/access-control/acls): How to configure access control using ACLs.
- [Grants](https://tailscale.com/docs/features/access-control/grants): Introduction to Grants with links to [syntax
  reference](https://tailscale.com/docs/reference/syntax/grants),
  [examples](https://tailscale.com/docs/reference/examples/grants) and a [migration guide from ACLs to
  Grants](https://tailscale.com/docs/reference/migrate-acls-grants).

## Getting started

Headscale supports both [ACLs](https://tailscale.com/docs/features/access-control/acls) and
[Grants](https://tailscale.com/docs/features/access-control/grants) to write an access control policy. We recommend the
use of Grants since ACLs are considered legacy and will not receive new features by Tailscale.

### Allow All

If you define a policy file but completely omit the `"acls"` or `"grants"` section, Headscale will default to an [allow
all](https://tailscale.com/docs/reference/examples/acls#allow-all-default-acl) policy. This means all devices connected
to your tailnet will be able to communicate freely with each other.

```json title="policy.json"
{}
```

### Deny All

To [prevent all communication within your tailnet](https://tailscale.com/docs/reference/examples/acls#deny-all), you can
include an empty array for the `"grants"` section in your policy file.

```json title="policy.json"
{
  "grants": []
}
```

### More examples

- See our documentation on [subnet routers](routes.md#subnet-router) and [exit nodes](routes.md#exit-node) to learn how
  to restrict their use or how to automatically approve them.
- The Tailscale documentation provides a large collection of configuration examples:
    - [ACL examples](https://tailscale.com/docs/reference/examples/acls)
    - [Grants examples](https://tailscale.com/docs/reference/examples/grants)
    - [SSH configuration](https://tailscale.com/docs/features/tailscale-ssh#configure-tailscale-ssh)
    - [Define a tag](https://tailscale.com/docs/features/tags#define-a-tag)

______________________________________________________________________

## Limitations

- [Device postures](https://tailscale.com/docs/features/device-posture) are supported through `postures`,
  `srcPosture` and `defaultSrcPosture`; see [Device trust](device-trust.md#postures-in-the-policy-file). Postures in the
  file have no schedule.
- [IP sets](https://tailscale.com/docs/features/tailnet-policy-file/ip-sets) aren't supported.
- A subset of [Autogroups](#autogroups) are available.

## Autogroups

Headscale supports several [Autogroups](https://tailscale.com/docs/reference/targets-and-selectors#autogroups) that
automatically include users, destinations, or devices with specific properties. Autogroups provide a convenient way to
write policy rules without manually listing individual users or devices.

### [`autogroup:internet`](https://tailscale.com/docs/reference/targets-and-selectors#autogroupinternet)

Allows access to the internet through [exit nodes](routes.md#exit-node). Can only be used in policy destinations.

```json title="policy.json"
{
  "grants": [
    {
      "src": ["alice@"],
      "dst": ["autogroup:internet"],
      "ip": ["*"]
    }
  ]
}
```

### [`autogroup:member`](https://tailscale.com/docs/reference/targets-and-selectors#autogrouprole)

Includes all [personal (untagged) devices](registration.md/#identity-model).

```json title="policy.json"
{
  "grants": [
    {
      "src": ["autogroup:member"],
      "dst": ["tag:prod-app-servers"],
      "ip": ["80,443"]
    }
  ]
}
```

### [`autogroup:tagged`](https://tailscale.com/docs/reference/targets-and-selectors#autogrouptagged)

Includes all devices that [have at least one tag](registration.md/#identity-model).

```json title="policy.json"
{
  "grants": [
    {
      "src": ["autogroup:tagged"],
      "dst": ["tag:monitoring"],
      "ip": ["9090"]
    }
  ]
}
```

### [`autogroup:owner`, `autogroup:admin`, `autogroup:network-admin`, `autogroup:it-admin`, `autogroup:auditor`](https://tailscale.com/docs/reference/targets-and-selectors#autogrouprole)

Include the personal (untagged) devices of every user holding that
[role](roles.md). Usable wherever `autogroup:member` is.

```json title="policy.json"
{
  "grants": [
    {
      "src": ["autogroup:admin"],
      "dst": ["tag:prod-app-servers"],
      "ip": ["22"]
    }
  ]
}
```

### [`autogroup:shared`](https://tailscale.com/docs/reference/targets-and-selectors#autogroupshared)

For each destination node, the personal devices of the users the node has been
[shared](sharing.md) with. Can only be used in policy sources. The destination
is narrowed to the shared node itself, so the rule below lets sharees reach the
nodes shared with them and nothing else.

```json title="policy.json"
{
  "grants": [
    {
      "src": ["autogroup:shared"],
      "dst": ["autogroup:member"],
      "ip": ["*"]
    }
  ]
}
```

### [`autogroup:self`](https://tailscale.com/docs/reference/targets-and-selectors#autogroupself)

Includes devices where the same user is authenticated on both the source and destination. Does not include tagged
devices. Can only be used in policy destinations.

```json title="policy.json"
{
  "grants": [
    {
      "src": ["autogroup:member"],
      "dst": ["autogroup:self"],
      "ip": ["*"]
    }
  ]
}
```

!!! warning "The current implementation of `autogroup:self` is inefficient"

    Using `autogroup:self` may cause performance degradation on the Headscale coordinator server in large deployments,
    as filter rules must be compiled per-node rather than globally and the current implementation is not very efficient.

    If you experience performance issues, consider using more specific policy rules or limiting the use of
    `autogroup:self`.

    ```json title="policy.json"
    {
      "grants": [
        // The following rules allow internal users to communicate with their
        // own nodes in case autogroup:self is causing performance issues.
        {
          "src": ["boss@"],
          "dst": ["boss@"],
          "ip": ["*"]
        },
        {
          "src": ["dev1@"],
          "dst": ["dev1@"],
          "ip": ["*"]
        },
        {
          "src": ["intern1@"],
          "dst": ["intern1@"],
          "ip": ["*"]
        }
      ]
    }
    ```

### [`autogroup:nonroot`](https://tailscale.com/docs/reference/targets-and-selectors#other-built-in-targets)

Used in Tailscale SSH rules to allow access to any user except root. Can only be used in the `users` field of SSH rules.

```json title="policy.json"
{
  "ssh": [
    {
      "action": "accept",
      "src": ["autogroup:member"],
      "dst": ["autogroup:self"],
      "users": ["autogroup:nonroot"]
    }
  ]
}
```

### [`autogroup:danger-all`](https://tailscale.com/docs/reference/targets-and-selectors#autogroupdanger-all)

This autogroup resolves to all IP addresses (`0.0.0.0/0` and `::/0`) which also includes all IP addresses outside the
standard Tailscale IP ranges. This autogroup can only be used as source.

## Node Attributes

[Node attributes](https://tailscale.com/docs/reference/syntax/policy-file#node-attributes) allow for device-specific
configuration and attributes. At least the following node attributes are currently supported by Headscale[^2]:

- `drive:access`, `drive:share`: [Taildrive support](https://tailscale.com/docs/features/taildrive).
- `nextdns:<profile>`, `nextdns:no-device-info`: [NextDNS integration](https://tailscale.com/docs/integrations/nextdns).
  Be sure to set NextDNS as global resolver in the [configuration](configuration.md).
- `magicdns-aaaa`: Respond to AAAA queries on the local [MagicDNS](https://tailscale.com/docs/features/magicdns)
  resolver at 100.100.100.100.
- `disable-ipv4`: Selectively disable IPv4 for specfic nodes. This is may be useful to workaround [CGNat
  conflicts](https://tailscale.com/docs/reference/troubleshooting/network-configuration/cgnat-conflicts).
- `randomize-client-port`: Allocate a [random port for WireGuard
  traffic](https://tailscale.com/docs/reference/syntax/policy-file#randomizeclientport) instead of the static default
  port 41641.
- `disable-captive-portal-detection`: [Disable automatic captive portal
  detection](https://tailscale.com/docs/integrations/captive-portals#disable-captive-portal-detection).

```json title="policy.json"
{
  "nodeAttrs": [
    {
      // Enable MagicDNS AAAA records for all nodes
      "target": ["*"]
      "attr": ["magicdns-aaaa"]
    }
  ]
}
```

An entry's `app` field carries [application capabilities with
data](https://tailscale.com/docs/reference/syntax/policy-file#app-capabilities): each key is a domain-qualified
capability name and its values are JSON objects the targets receive verbatim in their node capability map. This is how
[app connectors](https://tailscale.com/docs/features/app-connectors) are defined: the `tailscale.com/app-connectors`
entries name the connector tags and the domains they serve, and a client that carries them routes those domains through
the connector, which learns and advertises the routes. Approve the connector's routes with an
[auto approver](routes.md) for its tag or by hand.

```json title="policy.json"
{
  "nodeAttrs": [
    {
      "target": ["autogroup:member"],
      "app": {
        "tailscale.com/app-connectors": [
          { "name": "github", "connectors": ["tag:appc"], "domains": ["github.com", "*.github.com"] }
        ]
      }
    }
  ]
}
```

### IP pools

`ipPool` numbers new nodes from a range of your choosing, as Tailscale's
[IP pools](https://tailscale.com/kb/1304/ip-pool) do. The target must be a
user, group, tag or autogroup, because the pool is chosen before the node has
an address. The first grant that names the node's user or tags wins, the
first listed pool with a free address is used, and a node keeps its address
when the policy changes later. Pools must lie within the CGNAT range, outside
the ranges Tailscale reserves, and within `prefixes.v4`; a full pool refuses
the registration instead of falling back to the default range. Only IPv4 is
pooled.

```json title="policy.json"
{
  "nodeAttrs": [
    { "target": ["group:dev"], "ipPool": ["100.85.0.0/16"] },
    { "target": ["tag:server"], "ipPool": ["100.86.0.0/24", "100.87.0.0/24"] }
  ]
}
```

## Network-wide policy options

The following options are applied for the entire tailnet. Consider [node attributes](#node-attributes) for a more
fine-grained configuration instead.

- `randomizeClientPort`: Allocate a [random port for WireGuard
  traffic](https://tailscale.com/docs/reference/syntax/policy-file#randomizeclientport) instead of the static default
  port 41641.

```json title="policy.json"
{
  // Use a random WireGuard port for the entire tailnet
  "randomizeClientPort": true
}
```

[^1]: Headscale also allows to store the policy in the database. This is typically only required in case a [web
    interface](integration/web-ui.md) is used.

[^2]: Other key-only node attributes can be used as well. Find them in the client source code with `grep -E '^\s+NodeAttr\w+' tailcfg/tailcfg.go` or by using [GitHub code search (requires
    login)](https://github.com/search?q=repo%3Atailscale%2Ftailscale%20language%3Ago%20path%3Atailcfg%2Ftailcfg.go%20symbol%3A%2FNodeAttr%5Cw%2B%2F&type=code).
