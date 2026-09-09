import { Badge } from "@cloudflare/kumo/components/badge";
import { Button } from "@cloudflare/kumo/components/button";
import { SignOutIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import { displayName, roleLabel } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { signOut } from "~/auth/session.ts";
import { SettingRow } from "~/components/settings/setting-row.tsx";
import { DefinitionList } from "~/components/ui/definition-list.tsx";
import type { Definition } from "~/components/ui/definition-list.tsx";
import { Section } from "~/components/ui/section.tsx";

const kindLabels: Record<string, string> = {
  api_key: "API key",
  local: "Local socket",
  oauth: "OAuth token",
  session: "Browser session",
};

function consoleItems(me: Me): readonly Definition[] {
  const role = roleLabel(me);

  return [
    {
      label: "Signed in as",
      value: (
        <span className="flex items-center gap-1.5">
          {displayName(me)}
          <span className="text-kumo-subtle">{kindLabels[me.kind] ?? me.kind}</span>
        </span>
      ),
    },
    {
      label: "Role",
      value:
        role === null ? (
          <span className="text-kumo-subtle">Not bound to a user</span>
        ) : (
          <span>{role}</span>
        ),
    },
    { label: "Scopes", value: <ScopeList me={me} /> },
  ];
}

export function SessionSection({ me }: { readonly me: Me }): ReactElement {
  return (
    <Section
      title="Current session"
      description="Who this browser is signed in as."
      bodyClassName="p-0"
    >
      <DefinitionList items={consoleItems(me)} />
      <SettingRow
        title="Sign out"
        description="Ends this browser session. Machines and keys are unaffected."
        control={
          <Button
            variant="secondary"
            icon={SignOutIcon}
            onClick={() => {
              void signOut();
            }}
          >
            Sign out
          </Button>
        }
      />
    </Section>
  );
}

function ScopeList({ me }: { readonly me: Me }): ReactElement {
  if (me.allAccess) {
    return <span>All access</span>;
  }

  if (me.scopes.length === 0) {
    return <span className="text-kumo-subtle">None</span>;
  }

  return (
    <span className="flex flex-wrap justify-end gap-1">
      {me.scopes.map((scope) => (
        <Badge key={scope} variant="secondary" className="font-mono text-[0.9em] font-normal">
          {scope}
        </Badge>
      ))}
    </span>
  );
}
