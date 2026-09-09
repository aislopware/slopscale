import { useState } from "react";

/**
 * The record a form opened on, held still while the record behind it changes. An edit form seeds
 * itself from a query and sends back what the operator changed; comparing against the record as it
 * stands after a refetch would turn a field somebody else changed into a change of the form's own.
 *
 * `id` says which record this is. It changes only when the form is looking at a different one,
 * which is the one case where starting again is right.
 */
export function useBaseline<Value>(value: Value, id: string): Value {
  const [held, setHeld] = useState({ id, value });

  if (held.id !== id) {
    setHeld({ id, value });

    return value;
  }

  return held.value;
}
