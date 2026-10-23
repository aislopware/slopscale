import { Input } from "@cloudflare/kumo/components/input";
import { Select } from "@cloudflare/kumo/components/select";
import { useEffect, useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Dns } from "~/api/queries.ts";
import type { DnsRecord } from "~/api/schema.gen.ts";
import { InlineRowEditor } from "~/components/dns/inline-row-editor.tsx";
import {
  recordNameError,
  recordTypeLabel,
  recordTypes,
  recordValueError,
  withRecord,
  withoutRecord,
} from "~/components/dns/model.ts";
import type { RecordType } from "~/components/dns/model.ts";
import type { DnsMutations } from "~/components/dns/mutations.ts";
import { FormFooter } from "~/components/machines/dialogs.tsx";
import { DialogError } from "~/components/ui/dialog.tsx";
import { fieldError } from "~/components/ui/field-error.ts";
import { toast } from "~/components/ui/toast.ts";

/** The extra records table's columns: type, name, content, and the Edit button. */
const columnCount = 4;

const emptyRecord: DnsRecord = { name: "", type: "A", value: "" };

function isRecordType(value: string): value is RecordType {
  return (recordTypes as readonly string[]).includes(value);
}

interface RecordIssues {
  readonly name: string | null;
  readonly value: string | null;
}

/**
 * Holds a record draft and decides which of its errors show: format errors once a field is left,
 * then live; an empty field only after a save was tried.
 */
function useRecordDraft(initial: DnsRecord): {
  readonly record: DnsRecord;
  readonly update: (patch: Partial<DnsRecord>) => void;
  readonly issues: RecordIssues;
  readonly shown: RecordIssues;
  readonly leave: (field: "name" | "value") => void;
  readonly markSubmitted: () => void;
  readonly dirty: boolean;
} {
  const [record, setRecord] = useState(initial);
  const [left, setLeft] = useState<ReadonlySet<"name" | "value">>(new Set());
  const [submitted, setSubmitted] = useState(false);
  const issues = { name: recordNameError(record), value: recordValueError(record) };
  const show = (field: "name" | "value"): string | null =>
    submitted || (left.has(field) && record[field].trim() !== "") ? issues[field] : null;

  return {
    record,
    update: (patch) => {
      setRecord((current) => ({ ...current, ...patch }));
    },
    issues,
    shown: { name: show("name"), value: show("value") },
    leave: (field) => {
      setLeft((current) => new Set([...current, field]));
    },
    markSubmitted: () => {
      setSubmitted(true);
    },
    dirty: JSON.stringify(record) !== JSON.stringify(initial),
  };
}

/** Type, name and value on one line, as the dashboard lays out a DNS record. */
function RecordFields({
  record,
  shown,
  baseDomain,
  onChange,
  onLeave,
}: {
  readonly record: DnsRecord;
  readonly shown: RecordIssues;
  readonly baseDomain: string;
  readonly onChange: (patch: Partial<DnsRecord>) => void;
  readonly onLeave: (field: "name" | "value") => void;
}): ReactElement {
  return (
    <div className="grid items-start gap-4 sm:grid-cols-[10rem_minmax(0,3fr)_minmax(0,2fr)]">
      <Select
        className="w-full"
        label="Type"
        value={record.type}
        onValueChange={(value) => {
          onChange({ type: value !== null && isRecordType(value) ? value : "" });
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
        className="w-full"
        label="Name"
        value={record.name}
        placeholder={baseDomain === "" ? "grafana.example.com" : `grafana.${baseDomain}`}
        spellCheck={false}
        autoComplete="off"
        {...fieldError(shown.name ?? undefined)}
        onChange={(event) => {
          onChange({ name: event.target.value });
        }}
        onBlur={() => {
          onLeave("name");
        }}
      />
      <Input
        className="w-full"
        label="Value"
        value={record.value}
        placeholder="100.64.0.3"
        spellCheck={false}
        autoComplete="off"
        {...fieldError(shown.value ?? undefined)}
        onChange={(event) => {
          onChange({ value: event.target.value });
        }}
        onBlur={() => {
          onLeave("value");
        }}
      />
    </div>
  );
}

export function RecordAddForm({
  dns,
  mutations,
  onDone,
}: {
  readonly dns: Dns;
  readonly mutations: DnsMutations;
  readonly onDone: () => void;
}): ReactElement {
  const { record, update, issues, shown, leave, markSubmitted } = useRecordDraft(emptyRecord);

  // A failure from the last form must not greet this one. Only `reset` goes in the dependencies:
  // the mutation object is rebuilt on every render and `reset` itself notifies, so depending on
  // the whole object ran the effect after every render it caused, without end.
  const { reset } = mutations.set;

  useEffect(() => {
    reset();
  }, [reset]);

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();
    markSubmitted();

    if (issues.name !== null || issues.value !== null) {
      return;
    }

    mutations.set.mutate(
      { body: withRecord(dns.effective, record) },
      {
        onSuccess: () => {
          toast.success("Record added");
          onDone();
        },
      },
    );
  }

  return (
    <form noValidate onSubmit={submit} className="flex flex-col gap-4">
      <RecordFields
        record={record}
        shown={shown}
        baseDomain={dns.baseDomain}
        onChange={update}
        onLeave={leave}
      />
      <DialogError
        message={mutations.set.isError ? errorMessage(mutations.set.error) : undefined}
      />
      <FormFooter
        label="Save"
        pending={mutations.set.isPending}
        disabled={record.name.trim() === "" || record.value.trim() === ""}
      />
    </form>
  );
}

export function RecordInlineEditor({
  index,
  record: saved,
  dns,
  mutations,
  onClose,
  onDeleted,
}: {
  readonly index: number;
  readonly record: DnsRecord;
  readonly dns: Dns;
  readonly mutations: DnsMutations;
  readonly onClose: () => void;
  readonly onDeleted: () => void;
}): ReactElement {
  const { record, update, issues, shown, leave, markSubmitted, dirty } = useRecordDraft(saved);
  const { reset } = mutations.set;

  useEffect(() => {
    reset();
  }, [reset]);

  return (
    <InlineRowEditor
      columns={columnCount}
      label={`Edit ${saved.name}`}
      dirty={dirty}
      pending={mutations.set.isPending}
      error={mutations.set.isError ? errorMessage(mutations.set.error) : undefined}
      onSave={() => {
        markSubmitted();

        if (issues.name !== null || issues.value !== null) {
          return;
        }

        mutations.set.mutate(
          { body: withRecord(dns.effective, record, index) },
          {
            onSuccess: () => {
              toast.success("Record saved");
              onClose();
            },
          },
        );
      }}
      onCancel={onClose}
      onDelete={() => {
        mutations.apply(withoutRecord(dns.effective, index), `Removed ${saved.name}`);
        onDeleted();
      }}
    >
      <RecordFields
        record={record}
        shown={shown}
        baseDomain={dns.baseDomain}
        onChange={(patch) => {
          update(patch);

          if (mutations.set.isError) {
            reset();
          }
        }}
        onLeave={leave}
      />
    </InlineRowEditor>
  );
}
