import { daysFromNow } from "~/lib/time.ts";

const millisPerHour = 3_600_000;

export type ExpiryChoice = "1h" | "1d" | "7d" | "30d" | "90d" | "1y";

export const expiryOptions: readonly { value: ExpiryChoice; label: string }[] = [
  { value: "1h", label: "1 hour" },
  { value: "1d", label: "1 day" },
  { value: "7d", label: "7 days" },
  { value: "30d", label: "30 days" },
  { value: "90d", label: "90 days" },
  { value: "1y", label: "1 year" },
];

/** Days each choice covers; the hour choice is too short for whole days. */
const expiryDays: Record<ExpiryChoice, number> = {
  "1h": 0,
  "1d": 1,
  "7d": 7,
  "30d": 30,
  "90d": 90,
  "1y": 365,
};

/** The absolute RFC 3339 expiry the API wants for a relative choice. */
export function expirationFor(choice: ExpiryChoice): string {
  if (choice === "1h") {
    return new Date(Date.now() + millisPerHour).toISOString();
  }

  return daysFromNow(expiryDays[choice]);
}
