import { Badge } from "@cloudflare/kumo/components/badge";
import { Button } from "@cloudflare/kumo/components/button";
import { PencilSimpleIcon } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import type { ReactElement, ReactNode } from "react";

import { errorMessage } from "~/api/error.ts";
import { nodePreferencesQuery } from "~/api/queries.ts";
import type { Node } from "~/api/queries.ts";
import type { NodePreferences } from "~/api/schema.gen.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { PreferencesDialog } from "~/components/machines/preferences-dialog.tsx";
import {
  exitNodeLabel,
  preferenceSwitches,
  remoteConfigCommand,
} from "~/components/machines/preferences-model.ts";
import { CopyText } from "~/components/ui/copy-text.tsx";
import { DefinitionList } from "~/components/ui/definition-list.tsx";
import type { Definition } from "~/components/ui/definition-list.tsx";
import { HelpTip } from "~/components/ui/hover-popover.tsx";
import { Section, SectionRow } from "~/components/ui/section.tsx";
import { Status, StatusDetail } from "~/components/ui/status.tsx";

const buttonIconSize = 12;

/**
 * What the machine's own owner set: the routes it advertises, whether it takes routes and DNS, the
 * exit node it uses. Reading them is a live question to the client, so an offline machine is not
 * asked; changing them needs the machine to have handed its local API to the tailnet admin.
 */
export function PreferencesSection({
  node,
  me,
}: {
  readonly node: Node;
  readonly me: Me;
}): ReactElement {
  const [editing, setEditing] = useState(false);
  const preferences = useQuery({ ...nodePreferencesQuery(node.id), enabled: node.online });
  const editable = node.remoteConfig && can(me, "devices:core");

  return (
    <Section
      title="Preferences"
      description={
        node.remoteConfig
          ? "The settings the machine's owner chose. The tailnet may change them because the machine allowed it."
          : "The settings the machine's owner chose. Run this on the machine to let the tailnet change them:"
      }
      actions={
        editable && preferences.data !== undefined ? (
          <Button
            variant="secondary"
            size="sm"
            icon={<PencilSimpleIcon size={buttonIconSize} />}
            onClick={() => {
              setEditing(true);
            }}
          >
            Edit
          </Button>
        ) : undefined
      }
      bodyClassName="p-0"
    >
      {node.remoteConfig ? null : (
        <SectionRow>
          <CopyText value={remoteConfigCommand} />
        </SectionRow>
      )}
      <Body
        node={node}
        preferences={preferences.data}
        loading={preferences.isPending}
        error={preferences.isError ? errorMessage(preferences.error) : undefined}
      />
      <PreferencesDialog
        node={node}
        preferences={preferences.data}
        open={editing}
        onOpenChange={setEditing}
      />
    </Section>
  );
}

function Body({
  node,
  preferences,
  loading,
  error,
}: {
  readonly node: Node;
  readonly preferences: NodePreferences | undefined;
  readonly loading: boolean;
  readonly error: string | undefined;
}): ReactElement {
  if (!node.online) {
    return (
      <SectionRow className="text-kumo-subtle">
        Connect the machine to read its preferences.
      </SectionRow>
    );
  }

  if (error !== undefined) {
    return (
      <SectionRow>
        <StatusDetail
          tone="danger"
          label="Failed"
          title="The machine did not answer"
          detail={error}
        />
      </SectionRow>
    );
  }

  if (preferences === undefined) {
    return (
      <SectionRow className="text-kumo-subtle">
        {loading ? "Asking the machine…" : "No answer yet."}
      </SectionRow>
    );
  }

  return <DefinitionList items={facts(preferences)} columns={2} />;
}

function facts(preferences: NodePreferences): Definition[] {
  return [
    {
      key: "hostname",
      label: (
        <Label text="Hostname">
          What the machine calls itself. Empty means it uses its own operating system hostname.
        </Label>
      ),
      value: preferences.hostname === "" ? <Default /> : preferences.hostname,
      ...(preferences.hostname === "" ? {} : { copy: preferences.hostname }),
    },
    {
      key: "exitNode",
      label: (
        <Label text="Exit node">
          The machine whose internet connection this one uses. None means it routes for itself.
        </Label>
      ),
      value: exitNodeLabel(preferences),
    },
    {
      key: "advertiseRoutes",
      label: (
        <Label text="Advertised routes">
          The prefixes the machine offers to route. They still need approving before any peer uses
          them.
        </Label>
      ),
      value: <Routes routes={preferences.advertiseRoutes} />,
    },
    ...preferenceSwitches.map(({ key, label, hint }) => ({
      key,
      label: <Label text={label}>{hint}</Label>,
      value: <OnOff on={preferences[key]} />,
    })),
  ];
}

/**
 * A short label with what the setting does behind it. The labels of this list cannot shrink, so a
 * sentence in one of them takes the room the value needs at half a column and stacks on a phone.
 */
function Label({
  text,
  children,
}: {
  readonly text: string;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <span className="flex items-baseline gap-1.5">
      <span>{text}</span>
      <HelpTip label={text}>{children}</HelpTip>
    </span>
  );
}

/** What the client does with the field left empty, where the value would otherwise be blank. */
function Default(): ReactElement {
  return <span className="text-kumo-subtle">Its own hostname</span>;
}

/** A route is an identifier the operator picks out of a list, so it is a pill, not a state. */
function Routes({ routes }: { readonly routes: readonly string[] }): ReactElement {
  if (routes.length === 0) {
    return <span className="text-kumo-subtle">None</span>;
  }

  return (
    <span className="flex flex-wrap justify-end gap-1">
      {routes.map((route) => (
        <Badge key={route} variant="secondary">
          <span className="font-mono">{route}</span>
        </Badge>
      ))}
    </span>
  );
}

function OnOff({ on }: { readonly on: boolean }): ReactElement {
  return on ? <Status tone="success">On</Status> : <span className="text-kumo-subtle">Off</span>;
}
