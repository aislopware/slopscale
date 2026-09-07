import type { Webhook } from "~/api/queries.ts";

export type ProviderType = "" | "slack" | "mattermost" | "googlechat" | "discord";

/**
 * What the form picks from: the server's empty provider is "generic" here, since a select cannot
 * hold "".
 */
export type ProviderChoice = "generic" | Exclude<ProviderType, "">;

export interface ProviderOption {
  readonly value: ProviderChoice;
  readonly label: string;
  readonly description: string;
}

/** The delivery shapes the server knows; the first is the signed JSON array. */
export const providerOptions: readonly ProviderOption[] = [
  {
    value: "generic",
    label: "Generic (signed JSON)",
    description: "The full event array with a Tailscale-Webhook-Signature header.",
  },
  { value: "slack", label: "Slack", description: "An incoming webhook; the message as text." },
  {
    value: "mattermost",
    label: "Mattermost",
    description: "An incoming webhook; the message as text.",
  },
  {
    value: "googlechat",
    label: "Google Chat",
    description: "A space webhook; the message as text.",
  },
  { value: "discord", label: "Discord", description: "A channel webhook; the message as content." },
];

/** The form's choice for a server value; anything unknown shows as generic. */
export function toChoice(value: string): ProviderChoice {
  return providerOptions.find((option) => option.value === value)?.value ?? "generic";
}

/** The server value for a form choice. */
export function toProvider(choice: ProviderChoice): ProviderType {
  return choice === "generic" ? "" : choice;
}

export function providerLabel(value: string): string {
  return providerOptions.find((option) => option.value === toChoice(value))?.label ?? value;
}

/** What each event type means, for the picker; the server's list is the source of truth. */
const eventHints: Readonly<Record<string, string>> = {
  nodeCreated: "A machine registered",
  nodeNeedsApproval: "A machine is waiting for approval",
  nodeApproved: "A machine was approved",
  nodeKeyExpired: "A machine's key expired",
  nodeDeleted: "A machine was removed",
  policyUpdate: "The policy changed",
  userCreated: "A user was created",
  userNeedsApproval: "A user is waiting for approval",
  userApproved: "A user was approved",
  userRoleUpdated: "A user's role changed",
  userDeleted: "A user was deleted",
};

export function eventHint(type: string): string {
  return eventHints[type] ?? "";
}

export type DeliveryState = "never" | "ok" | "failed";

const httpOk = 200;
const httpRedirect = 300;

/**
 * How the last delivery went: a 2xx is a success, anything else the receiver's or the network's
 * fault.
 */
export function deliveryState(webhook: Pick<Webhook, "lastDeliveryStatus">): DeliveryState {
  const status = webhook.lastDeliveryStatus;

  if (status === "") {
    return "never";
  }

  const code = Number(status);

  return Number.isInteger(code) && code >= httpOk && code < httpRedirect ? "ok" : "failed";
}

/** A short line for the list: the status code or the start of the error text. */
export function deliveryLabel(webhook: Pick<Webhook, "lastDeliveryStatus">): string {
  const status = webhook.lastDeliveryStatus;
  const maxLength = 40;

  if (status === "") {
    return "Never delivered";
  }

  if (Number.isInteger(Number(status))) {
    return `HTTP ${status}`;
  }

  return status.length > maxLength ? `${status.slice(0, maxLength)}…` : status;
}

/** The host of the URL, the part an operator recognises at a glance. */
export function urlHost(url: string): string {
  try {
    return new URL(url).host;
  } catch {
    return url;
  }
}

export function urlError(url: string): string | null {
  const trimmed = url.trim();

  if (trimmed === "") {
    return null;
  }

  try {
    const parsed = new URL(trimmed);

    return parsed.protocol === "http:" || parsed.protocol === "https:"
      ? null
      : "The URL must start with http:// or https://";
  } catch {
    return "Enter a full URL, like https://example.com/hook";
  }
}
