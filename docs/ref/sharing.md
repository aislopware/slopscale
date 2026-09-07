# Node sharing

A user can give another user's personal devices access to one of their nodes
without an administrator editing the policy, following
[Tailscale's node sharing](https://tailscale.com/docs/features/node-sharing).
A share is one way: the sharee's devices may reach the shared node, the shared
node gets no access back, and nothing else the owner has (other nodes, routes,
tags) travels with it.

## How a share takes effect

The policy decides what a share allows. The `autogroup:shared` source stands,
for each destination node, for the personal devices of the users that node is
shared with:

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

With this grant, every node that has been shared admits its sharees' devices
on every port. The destination is always narrowed to the shared node itself,
so `dst` only says _which_ shared nodes the rule covers (here: any personal
device) and `ip` on which ports. `autogroup:shared` is valid as a source in
ACLs, grants and SSH rules, not as a destination, not together with an
`autogroup:self` destination and not in a `via` grant.

Without a policy every node already sees every other, and a policy that never
names `autogroup:shared` ignores shares entirely.

A sharee's device sees the shared node as a peer with the owner as its user and
`Sharer` set to the owner, the way Tailscale marks nodes shared into a tailnet,
so the client lists it under the sharing user. The shared node sees the
sharee's device as a peer but has no rule towards it.

## Sharing a node

The owner of a node, or anyone holding the `devices` scope, shares it with a
user by id:

```console
$ headscale nodes share --identifier 7 --user 3
$ headscale nodes unshare --identifier 7 --user 3
```

The same operations are `POST /api/v1/node/{id}/share` with `{"userId": "3"}`
and `DELETE /api/v1/node/{id}/share/{userId}`. A node's `sharedWith` field
lists the ids of the users it is shared with. Sharing a node with its owner is
rejected, sharing it twice with the same user is a conflict, and deleting a
user or a node removes its shares.
