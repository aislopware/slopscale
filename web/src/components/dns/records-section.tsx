import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { Input } from "@cloudflare/kumo/components/input";
import { Select } from "@cloudflare/kumo/components/select";
import { PencilSimpleIcon, PlusIcon, TrashIcon } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Dns } from "~/api/queries.ts";
import type { DnsRecord } from "~/api/schema.gen.ts";
import { ListCell, ListRow, ListTable } from "~/components/dns/list-table.tsx";
import {
  recordError,
  recordTypeLabel,
  recordTypes,
  withRecord,
  withoutRecord,
} from "~/components/dns/model.ts";
import type { RecordType } from "~/components/dns/model.ts";
import type { DnsMutations } from "~/components/dns/mutations.ts";
import { FormFooter } from "~/components/machines/dialogs.tsx";
import { SearchInput } from "~/components/table/search-input.tsx";
import { TableToolbar } from "~/components/table/toolbar.tsx";
import { DialogContent, DialogError, DialogRoot } from "~/components/ui/dialog.tsx";
import { RowMenu } from "~/components/ui/row-menu.tsx";
import { Section, SectionEmpty } from "~/components/ui/section.tsx";
import { toast } from "~/components/ui/toast.ts";

interface Editing {
  readonly index: number;
  readonly record: DnsRecord;
}

function isRecordType(value: string): value is RecordType {
  return (recordTypes as readonly string[]).includes(value);
}

export function ExtraRecordsSection({
  dns,
  canEdit,
  mutations,
}: {
  readonly dns: Dns;
  readonly canEdit: boolean;
  readonly mutations: DnsMutations;
}): ReactElement {
  const [dialog, setDialog] = useState<"closed" | "new" | Editing>("closed");
  const [query, setQuery] = useState("");
  const settings = dns.effective;
  const fromFile = dns.extraRecordsPath !== "";
  const editable = canEdit && !fromFile;
  const pending = mutations.set.isPending;

  const needle = query.trim().toLowerCase();
  const shown = settings.extraRecords
    .map((record, index) => ({ record, index }))
    .filter(
      ({ record }) =>
        needle === "" ||
        record.name.toLowerCase().includes(needle) ||
        record.value.toLowerCase().includes(needle),
    );
  const total = settings.extraRecords.length;

  return (
    <>
      <TableToolbar
        actions={
          editable ? (
            <Button
              variant="primary"
              icon={PlusIcon}
              onClick={() => {
                setDialog("new");
              }}
            >
              Add record
            </Button>
          ) : undefined
        }
      >
        <SearchInput value={query} placeholder="Search extra records" onValueChange={setQuery} />
      </TableToolbar>
      <Section
        title="Extra records"
        meta={total === 1 ? "1 record" : `${total} records`}
        description={
          fromFile
            ? `Read from ${dns.extraRecordsPath} on the server, which owns them while that setting is on.`
            : "Names MagicDNS answers on top of the machines, such as a service behind a reverse proxy."
        }
        panel={false}
      >
        <ListTable
          columns={[
            { key: "type", label: "Type", className: "w-24" },
            { key: "name", label: "Name", className: "w-[45%]" },
            { key: "value", label: "Content" },
          ]}
          actions={editable}
          empty={emptyState(total, shown.length, () => {
            setQuery("");
          })}
        >
          {shown.map(({ record, index }) => (
            <RecordRow
              key={`${record.name}:${record.type}:${record.value}`}
              record={record}
              editable={editable}
              pending={pending}
              onEdit={() => {
                setDialog({ index, record });
              }}
              onRemove={() => {
                mutations.apply(withoutRecord(settings, index), `Removed ${record.name}`);
              }}
            />
          ))}
        </ListTable>
      </Section>
      <DialogRoot
        open={dialog !== "closed"}
        onOpenChange={(open) => {
          if (!open) {
            setDialog("closed");
          }
        }}
      >
        <DialogContent
          size="base"
          title={dialog === "new" ? "Add record" : "Edit record"}
          description="Machines resolve the name through MagicDNS. A and AAAA only."
        >
          <RecordForm
            editing={dialog === "new" || dialog === "closed" ? null : dialog}
            dns={dns}
            mutations={mutations}
            onDone={() => {
              setDialog("closed");
            }}
          />
        </DialogContent>
      </DialogRoot>
    </>
  );
}

