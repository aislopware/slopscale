import { useEffect, useState } from "react";

import { useDebounced } from "~/lib/use-debounced.ts";

const settleMs = 300;

/**
 * The search box's own text. It takes the address's value whenever the address changes it from
 * outside, and hands typed text to `onSettle` once it stops changing, so the server is asked once
 * per pause rather than once per key.
 */
export function useSearchDraft(
  fromSearch: string,
  onSettle: (text: string) => void,
): [string, (next: string) => void] {
  const [draft, setDraft] = useState(fromSearch);
  const [seen, setSeen] = useState(fromSearch);
  const settled = useDebounced(draft, settleMs);

  if (seen !== fromSearch) {
    setSeen(fromSearch);
    setDraft(fromSearch);
  }

  useEffect(() => {
    if (settled === draft && settled !== fromSearch) {
      onSettle(settled);
    }
  }, [settled, draft, fromSearch, onSettle]);

  return [draft, setDraft];
}
