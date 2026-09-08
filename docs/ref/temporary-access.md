# Temporary access

Access that ends on its own: a rule with an expiry, a group membership
with an expiry, and a request flow in which a user asks to join a group
for a while and an approver decides. Together they cover the on-call
window, the contractor's week and the "let me into prod for an hour"
without anyone having to remember to take the access away again.

## Expiring rules

An [access rule](access-control.md#access-rules) takes an optional
`expiresAt`. Past it the rule stops applying; it stays in the list,
marked expired, until someone extends or deletes it, so the record of
what was open is kept. A new expiry must be in the future; an expired
rule may still be edited as long as its expiry is kept, extended or
cleared.

```console
headscale access-rules create --name "Migration window" \
  --src 2 --dst 3 --protocol tcp --ports 5432 --expires 48h
headscale access-rules update -i 7 --name "Migration window" \
  --src 2 --dst 3 --protocol tcp --ports 5432 --expires 2026-09-30T18:00:00Z
```

The console's rule editor has an _Expires_ field, and the rule list says
when a rule expires or that it did.

## Expiring memberships

A machine or user can be added to a group until a time. The membership
counts until then and is deleted afterwards; the rest of the group is
untouched. Adding the same member again for good makes the membership
permanent; a group edit that keeps the member keeps its expiry.

```console
headscale groups add-node -i 4 --node 12 --expires 4h
headscale groups add-user -i 4 --user 3 --expires 2026-09-12T09:00:00Z
```

The console's _Groups_ dialog on a machine or user has an _Until_ field
for the groups being joined, and `GET /api/v1/group/{id}` lists the
temporary memberships under `expiries`.

## Access requests

A group marked requestable takes requests from users. A signed-in user
asks to join it for a duration, between five minutes and thirty days,
for one of their machines or for every machine they own, with a reason.
An approver, anyone with the `policy_file` scope, approves the request
for the duration asked or another one, or denies it, with a note the
requester sees. Approval adds the membership with its expiry and
rebuilds the policy at once; nobody decides their own request. A pending
request can be withdrawn by its requester; a decided one stays on record
until an approver deletes it.

Requests need a user behind the credential: a console session, or an
API key owned by a user. A member holds no admin scope and can still
file and follow their own requests. The console shows them under
_My access_, which every signed-in user has, and shows an approver the
queue on the _Requests_ page under _Access controls_, with the number
pending next to it in the sidebar.

```console
headscale groups create --name "Prod" --requestable
headscale access-requests create --group 4 --node 12 --duration 2h --reason "deploy"
headscale access-requests list --status pending
headscale access-requests approve -i 1 --duration 1h --note "one hour is enough"
headscale access-requests deny -i 2 --note "ask the team lead first"
headscale access-requests cancel -i 3
```

Through the API, `GET /api/v1/access-request/options` lists what the
caller may ask for, `POST /api/v1/access-request` files a request,
`GET /api/v1/access-request` lists them (every one for
`policy_file:read`, otherwise the caller's own; `?mine=true` and
`?status=pending` narrow), `POST /api/v1/access-request/{id}/approve`
and `/deny` decide, and `DELETE /api/v1/access-request/{id}` withdraws
or deletes.

## When access ends

The server checks every minute for rules and memberships whose time has
passed, deletes the memberships and rebuilds the policy without them and
without the expired rules, so a grant ends within a minute of its time
and the clients get a new map. A membership that ends is not a policy
change to a webhook subscriber; the approval was. Postures with a
schedule (see [Device trust](device-trust.md#schedules)) are the other
way to open access by the clock, for a window that repeats every week.

## Events and audit

The webhook events `accessRequestCreated`, `accessRequestApproved` and
`accessRequestDenied` carry the requester, the group, the machine, the
duration, the reason, the note and, once approved, the expiry, so a
Slack channel can follow the queue. The audit log records
`access_request.create`, `access_request.approve`, `access_request.deny`
and `access_request.cancel`, and the existing `group.member.add` and
`access_rule.*` actions carry the expiry.
