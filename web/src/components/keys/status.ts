import type { ApiKey, PreAuthKey } from "~/api/queries.ts";
import { isPast, parseTime } from "~/lib/time.ts";

/** A key is spent, past its expiry, or still good for a registration. */
export type KeyStatus = "active" | "used" | "expired";

/** Active first: the rows an operator can still act on belong at the top. */
export const statusOrder: Record<KeyStatus, number> = { active: 0, used: 1, expired: 2 };

export function preAuthKeyStatus(authKey: PreAuthKey): KeyStatus {
  if (authKey.used) {
    return "used";
  }

  return isPast(parseTime(authKey.expiration)) ? "expired" : "active";
}

/** API keys are never "used up"; they only run out of time. */
export function apiKeyStatus(apiKey: ApiKey): KeyStatus {
  return isPast(parseTime(apiKey.expiration)) ? "expired" : "active";
}

const millisPerHour = 3_600_000;
const hoursPerDay = 24;
/** Under a day left is worth flagging: the key stops working without anyone touching it. */
const soonMillis = hoursPerDay * millisPerHour;

export function expiresSoon(date: Date, now: Date = new Date()): boolean {
  return date.getTime() - now.getTime() < soonMillis;
}
