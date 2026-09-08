import type { Invite } from "~/api/queries.ts";
import { roleOptions } from "~/components/users/roles.ts";
import type { RoleOption } from "~/components/users/roles.ts";

/**
 * How long an invitation link works for. The API takes a Go duration and allows 720h at most, so
 * the console offers the three spans an operator asks for rather than a free-text field.
 */
export const inviteExpiries = ["24h", "168h", "720h"] as const;

export type InviteExpiry = (typeof inviteExpiries)[number];

/** The default the server itself applies when a request leaves the expiry out. */
export const defaultInviteExpiry: InviteExpiry = "168h";

const expiryLabels: Record<InviteExpiry, string> = {
  "24h": "1 day",
  "168h": "7 days",
  "720h": "30 days",
};

/** What the expiry select offers: a span in days against the duration the API takes. */
export const inviteExpiryOptions: readonly { value: InviteExpiry; label: string }[] =
  inviteExpiries.map((value) => ({ value, label: expiryLabels[value] }));

/** The label for a duration, for the select's own value; an unknown one is shown as it came. */
export function inviteExpiryLabel(value: string): string {
  const known = inviteExpiries.find((candidate) => candidate === value);

  return known === undefined ? value : expiryLabels[known];
}

/** Narrows what a select hands back; anything unknown falls back to the default span. */
export function toInviteExpiry(value: string | null): InviteExpiry {
  return inviteExpiries.find((known) => known === value) ?? defaultInviteExpiry;
}

/**
 * The roles an invitation may carry. There is exactly one owner and it is transferred, never handed
 * out, so the server refuses it here.
 */
export const invitableRoles: readonly RoleOption[] = roleOptions.filter(
  (option) => option.value !== "owner",
);

/** What an invitation is waiting on, which is what its row's badge says. */
export type InviteState = "pending" | "expired" | "accepted";

export function inviteState(invite: Invite): InviteState {
  if (invite.accepted) {
    return "accepted";
  }

  return invite.expired ? "expired" : "pending";
}

/**
 * The invitations the users page lists: an accepted one became a user row above, so listing it
 * again would say the same thing twice.
 */
export function openInvites(invites: readonly Invite[]): Invite[] {
  return invites.filter((invite) => !invite.accepted);
}
