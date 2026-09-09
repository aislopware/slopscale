import { Button } from "@cloudflare/kumo/components/button";
import { Select } from "@cloudflare/kumo/components/select";
import { Tabs } from "@cloudflare/kumo/components/tabs";
import { PlusIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import type { User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import {
  attestationFilterLabels,
  attestationFilters,
  statusFilterLabels,
  statusFilters,
  toAttestationFilter,
  toStatusFilter,
} from "~/components/machines/filters.ts";
import type { AttestationFilter, StatusFilter } from "~/components/machines/filters.ts";
import { SearchInput } from "~/components/table/search-input.tsx";
import { countedTabs } from "~/components/table/tab-count.tsx";
import { TableToolbar } from "~/components/table/toolbar.tsx";
import { userLabel } from "~/lib/node.ts";

const attestationOptions = attestationFilters.map((value) => ({
  value,
  label: attestationFilterLabels[value],
}));

export interface MachinesToolbarProps {
  readonly me: Me;
  readonly query: string;
  readonly status: StatusFilter;
  /** The selected user id; "" for every user. */
  readonly user: string;
  /** Undefined while the caller may not read users, which hides the filter. */
  readonly users: readonly User[] | undefined;
  /** Every tag the tailnet uses; the filter is absent while nothing is tagged. */
  readonly tags: readonly string[];
  /** The selected tag; "" for every machine. */
  readonly tag: string;
  /** The selected attestation; the filter is absent while no machine reports one. */
  readonly attestation: AttestationFilter;
  /** Whether any machine has an attestation record at all. */
  readonly attestable: boolean;
  readonly counts: Record<StatusFilter, number>;
  /** Opens the page's "Add machine" dialog, which the empty state shares. */
  readonly onAddMachine: () => void;
  readonly onQueryChange: (value: string) => void;
  readonly onStatusChange: (value: StatusFilter) => void;
  readonly onUserChange: (value: string) => void;
  readonly onTagChange: (value: string) => void;
  readonly onAttestationChange: (value: AttestationFilter) => void;
}

/** The first row of the machines card: search, the status segments, the owner filter, add. */
export function MachinesToolbar({
  me,
  query,
  status,
  user,
  users,
  tags,
  tag,
  attestation,
  attestable,
  counts,
  onAddMachine,
  onQueryChange,
  onStatusChange,
  onUserChange,
  onTagChange,
  onAttestationChange,
}: MachinesToolbarProps): ReactElement {
  return (
    <TableToolbar actions={<AddMachine me={me} onAdd={onAddMachine} />}>
      {/* Narrower than the console's default search box, so the owner filter stays on this row. */}
      <SearchInput
        className="max-w-44"
        value={query}
        placeholder="Search machines"
        onValueChange={onQueryChange}
      />
      <Tabs
        variant="segmented"
        value={status}
        tabs={countedTabs(
          statusFilters.map((value) => ({ value, label: statusFilterLabels[value] })),
          (value) => counts[value],
        )}
        onValueChange={(value) => {
          onStatusChange(toStatusFilter(value));
        }}
      />
      {users === undefined ? null : (
        <Select
          aria-label="Filter by user"
          className="w-32"
          value={user}
          items={userOptions(users)}
          onValueChange={(value) => {
            handlePick(value, onUserChange);
          }}
        />
      )}
      {tags.length === 0 ? null : (
        <Select
          aria-label="Filter by tag"
          className="w-32"
          value={tag}
          items={tagFilterOptions(tags)}
          onValueChange={(value) => {
            handlePick(value, onTagChange);
          }}
        />
      )}
      {attestable ? (
        <Select
          aria-label="Filter by hardware attestation"
          className="w-36"
          value={attestation}
          items={attestationOptions}
          onValueChange={(value) => {
            handlePick(value, (picked) => {
              onAttestationChange(toAttestationFilter(picked));
            });
          }}
        />
      ) : null}
    </TableToolbar>
  );
}

/**
 * A machine joins by registering itself, so the console's part is handing out a pre-auth key. The
 * dialog belongs to the page, because the empty state opens the same one.
 */
function AddMachine({
  me,
  onAdd,
}: {
  readonly me: Me;
  readonly onAdd: () => void;
}): ReactElement | null {
  return can(me, "auth_keys") ? (
    <Button variant="primary" icon={PlusIcon} onClick={onAdd}>
      Add machine
    </Button>
  ) : null;
}

/**
 * A Kumo select hands back null while it cannot resolve its value to one of its items, which
 * happens on the first render of a filter that arrived in the URL. Taking that as a choice would
 * throw the filter away before the page had drawn it, so only a real pick is passed on; picking
 * "Any user" sends that item's own empty value.
 */
function handlePick(value: string | null, onPick: (value: string) => void): void {
  if (value !== null) {
    onPick(value);
  }
}

function userOptions(users: readonly User[]): { value: string; label: string }[] {
  return [
    { value: "", label: "Any user" },
    ...users.map((user) => ({ value: user.id, label: userLabel(user) })),
  ];
}

function tagFilterOptions(tags: readonly string[]): { value: string; label: string }[] {
  return [{ value: "", label: "Any tag" }, ...tags.map((tag) => ({ value: tag, label: tag }))];
}
