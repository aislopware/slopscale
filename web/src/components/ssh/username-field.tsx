import { Input } from "@cloudflare/kumo/components/input";
import { useId } from "react";
import type { ReactElement } from "react";

/**
 * Whether the machine's own answer should fill the field. Only once, only while the field is still
 * empty, and only when the machine named exactly one account: anything else would be the console
 * guessing, and filling a field the operator has already typed in — or the server has already
 * answered for — would take the choice back off them.
 */
export function shouldPrefillUsername(
  draft: string,
  suggestions: readonly string[],
  prefilled: boolean,
): boolean {
  return !prefilled && draft === "" && suggestions.length === 1;
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
