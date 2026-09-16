import { useEffect, useState } from "react";

import { minuteSeconds } from "~/lib/time.ts";

const millisecond = 1000;

/** Often enough that a grant does not sit "in effect" for long after it ended. */
export const clockTick = (minuteSeconds / 2) * millisecond;

/**
 * The current time, re-read on a timer, for the things that go stale on their own: temporary access
 * ends without anybody touching it, so a page left open must stop offering to revoke a grant that
 * has already run out. Everything else on a page is only as old as its last fetch, and a query
 * refetch re-renders by itself.
 */
export function useNow(every: number = clockTick): Date {
  const [now, setNow] = useState(() => new Date());

  useEffect(() => {
    const timer = setInterval((): void => {
      setNow(new Date());
    }, every);

    return (): void => {
      clearInterval(timer);
    };
  }, [every]);

  return now;
}
