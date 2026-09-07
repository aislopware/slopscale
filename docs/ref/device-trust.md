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
