import { useEffect, useState } from "react";

/** The value as it was once it stopped changing for `delayMs`; the latest value until then. */
export function useDebounced<Value>(value: Value, delayMs: number): Value {
  const [settled, setSettled] = useState(value);

  useEffect(() => {
    const timer = setTimeout(() => {
      setSettled(value);
    }, delayMs);

    return (): void => {
      clearTimeout(timer);
    };
  }, [value, delayMs]);

  return settled;
}
