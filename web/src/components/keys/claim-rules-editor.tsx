import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { PlusIcon, TrashIcon } from "@phosphor-icons/react";
import { useId } from "react";
import type { ReactElement } from "react";

import { addClaimRule, removeClaimRule, updateClaimRule } from "~/components/keys/federated.ts";
import type { ClaimRuleEntry } from "~/components/keys/federated.ts";

/**
 * The group is named by `aria-labelledby` rather than a `legend`: a legend only names a fieldset
 * when it is its first child, and this one shares a row with the button that adds a rule.
 */
export function ClaimRulesEditor({
  entries,
  error,
  onChange,
}: {
  readonly entries: readonly ClaimRuleEntry[];
  /** What is wrong with the rows as they stand, shown under them rather than as a failed request. */
  readonly error?: string | undefined;
  readonly onChange: (entries: ClaimRuleEntry[]) => void;
}): ReactElement {
  const labelId = useId();
  const addRow = (): void => {
    onChange(addClaimRule(entries));
  };

  const removeRow = (id: string): void => {
    onChange(removeClaimRule(entries, id));
  };

  const updateRow = (id: string, patch: Partial<Pick<ClaimRuleEntry, "claim" | "value">>): void => {
    onChange(updateClaimRule(entries, id, patch));
  };

  return (
    <fieldset aria-labelledby={labelId} className="flex flex-col gap-2">
      <div className="flex items-center justify-between gap-3">
        <div>
          <span id={labelId} className="text-sm font-medium text-kumo-default">
            Custom claim rules
          </span>
          <p className="text-sm text-kumo-subtle">
            Additional claims that must match in the workload&apos;s OIDC token.
          </p>
        </div>
        <Button type="button" variant="secondary" size="sm" icon={PlusIcon} onClick={addRow}>
          Add rule
        </Button>
      </div>
      {entries.length === 0 ? (
        <p className="text-sm text-kumo-subtle">No custom claim rules defined.</p>
      ) : (
        <div className="flex flex-col gap-2">
          {entries.map((entry, index) => (
            <div key={entry.id} className="flex items-center gap-2">
              <Input
                aria-label={`Claim ${index + 1}`}
                placeholder="Claim (e.g. workflow)"
                value={entry.claim}
                onChange={(event) => {
                  updateRow(entry.id, { claim: event.target.value });
                }}
                className="min-w-0 flex-1"
              />
              <Input
                aria-label={`Value ${index + 1}`}
                placeholder="Value (e.g. Deploy)"
                value={entry.value}
                onChange={(event) => {
                  updateRow(entry.id, { value: event.target.value });
                }}
                className="min-w-0 flex-1"
              />
              <Button
                type="button"
                variant="ghost"
                shape="square"
                icon={TrashIcon}
                aria-label={`Remove rule ${index + 1}`}
                onClick={() => {
                  removeRow(entry.id);
                }}
              />
            </div>
          ))}
        </div>
      )}
      {error === undefined ? null : (
        <p role="alert" className="text-sm text-kumo-danger">
          {error}
        </p>
      )}
    </fieldset>
  );
}
