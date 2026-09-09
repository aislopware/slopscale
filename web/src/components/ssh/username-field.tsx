import { Input } from "@cloudflare/kumo/components/input";
import { useId } from "react";
import type { ReactElement } from "react";

export interface UsernamePrefill {
  /** The account to put in the field now, or null to leave it as it is. */
  readonly insert: string | null;
  /** Whether the one chance to fill the field has been used, which the caller keeps for next time. */
  readonly consumed: boolean;
}

/**
 * What to do with the username field on this render. The field is filled once at most: the server's
 * guess and the machine's own suggestion are both offers, and whichever reaches the field first is
 * the one that meant something. Anything in the field — the server's answer, the machine's
 * suggestion or the operator's typing — uses that one chance up, so a field cleared afterwards
 * stays cleared instead of filling itself again with the account the operator just deleted.
 */
export function usernamePrefillStep(
  draft: string,
  suggestions: readonly string[],
  consumed: boolean,
): UsernamePrefill {
  if (consumed || draft !== "") {
    return { insert: null, consumed: true };
  }

  // One account is a suggestion; two are a choice, and choosing is the operator's.
  if (suggestions.length === 1) {
    return { insert: suggestions[0] ?? "", consumed: true };
  }

  return { insert: null, consumed: false };
}

/**
 * The account the terminal dials as, with the logins the machine suggests under it. They are a hint
 * rather than a list to choose from — the SSH policy decides, and an account the client did not
 * name may still work — so the field stays a plain text input with a `datalist` behind it. Kumo's
 * Autocomplete would put its own label above the control, and this field sits on the page header's
 * action row beside the buttons, where there is no line for one.
 */
export function UsernameField({
  value,
  suggestions,
  disabled,
  onValueChange,
}: {
  readonly value: string;
  readonly suggestions: readonly string[];
  readonly disabled: boolean;
  readonly onValueChange: (value: string) => void;
}): ReactElement {
  const listId = useId();

  return (
    <>
      <Input
        value={value}
        placeholder="Username"
        aria-label="Username"
        className="w-36"
        disabled={disabled}
        {...(suggestions.length === 0 ? {} : { list: listId })}
        onChange={(event) => {
          onValueChange(event.target.value);
        }}
      />
      {suggestions.length === 0 ? null : (
        /* An option in a datalist takes its value from its text, so the name is written once and
           the browser offers exactly what it shows. */
        <datalist id={listId}>
          {suggestions.map((username) => (
            <option key={username}>{username}</option>
          ))}
        </datalist>
      )}
    </>
  );
}
