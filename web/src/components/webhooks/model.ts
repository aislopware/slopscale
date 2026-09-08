import type { Webhook } from "~/api/queries.ts";

export type ProviderType =
  | ""
  | "slack"
  | "mattermost"
  | "googlechat"
  | "discord"
  | "teams"
  | "telegram"
  | "ntfy"
  | "email";

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
  { value: "slack", label: "Slack", description: "An incoming webhook. The message goes as text." },
  {
    value: "mattermost",
    label: "Mattermost",
    description: "An incoming webhook. The message goes as text.",
  },
  {
    value: "googlechat",
    label: "Google Chat",
    description: "A space webhook. The message goes as text.",
  },
  {
    value: "discord",
    label: "Discord",
    description: "A channel webhook. The message goes as content.",
  },
  {
    value: "teams",
    label: "Microsoft Teams",
    description: "An incoming webhook or workflow. The message goes as text.",
  },
  {
    value: "telegram",
    label: "Telegram",
    description: "The Bot API's sendMessage URL with the chat in a chat_id query parameter.",
  },
  {
    value: "ntfy",
    label: "ntfy",
    description: "A topic URL. The message goes as the notification.",
  },
  {
    value: "email",
    label: "Email",
    description: "mailto: and the recipients. Sent through the server's SMTP settings.",
  },
];

/** What the URL field asks for, per provider. */
export interface UrlField {
  readonly label: string;
  readonly placeholder: string;
  readonly hint: string;
}

const defaultUrlField: UrlField = {
  label: "URL",
  placeholder: "https://ops.example.com/headscale",
  hint: "",
};

const urlFields: Partial<Record<ProviderChoice, UrlField>> = {
  telegram: {
    label: "Bot URL",
    placeholder: "https://api.telegram.org/bot<token>/sendMessage?chat_id=-100123",
    hint: "The token is in the URL. The chat_id query parameter names the chat.",
  },
  email: {
    label: "Recipients",
    placeholder: "mailto:ops@example.com, security@example.com",
    hint: "mailto: followed by one or more addresses. The server needs notifications.smtp configured.",
  },
  ntfy: { label: "Topic URL", placeholder: "https://ntfy.sh/headscale-ops", hint: "" },
};

export function urlField(choice: ProviderChoice): UrlField {
  return urlFields[choice] ?? defaultUrlField;
}

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
  nodeSuspended: "A machine was suspended",
  nodeUnsuspended: "A machine's suspension was lifted",
  nodeDeleted: "A machine was removed",
  policyUpdate: "The policy changed",
  userCreated: "A user was created",
  userNeedsApproval: "A user is waiting for approval",
  userApproved: "A user was approved",
  userRoleUpdated: "A user's role changed",
  userDeleted: "A user was deleted",
  accessRequestCreated: "A member asked for temporary access",
  accessRequestApproved: "An access request was approved",
  accessRequestDenied: "An access request was denied",
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

const mailto = "mailto:";

/** The host of the URL, the part an operator recognises at a glance; the recipients for email. */
export function urlHost(url: string): string {
  if (url.startsWith(mailto)) {
    return url.slice(mailto.length).trim();
  }

  try {
    return new URL(url).host;
  } catch {
    return url;
  }
}

export function urlError(url: string, choice: ProviderChoice = "generic"): string | null {
  const trimmed = url.trim();

  if (trimmed === "") {
    return null;
  }

  if (choice === "email") {
    return trimmed.startsWith(mailto) && trimmed.length > mailto.length
      ? null
      : "Enter mailto: followed by the recipients";
  }

  try {
    const parsed = new URL(trimmed);

    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
      return "The URL must start with http:// or https://";
    }

    if (choice === "telegram" && parsed.searchParams.get("chat_id") === null) {
      return "Add the chat as a chat_id query parameter";
    }

    return null;
  } catch {
    return "Enter a full URL, like https://example.com/hook";
  }
}

const msPerSecond = 1000;
const secondsPerMinute = 60;

/** "120 ms", "2.5 s", "1m 12s": the time a delivery took, retries included. */
export function formatDuration(ms: number): string {
  if (ms < msPerSecond) {
    return `${ms} ms`;
  }

  const seconds = ms / msPerSecond;

  if (seconds < secondsPerMinute) {
    return `${Number.isInteger(seconds) ? seconds : seconds.toFixed(1)} s`;
  }

  const minutes = Math.floor(seconds / secondsPerMinute);
  const rest = Math.round(seconds - minutes * secondsPerMinute);

  return `${minutes}m ${rest}s`;
}

/** "3 events" for the compact list, where the chips have no room. */
export function countEvents(total: number): string {
  return total === 1 ? "1 event" : `${total} events`;
}
