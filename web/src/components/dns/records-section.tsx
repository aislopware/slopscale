import { Button } from "@cloudflare/kumo/components/button";
import { PlusIcon } from "@phosphor-icons/react";
import { Fragment, useState } from "react";
import type { ReactElement } from "react";

import type { Dns } from "~/api/queries.ts";
import type { DnsRecord } from "~/api/schema.gen.ts";
import { EditToggle, editToggleId, focusEditToggle } from "~/components/dns/inline-row-editor.tsx";
import { ListCell, ListRow, ListTable } from "~/components/dns/list-table.tsx";
import type { DnsMutations } from "~/components/dns/mutations.ts";
import { RecordAddForm, RecordInlineEditor } from "~/components/dns/record-forms.tsx";
import { SearchInput } from "~/components/table/search-input.tsx";
import { TableToolbar } from "~/components/table/toolbar.tsx";
import { DialogContent, DialogRoot } from "~/components/ui/dialog.tsx";
import { Section, SectionEmpty } from "~/components/ui/section.tsx";

export function ExtraRecordsSection({
  dns,
  canEdit,
  mutations,
}: {
  readonly dns: Dns;
  readonly canEdit: boolean;
  readonly mutations: DnsMutations;
}): ReactElement {
  const [adding, setAdding] = useState(false);
  // The one row whose editor is open, by its place in the list.
  const [open, setOpen] = useState<number | null>(null);
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
                setAdding(true);
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
          actionsClassName="w-20 px-2"
          empty={emptyState(total, shown.length, () => {
            setQuery("");
          })}
        >
          {shown.map(({ record, index }) => (
            <Fragment key={`${record.name}:${record.type}:${record.value}`}>
              <RecordRow
                record={record}
                index={index}
                editable={editable}
                open={open === index}
                pending={pending}
                onToggle={() => {
                  setOpen(open === index ? null : index);
                }}
              />
              {open === index ? (
                <RecordInlineEditor
                  index={index}
                  record={record}
                  dns={dns}
                  mutations={mutations}
                  onClose={() => {
                    setOpen(null);
                    focusEditToggle(editToggleId("record", String(index)));
                  }}
                  onDeleted={() => {
                    setOpen(null);
                  }}
                />
              ) : null}
            </Fragment>
          ))}
        </ListTable>
      </Section>
      <DialogRoot open={adding} onOpenChange={setAdding}>
        <DialogContent
          size="xl"
          title="Add record"
          description="Machines resolve the name through MagicDNS. A and AAAA only."
        >
          <RecordAddForm
            dns={dns}
            mutations={mutations}
            onDone={() => {
              setAdding(false);
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

/** One extra record: its type, name and value, and its Edit button while the console owns them. */
function RecordRow({
  record,
  index,
  editable,
  open,
  pending,
  onToggle,
}: {
  readonly record: DnsRecord;
  readonly index: number;
  readonly editable: boolean;
  readonly open: boolean;
  readonly pending: boolean;
  readonly onToggle: () => void;
}): ReactElement {
  return (
    <ListRow>
      <ListCell className="font-mono text-sm">{record.type === "" ? "Auto" : record.type}</ListCell>
      <ListCell className="font-mono text-sm font-medium break-all">{record.name}</ListCell>
      <ListCell className="font-mono text-sm break-all text-kumo-subtle">{record.value}</ListCell>
      {editable ? (
        <ListCell actions className="w-20 px-2">
          <EditToggle
            id={editToggleId("record", String(index))}
            open={open}
            label={`record ${record.name}`}
            disabled={pending && !open}
            onToggle={onToggle}
          />
        </ListCell>
      ) : null}
    </ListRow>
  );
}
