import type { LogStream } from "~/api/queries.ts";
import type { LogStreamRequestBody } from "~/api/schema.gen.ts";

export type Destination = LogStreamRequestBody["destination"];

export interface DestinationOption {
  readonly value: Destination;
  readonly label: string;
  readonly description: string;
  /** What the URL field asks for. */
  readonly placeholder: string;
  /** What the token field asks for; empty when the destination takes none. */
  readonly tokenLabel: string;
  readonly tokenRequired: boolean;
}

const fallbackDestination: DestinationOption = {
  value: "http",
  label: "HTTP (JSON)",
  description: "A JSON array of entries with a bearer token: Cribl, Panther, Vector, Fluent Bit.",
  placeholder: "https://collector.example.com/headscale",
  tokenLabel: "Bearer token",
  tokenRequired: false,
};

/** The sinks the server encodes for, in the server's order. */
export const destinationOptions: readonly DestinationOption[] = [
  fallbackDestination,
  {
    value: "splunk",
    label: "Splunk",
    description: "HTTP Event Collector events.",
    placeholder: "https://splunk.example.com:8088/services/collector/event",
    tokenLabel: "HEC token",
    tokenRequired: true,
  },
  {
    value: "elastic",
    label: "Elasticsearch",
    description: "A bulk request to the index named in the URL.",
    placeholder: "https://es.example.com:9200/headscale-audit/_bulk",
    tokenLabel: "API key",
    tokenRequired: false,
  },
  {
    value: "datadog",
    label: "Datadog",
    description: "The logs intake of your site.",
    placeholder: "https://http-intake.logs.datadoghq.com/api/v2/logs",
    tokenLabel: "API key",
    tokenRequired: true,
  },
  {
    value: "axiom",
    label: "Axiom",
    description: "A dataset's ingest endpoint.",
    placeholder: "https://api.axiom.co/v1/datasets/headscale/ingest",
    tokenLabel: "API token",
    tokenRequired: true,
  },
  {
    value: "loki",
    label: "Grafana Loki",
    description: "The push API; labels job, type, stream and tailnet.",
    placeholder: "https://loki.example.com/loki/api/v1/push",
    tokenLabel: "Bearer token",
    tokenRequired: false,
  },
];

export function destinationOption(value: string): DestinationOption {
  return destinationOptions.find((option) => option.value === value) ?? fallbackDestination;
}

/** The form's choice for a server value; anything unknown shows as HTTP. */
export function toDestination(value: string): Destination {
  return destinationOption(value).value;
}

export function destinationLabel(value: string): string {
  return destinationOption(value).label;
}

export type StreamState = "disabled" | "never" | "ok" | "failed";

const httpOk = 200;
const httpRedirect = 300;

/** How the stream is doing: off, untried, or the verdict of the last batch. */
export function streamState(
  stream: Pick<LogStream, "enabled" | "lastDeliveryStatus">,
): StreamState {
  if (!stream.enabled) {
    return "disabled";
  }

  const status = stream.lastDeliveryStatus;

  if (status === "") {
    return "never";
  }

  const code = Number(status);

  return Number.isInteger(code) && code >= httpOk && code < httpRedirect ? "ok" : "failed";
}

/** A short line for the list: the status code or the start of the error text. */
export function statusLabel(stream: Pick<LogStream, "lastDeliveryStatus">): string {
  const status = stream.lastDeliveryStatus;
  const maxLength = 40;

  if (status === "") {
    return "Never delivered";
  }

  if (Number.isInteger(Number(status))) {
    return `HTTP ${status}`;
  }

  return status.length > maxLength ? `${status.slice(0, maxLength)}…` : status;
}

/** "1,204 delivered · 3 dropped", with dropped shown only when there are any. */
export function countersLabel(stream: Pick<LogStream, "delivered" | "dropped">): string {
  const format = new Intl.NumberFormat();
  const delivered = `${format.format(stream.delivered)} delivered`;

  return stream.dropped === 0
    ? delivered
    : `${delivered} · ${format.format(stream.dropped)} dropped`;
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
    return "Enter a full URL, like https://collector.example.com/logs";
  }
}
