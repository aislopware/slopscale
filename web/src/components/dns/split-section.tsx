import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { Input, Textarea } from "@cloudflare/kumo/components/input";
import { PencilSimpleIcon, PlusIcon, TrashIcon } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { DnsSettings } from "~/api/schema.gen.ts";
import {
  domainError,
  nameserversError,
  normalizeDomain,
  parseList,
  splitEntries,
  splitKeptWithExitNode,
  withSplit,
  withSplitUseWithExitNode,
  withoutSplit,
} from "~/components/dns/model.ts";
import type { DnsMutations } from "~/components/dns/mutations.ts";
import { ExitNodeToggle } from "~/components/dns/nameservers-section.tsx";
import { FormFooter } from "~/components/machines/dialogs.tsx";
import { DialogContent, DialogError, DialogRoot } from "~/components/ui/dialog.tsx";
import { Domain } from "~/components/ui/domain.tsx";
import { RowMenu } from "~/components/ui/row-menu.tsx";
import { Section, SectionEmpty, SectionRow } from "~/components/ui/section.tsx";
import { toast } from "~/components/ui/toast.ts";

interface Editing {
  readonly domain: string;
  readonly servers: readonly string[];
}

export function SplitDnsSection({
  settings,
  canEdit,
  mutations,
}: {
  readonly settings: DnsSettings;
  readonly canEdit: boolean;
  readonly mutations: DnsMutations;
}): ReactElement {
  const [dialog, setDialog] = useState<"closed" | "new" | Editing>("closed");
  const entries = splitEntries(settings);
  const pending = mutations.set.isPending;

  return (
    <Section
      title="Split DNS"
      description="Domains answered by their own resolvers, such as an internal zone behind a subnet router."
      bodyClassName="p-0"
      {...(canEdit
        ? {
            actions: (
              <Button
                variant="secondary"
                icon={PlusIcon}
                onClick={() => {
                  setDialog("new");
                }}
              >
                Add domain
              </Button>
            ),
          }
        : {})}
    >
      {entries.length === 0 ? (
        <SectionEmpty
          title="No split DNS domains"
          description="Every domain is answered by the global nameservers."
        />
      ) : (
        entries.map(([domain, servers]) => (
          <SectionRow key={domain} className="flex items-center justify-between gap-4 py-2.5">
            <div className="grid min-w-0 flex-1 gap-x-6 gap-y-0.5 sm:grid-cols-[minmax(0,1fr)_minmax(0,1.5fr)]">
              <span className="flex min-w-0">
                <Domain domain={domain} />
              </span>
              <span className="min-w-0 font-mono text-sm break-all text-kumo-subtle">
                {servers.join(", ")}
              </span>
            </div>
            <div className="flex shrink-0 items-center gap-3">
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
              {canEdit ? (
                <RowMenu label={`Actions for split DNS ${domain}`} disabled={pending}>
                  <DropdownMenu.Item
                    icon={PencilSimpleIcon}
                    onClick={() => {
                      setDialog({ domain, servers });
                    }}
                  >
                    Edit…
                  </DropdownMenu.Item>
                  <DropdownMenu.Separator />
                  <DropdownMenu.Item
                    icon={TrashIcon}
                    variant="danger"
                    onClick={() => {
                      mutations.apply(withoutSplit(settings, domain), `Removed ${domain}`);
                    }}
                  >
                    Remove
                  </DropdownMenu.Item>
                </RowMenu>
              ) : null}
            </div>
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
          title={dialog === "new" ? "Add split DNS domain" : "Edit split DNS domain"}
          description="Queries for the domain and everything under it go to these resolvers instead of the global ones."
        >
          <SplitForm
            editing={dialog === "new" || dialog === "closed" ? null : dialog}
            settings={settings}
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

/** What stops the form from saving, per field; a domain that already has an entry counts. */
function splitIssues(
  settings: DnsSettings,
  editing: Editing | null,
  { domain, servers }: { readonly domain: string; readonly servers: readonly string[] },
): { readonly domain: string | null; readonly servers: string | null } {
  const taken =
    editing?.domain !== domain && settings.splitNameservers[domain] !== undefined
      ? "This domain already has resolvers. Edit that entry instead."
      : null;

  return { domain: domainError(domain) ?? taken, servers: nameserversError(servers) };
}

function SplitForm({
  editing,
  settings,
  mutations,
  onDone,
}: {
  readonly editing: Editing | null;
  readonly settings: DnsSettings;
  readonly mutations: DnsMutations;
  readonly onDone: () => void;
}): ReactElement {
  const [domain, setDomain] = useState(editing?.domain ?? "");
  const [servers, setServers] = useState(editing?.servers.join("\n") ?? "");
  const [touched, setTouched] = useState(false);

  useEffect(() => {
    mutations.set.reset();
  }, [mutations.set]);
  const cleanDomain = normalizeDomain(domain);
  const list = parseList(servers);
  const issues = splitIssues(settings, editing, { domain: cleanDomain, servers: list });
  const issue = issues.domain ?? issues.servers;

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();
    setTouched(true);

    if (issue !== null) {
      return;
    }

    mutations.set.mutate(
      {
        body: withSplit(settings, {
          domain: cleanDomain,
          servers: list,
          previous: editing?.domain,
        }),
      },
      {
        onSuccess: () => {
          toast.success(editing === null ? "Split DNS domain added" : "Split DNS domain updated");
          onDone();
        },
      },
    );
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <Input
        label="Domain"
        value={domain}
        placeholder="corp.example.com"
        spellCheck={false}
        autoComplete="off"
        onChange={(event) => {
          setDomain(event.target.value);
        }}
        onBlur={() => {
          setTouched(true);
        }}
        {...(touched && issues.domain !== null ? { error: issues.domain } : {})}
      />
      <Textarea
        label="Nameservers"
        description="One per line. An IP, an IP with port, or a DNS-over-HTTPS URL."
        value={servers}
        placeholder={"10.0.0.53\n10.0.0.54"}
        spellCheck={false}
        autoResize
        minRows={3}
        maxRows={8}
        onChange={(event) => {
          setServers(event.target.value);
        }}
        onBlur={() => {
          setTouched(true);
        }}
        {...(touched && issues.domain === null && issues.servers !== null
          ? { error: issues.servers }
          : {})}
      />
      <DialogError
        message={mutations.set.isError ? errorMessage(mutations.set.error) : undefined}
      />
      <FormFooter
        label={editing === null ? "Add" : "Save"}
        pending={mutations.set.isPending}
        disabled={cleanDomain === "" || list.length === 0}
      />
    </form>
  );
}
