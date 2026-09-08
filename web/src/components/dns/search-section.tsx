import { Button } from "@cloudflare/kumo/components/button";
import { PlusIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import type { Dns } from "~/api/queries.ts";
import { EntryList } from "~/components/dns/entry-list.tsx";
import type { Entry } from "~/components/dns/entry-list.tsx";
import {
  domainError,
  editableSettings,
  normalizeDomain,
  withSearchDomain,
  withoutSearchDomain,
} from "~/components/dns/model.ts";
import type { DnsMutations } from "~/components/dns/mutations.ts";
import { FromFileBadge } from "~/components/dns/source.tsx";
import { ValueDialog } from "~/components/dns/value-dialog.tsx";
import { Section } from "~/components/ui/section.tsx";

export function SearchDomainsSection({
  dns,
  canEdit,
  mutations,
}: {
  readonly dns: Dns;
  readonly canEdit: boolean;
  readonly mutations: DnsMutations;
}): ReactElement {
  const [adding, setAdding] = useState(false);
  const settings = editableSettings(dns);
  const entries: Entry[] = [];

  if (dns.baseDomain !== "") {
    entries.push({
      key: `base:${dns.baseDomain}`,
      value: dns.baseDomain,
      aside: <FromFileBadge />,
    });
  }

  for (const domain of settings.searchDomains) {
    entries.push({
      key: domain,
      value: domain,
      removeLabel: `Remove search domain ${domain}`,
      onRemove: () => {
        mutations.apply(withoutSearchDomain(settings, domain), `Removed ${domain}`);
      },
    });
  }

  return (
    <Section
      title="Search domains"
      description="Suffixes machines try for short names. The base domain is always first."
      bodyClassName="p-0"
      {...(canEdit
        ? {
            actions: (
              <Button
                variant="secondary"
                icon={PlusIcon}
                onClick={() => {
                  setAdding(true);
                }}
              >
                Add search domain
              </Button>
            ),
          }
        : {})}
    >
      <EntryList
        canEdit={canEdit}
        pending={mutations.set.isPending}
        empty={{
          title: "No search domains",
          description: "Only the base domain is tried for a short name.",
        }}
        entries={entries}
      />
      <ValueDialog
        open={adding}
        onOpenChange={setAdding}
        title="Add search domain"
        description="Machines append it to names without a dot, so `db` also tries `db.corp.example.com`."
        label="Domain"
        placeholder="corp.example.com"
        normalize={normalizeDomain}
        validate={domainError}
        successMessage="Search domain added"
        mutation={mutations.set}
        onSubmit={(value, done) => {
          mutations.set.mutate({ body: withSearchDomain(settings, value) }, { onSuccess: done });
        }}
      />
    </Section>
  );
}
