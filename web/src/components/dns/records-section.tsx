import { Badge } from "@cloudflare/kumo/components/badge";
import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { Select } from "@cloudflare/kumo/components/select";
import { PencilSimpleIcon, PlusIcon, TrashIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Dns } from "~/api/queries.ts";
import type { DnsRecord } from "~/api/schema.gen.ts";
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
import { DialogContent, DialogError, DialogRoot } from "~/components/ui/dialog.tsx";
import { Section, SectionEmpty, SectionRow } from "~/components/ui/section.tsx";
import { toast } from "~/components/ui/toast.ts";

const iconSize = 16;

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
  const settings = dns.effective;
  const fromFile = dns.extraRecordsPath !== "";
  const editable = canEdit && !fromFile;
  const pending = mutations.set.isPending;

  return (
    <Section
      title="Extra records"
      description={
        fromFile
          ? `Read from ${dns.extraRecordsPath} on the server, which owns them while that setting is on.`
          : "Names MagicDNS answers on top of the machines, such as a service behind a reverse proxy."
      }
      bodyClassName="p-0"
      {...(editable
        ? {
            actions: (
              <Button
                variant="secondary"
                icon={PlusIcon}
                onClick={() => {
                  setDialog("new");
                }}
              >
                Add record
              </Button>
            ),
          }
        : {})}
    >
      {settings.extraRecords.length === 0 ? (
        <SectionEmpty
          title="No extra records"
          description="Add a record to answer a name from the control server itself."
        />
      ) : (
        settings.extraRecords.map((record, index) => (
          <SectionRow
            key={`${record.name}:${record.type}:${record.value}`}
            className="flex items-center justify-between gap-4 py-2.5"
          >
            <div className="grid min-w-0 flex-1 items-center gap-x-4 gap-y-0.5 sm:grid-cols-[minmax(0,1.4fr)_auto_minmax(0,1fr)]">
              <span className="min-w-0 font-mono text-sm font-medium break-all text-kumo-strong">
                {record.name}
              </span>
              <span>
                <Badge variant="secondary">{record.type === "" ? "Auto" : record.type}</Badge>
              </span>
              <span className="min-w-0 font-mono text-sm break-all text-kumo-subtle">
                {record.value}
              </span>
            </div>
            {editable ? (
              <span className="flex shrink-0 items-center">
                <Button
                  variant="ghost"
                  shape="square"
                  size="sm"
                  icon={<PencilSimpleIcon size={iconSize} />}
                  aria-label={`Edit record ${record.name}`}
                  disabled={pending}
                  onClick={() => {
                    setDialog({ index, record });
                  }}
                />
                <Button
                  variant="ghost"
                  shape="square"
                  size="sm"
                  icon={<TrashIcon size={iconSize} />}
                  aria-label={`Remove record ${record.name}`}
                  disabled={pending}
                  onClick={() => {
                    mutations.apply(withoutRecord(settings, index), `Removed ${record.name}`);
                  }}
                />
              </span>
            ) : null}
          </SectionRow>
        ))
      )}
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
          description="Machines resolve the name through MagicDNS. Only A and AAAA records are served."
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
    </Section>
  );
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
    editing?.record ?? { name: "", type: "", value: "" },
  );
  const [touched, setTouched] = useState(false);
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
