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
import { Code } from "~/components/ui/code.tsx";
import { Domain } from "~/components/ui/domain.tsx";
import { Section } from "~/components/ui/section.tsx";
import { StagedAddPanel } from "~/components/ui/staged-add-panel.tsx";

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
      value: <Domain domain={dns.baseDomain} />,
      aside: <FromFileBadge />,
    });
  }

  for (const domain of settings.searchDomains) {
    entries.push({
      key: domain,
      value: <Domain domain={domain} />,
      menuLabel: `Actions for search domain ${domain}`,
      onRemove: () => {
        mutations.apply(withoutSearchDomain(settings, domain), `Removed ${domain}`);
      },
    });
  }

  return (
    <>
      {adding ? (
        <StagedAddPanel
          title="Add search domains"
          description={
            <>
              Machines append each to names without a dot, so <Code>db</Code> also tries{" "}
              <Code>db.corp.example.com</Code>.
            </>
          }
          label="Domain"
          placeholder="corp.example.com"
          normalize={normalizeDomain}
          validate={domainError}
          existing={settings.searchDomains}
          successMessage={(count) =>
            count === 1 ? "Search domain added" : `${count} search domains added`
          }
          mutation={mutations.set}
          onClose={() => {
            setAdding(false);
          }}
          onSubmit={(values, done) => {
            mutations.set.mutate(
              { body: values.reduce((next, domain) => withSearchDomain(next, domain), settings) },
              { onSuccess: done },
            );
          }}
        />
      ) : null}
      <Section
        title="Search domains"
        description="Suffixes machines try for short names. The base domain is always first."
        bodyClassName="p-0"
        {...(canEdit
          ? {
              actions: (
                <Button
                  variant="secondary"
                  size="sm"
                  icon={PlusIcon}
                  disabled={adding}
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
      </Section>
    </>
  );
}
