import { Button } from "@cloudflare/kumo/components/button";
import { PlusIcon } from "@phosphor-icons/react";
import { Fragment, useState } from "react";
import type { ReactElement } from "react";

import type { DnsSettings } from "~/api/schema.gen.ts";
import { EditToggle, editToggleId, focusEditToggle } from "~/components/dns/inline-row-editor.tsx";
import { AddressList, ListCell, ListRow, ListTable } from "~/components/dns/list-table.tsx";
import {
  splitEntries,
  splitKeptWithExitNode,
  withSplitUseWithExitNode,
} from "~/components/dns/model.ts";
import type { DnsMutations } from "~/components/dns/mutations.ts";
import { ExitNodeToggle } from "~/components/dns/nameservers-section.tsx";
import { SplitAddForm, SplitInlineEditor } from "~/components/dns/split-forms.tsx";
import { DialogContent, DialogRoot } from "~/components/ui/dialog.tsx";
import { Domain } from "~/components/ui/domain.tsx";
import { Section, SectionEmpty } from "~/components/ui/section.tsx";

export function SplitDnsSection({
  settings,
  canEdit,
  mutations,
}: {
  readonly settings: DnsSettings;
  readonly canEdit: boolean;
  readonly mutations: DnsMutations;
}): ReactElement {
  const [adding, setAdding] = useState(false);
  // The one row whose editor is open, by domain.
  const [open, setOpen] = useState<string | null>(null);
  const entries = splitEntries(settings);
  const close = (domain: string): void => {
    setOpen(null);
    focusEditToggle(editToggleId("split", domain));
  };

  return (
    <Section
      title="Split DNS"
      description="Every machine in the tailnet resolves these domains through the resolvers you name here."
      panel={false}
      {...(canEdit
        ? {
            actions: (
              <Button
                variant="secondary"
                size="sm"
                icon={PlusIcon}
                onClick={() => {
                  setAdding(true);
                }}
              >
                Add domain
              </Button>
            ),
          }
        : {})}
    >
      <ListTable
        columns={[
          { key: "domain", label: "Domain", className: "w-[30%]" },
          { key: "servers", label: "Nameservers" },
          { key: "exit", label: "Exit node", className: "w-56" },
        ]}
        actions={canEdit}
        actionsClassName="w-20 px-2"
        empty={
          entries.length === 0 ? (
            <SectionEmpty
              title="No split DNS domains"
              description="Every domain is answered by the global nameservers."
            />
          ) : null
        }
      >
        {entries.map(([domain, servers]) => (
          <Fragment key={domain}>
            <SplitRow
              domain={domain}
              servers={servers}
              settings={settings}
              canEdit={canEdit}
              open={open === domain}
              mutations={mutations}
              onToggle={() => {
                setOpen(open === domain ? null : domain);
              }}
            />
            {open === domain ? (
              <SplitInlineEditor
                editing={{ domain, servers }}
                settings={settings}
                mutations={mutations}
                onClose={() => {
                  close(domain);
                }}
                onDeleted={() => {
                  setOpen(null);
                }}
              />
            ) : null}
          </Fragment>
        ))}
      </ListTable>
      <DialogRoot open={adding} onOpenChange={setAdding}>
        <DialogContent
          size="xl"
          title="Add split DNS domain"
          description="Queries for the domain and everything under it go to these resolvers instead of the global ones."
        >
          <SplitAddForm
            settings={settings}
            mutations={mutations}
            onDone={() => {
              setAdding(false);
            }}
          />
        </DialogContent>
      </DialogRoot>
    </Section>
  );
}

/** One split DNS domain: its resolvers, whether an exit node keeps them, and its Edit button. */
function SplitRow({
  domain,
  servers,
  settings,
  canEdit,
  open,
  mutations,
  onToggle,
}: {
  readonly domain: string;
  readonly servers: readonly string[];
  readonly settings: DnsSettings;
  readonly canEdit: boolean;
  readonly open: boolean;
  readonly mutations: DnsMutations;
  readonly onToggle: () => void;
}): ReactElement {
  const pending = mutations.set.isPending;

  return (
    <ListRow>
      <ListCell>
        <span className="flex min-w-0">
          <Domain domain={domain} />
        </span>
      </ListCell>
      <ListCell className="font-mono text-sm text-kumo-subtle">
        <AddressList addresses={servers} />
      </ListCell>
      <ListCell>
        <ExitNodeToggle
          name={domain}
          checked={splitKeptWithExitNode(settings, domain)}
          disabled={!canEdit || pending}
          reason={canEdit ? undefined : "Your credentials cannot change DNS"}
          pending={pending}
          onChange={(on) => {
            mutations.apply(
              withSplitUseWithExitNode(settings, domain, on),
              `${domain} ${on ? "will be used" : "will not be used"} with an exit node`,
            );
          }}
        />
      </ListCell>
      {canEdit ? (
        <ListCell actions className="w-20 px-2">
          <EditToggle
            id={editToggleId("split", domain)}
            open={open}
            label={domain}
            disabled={pending && !open}
            onToggle={onToggle}
          />
        </ListCell>
      ) : null}
    </ListRow>
  );
}