/** What the table says with no rows: nothing yet, or nothing that matches the search. */
function emptyState(total: number, shown: number, onClear: () => void): ReactElement | null {
  if (total === 0) {
    return (
      <SectionEmpty
        title="No extra records"
        description="The control server answers no names of its own."
      />
    );
  }

  if (shown === 0) {
    return (
      <SectionEmpty
        title="No record matches"
        description="Try a different name or address."
        contents={
          <Button variant="secondary" onClick={onClear}>
            Clear search
          </Button>
        }
      />
    );
  }

  return null;
}

function RecordForm({
  editing,
  dns,
  mutations,
  onDone,
}: {
  readonly editing: Editing | null;
  readonly dns: Dns;
  readonly mutations: DnsMutations;
  readonly onDone: () => void;
}): ReactElement {
  const [record, setRecord] = useState<DnsRecord>(
    editing?.record ?? { name: "", type: "A", value: "" },
  );
  const [touched, setTouched] = useState(false);

  // A failure from the last form must not greet this one. Only `reset` goes in the dependencies:
  // the mutation object is rebuilt on every render and `reset` itself notifies, so depending on
  // the whole object ran the effect after every render it caused, without end.
  const { reset } = mutations.set;

  useEffect(() => {
    reset();
  }, [reset]);
  const issue = recordError(record);
  const placeholderName =
    dns.baseDomain === "" ? "grafana.example.com" : `grafana.${dns.baseDomain}`;

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();
    setTouched(true);

    if (issue !== null) {
      return;
    }

    mutations.set.mutate(
      { body: withRecord(dns.effective, record, editing?.index) },
      {
        onSuccess: () => {
          toast.success(editing === null ? "Record added" : "Record updated");
          onDone();
        },
      },
    );
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <Input
        label="Name"
        value={record.name}
        placeholder={placeholderName}
        spellCheck={false}
        autoComplete="off"
        onChange={(event) => {
          setRecord({ ...record, name: event.target.value });
        }}
      />
      <div className="grid items-start gap-4 sm:grid-cols-[10rem_minmax(0,1fr)]">
        <Select
          className="w-full"
          label="Type"
          value={record.type}
          onValueChange={(value) => {
            setRecord({ ...record, type: value !== null && isRecordType(value) ? value : "" });
          }}
          renderValue={(value) => recordTypeLabel(isRecordType(value) ? value : "")}
        >
          {recordTypes.map((type) => (
            <Select.Option key={type} value={type}>
              {recordTypeLabel(type)}
            </Select.Option>
          ))}
        </Select>
        <Input
          label="Value"
          value={record.value}
          placeholder="100.64.0.3"
          spellCheck={false}
          autoComplete="off"
          onChange={(event) => {
            setRecord({ ...record, value: event.target.value });
          }}
          onBlur={() => {
            setTouched(true);
          }}
          {...(touched && issue !== null ? { error: issue } : {})}
        />
      </div>
      <DialogError
        message={mutations.set.isError ? errorMessage(mutations.set.error) : undefined}
      />
      <FormFooter
        label={editing === null ? "Add" : "Save"}
        pending={mutations.set.isPending}
        disabled={record.name.trim() === "" || record.value.trim() === ""}
      />
    </form>
  );
}

/** One extra record: its type, name and value, and its menu while the console owns the records. */
function RecordRow({
  record,
  editable,
  pending,
  onEdit,
  onRemove,
}: {
  readonly record: DnsRecord;
  readonly editable: boolean;
  readonly pending: boolean;
  readonly onEdit: () => void;
  readonly onRemove: () => void;
}): ReactElement {
  return (
    <ListRow>
      <ListCell className="font-mono text-sm">{record.type === "" ? "Auto" : record.type}</ListCell>
      <ListCell className="font-mono text-sm font-medium break-all">{record.name}</ListCell>
      <ListCell className="font-mono text-sm break-all text-kumo-subtle">{record.value}</ListCell>
      {editable ? (
        <ListCell actions>
          <RowMenu label={`Actions for record ${record.name}`} disabled={pending}>
            <DropdownMenu.Item icon={PencilSimpleIcon} onClick={onEdit}>
              Edit…
            </DropdownMenu.Item>
            <DropdownMenu.Separator />
            <DropdownMenu.Item icon={TrashIcon} variant="danger" onClick={onRemove}>
              Remove
            </DropdownMenu.Item>
          </RowMenu>
        </ListCell>
      ) : null}
    </ListRow>
  );
}
