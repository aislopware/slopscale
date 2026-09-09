import type { NodeDiagnosticKind } from "~/api/queries.ts";
import type { NodeClientWarning } from "~/api/schema.gen.ts";
import type { Tone } from "~/components/ui/status.tsx";

/**
 * How bad the client considers a warning, in the console's tones. The severity is the client's
 * word, and a newer client may invent one, so anything unknown reads as a warning rather than
 * disappearing into the neutral tone.
 */
const tones: Record<string, Tone> = {
  high: "danger",
  medium: "warning",
  low: "info",
};

export function warningTone(severity: string): Tone {
  return tones[severity] ?? "warning";
}

/** What each severity is called in a status: one word, whatever the client titled the warning. */
const severities: Record<string, string> = {
  high: "Unhealthy",
  medium: "Warning",
  low: "Notice",
};

/**
 * The state as one word. A status is a state, not a sentence: the client's own title, its text and
 * when it broke are the reason behind the word and belong in the popover beside it.
 */
export function severityLabel(severity: string): string {
  return severities[severity] ?? "Warning";
}

/**
 * Whether traffic is affected, in the words that go under the warning's own text. It is the first
 * thing an operator asks about a warning, so it is said rather than left to the severity.
 */
export function connectivityNote(warning: NodeClientWarning): string | undefined {
  return warning.impactsConnectivity ? "The client says this affects connectivity." : undefined;
}

/** What each dump the client hands over is called, in the order the section offers them. */
export const diagnosticLabels: Record<NodeDiagnosticKind, string> = {
  prefs: "Preferences",
  netmap: "Network map",
  metrics: "Metrics",
  goroutines: "Goroutines",
  sockstats: "Socket stats",
  "tka-log": "Tailnet lock log",
};

/**
 * What to call the file when the server named none. The server passes the client's own dump through
 * with a Content-Disposition, so this is only the fallback for a response without one.
 */
export function diagnosticFileName(nodeId: string, kind: NodeDiagnosticKind): string {
  return `slopscale-${kind}-${nodeId}.txt`;
}
