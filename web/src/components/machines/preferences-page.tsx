import { LinkButton } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { api } from "~/api/client.ts";
import { errorMessage } from "~/api/error.ts";
import { invalidate, nodePreferencesQuery } from "~/api/queries.ts";
import type { Node } from "~/api/queries.ts";
import type { NodePreferences } from "~/api/schema.gen.ts";
import {
  hasPreferenceChanges,
  preferenceChanges,
  preferenceSwitches,
  remoteConfigCommand,
} from "~/components/machines/preferences-model.ts";
import type { PreferenceSwitch } from "~/components/machines/preferences-model.ts";
import { prefixError } from "~/components/networks/model.ts";
import { SettingToggle } from "~/components/policy/rule-builder.tsx";
import { CopyText } from "~/components/ui/copy-text.tsx";
import { FormPage, FormSection, nameFieldClass, useDraft } from "~/components/ui/form-page.tsx";
import { ListField } from "~/components/ui/list-field.tsx";
import { Section, SectionEmpty } from "~/components/ui/section.tsx";
import { toast } from "~/components/ui/toast.ts";
import { listError, listValues } from "~/lib/list.ts";
import { useBaseline } from "~/lib/use-baseline.ts";

/** Where the page goes back to: the machine's Settings tab, which lists the preferences. */
function machineSettingsPath(node: Node): string {
  return `/machines/${node.id}/settings`;
}

/**
 * The machine's own preferences on a page of their own. Reading them is a live question to the
 * client, so the page says why it cannot ask when the machine is offline or has not handed its
 * local API to the tailnet, and waits for the answer otherwise.
 */
export function MachinePreferences({
  node,
  canEdit,
}: {
  readonly node: Node;
  readonly canEdit: boolean;
}): ReactElement {
  const preferences = useQuery({
    ...nodePreferencesQuery(node.id),
    enabled: node.online && node.remoteConfig,
  });

  if (!node.remoteConfig) {
    return (
      <Unavailable
        node={node}
        title="The tailnet cannot change this machine's preferences"
        description="Run this on the machine to let it:"
        command
      />
    );
  }

  if (!node.online) {
    return (
      <Unavailable
        node={node}
        title="The machine is offline"
        description="Connect the machine to read and change its preferences."
      />
    );
  }

  if (preferences.isError) {
    return (
      <Unavailable
        node={node}
        title="The machine did not answer"
        description={errorMessage(preferences.error)}
      />
    );
  }

  if (preferences.data === undefined) {
    return (
      <Section title="Preferences" bodyClassName="p-0">
        <SectionEmpty title="Asking the machine…" />
      </Section>
    );
  }

  // Keyed by the machine alone: the form starts from what the machine last answered and keeps it,
  // so a later answer does not remount it under the operator.
  return (
    <PreferencesEditor key={node.id} node={node} preferences={preferences.data} canEdit={canEdit} />
  );
}

function Unavailable({
  node,
  title,
  description,
  command = false,
}: {
  readonly node: Node;
  readonly title: string;
  readonly description: string;
  readonly command?: boolean;
}): ReactElement {
  return (
    <Section title="Preferences" bodyClassName="p-0">
      <SectionEmpty
        title={title}
        description={description}
        contents={
          <div className="flex flex-col items-center gap-4">
            {command ? <CopyText value={remoteConfigCommand} /> : null}
            <LinkButton href={machineSettingsPath(node)} variant="secondary">
              Back to the machine
            </LinkButton>
          </div>
        }
      />
    </Section>
  );
}

/** The preferences as the form holds them, with the routes as typed, blank rows and all. */
type Draft = NodePreferences & { readonly routes: readonly string[] };

type Change = (patch: Partial<Draft>) => void;

function draftFrom(answer: NodePreferences): Draft {
  return { ...answer, routes: answer.advertiseRoutes };
}

/** What the draft is worth: the routes it holds, without the blank rows. */
function preferencesFrom(draft: Draft): NodePreferences {
  return { ...draft, advertiseRoutes: listValues(draft.routes) };
}

/**
 * Changes the machine's own preferences from the console. Only the fields the operator touched are
 * sent, so a setting the machine's owner changed while the page was open is left alone.
 */
function PreferencesEditor({
  node,
  preferences,
  canEdit,
}: {
  readonly node: Node;
  readonly preferences: NodePreferences;
  readonly canEdit: boolean;
}): ReactElement {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  // The answer the page opened on, kept as it was. The machine is asked again while the page is
  // open, and diffing against a fresher answer would send back fields nobody touched.
  const baseline = useBaseline(preferences, node.id);
  const save = api.useMutation("patch", "/api/v1/node/{nodeId}/preferences", {
    onSuccess: async () => {
      await invalidate(queryClient, "/api/v1/node");
    },
  });
  const { draft, dirty, update } = useDraft(draftFrom(baseline), save);
  const back = machineSettingsPath(node);

  const leave = (): void => {
    void navigate({ to: back, ignoreBlocker: true });
  };

  function submit(): boolean {
    if (listError(draft.routes, prefixError) !== null) {
      return false;
    }

    const changes = preferenceChanges(preferencesFrom(draft), baseline);

    // Nothing to tell the machine: going back is all a save would do.
    if (!hasPreferenceChanges(changes)) {
      leave();

      return true;
    }

    save.mutate(
      { params: { path: { nodeId: node.id } }, body: changes },
      {
        onSuccess: () => {
          toast.success("Preferences saved");
          leave();
        },
      },
    );

    return true;
  }

  return (
    <FormPage
      noun="preferences"
      creating={false}
      canEdit={canEdit}
      error={save.isError ? errorMessage(save.error) : undefined}
      errorTitle="The machine did not take the change"
      pending={save.isPending}
      dirty={dirty}
      onSubmit={submit}
      cancelTo={back}
      submitLabel="Save preferences"
    >
      <FormSection title="Identity">
        <Input
          label="Hostname"
          required={false}
          className={nameFieldClass}
          description="What the machine calls itself; the tailnet name follows it unless the machine was renamed here. Leave it empty to use the operating system's hostname."
          placeholder="Its own hostname"
          value={draft.hostname}
          spellCheck={false}
          onChange={(event) => {
            update({ hostname: event.target.value });
          }}
        />
      </FormSection>
      <ConnectionsSection draft={draft} onChange={update} />
      <OfferedSection draft={draft} onChange={update} />
      <Toggles
        title="Security"
        keys={["runSsh", "shieldsUp", "postureChecking"]}
        draft={draft}
        onChange={update}
      />
      <Toggles
        title="Updates"
        keys={["autoUpdateCheck", "autoUpdateApply"]}
        draft={draft}
        onChange={update}
      />
    </FormPage>
  );
}

/** How the machine uses the tailnet: the exit node it sends its traffic through, routes and DNS. */
function ConnectionsSection({
  draft,
  onChange,
}: {
  readonly draft: Draft;
  readonly onChange: Change;
}): ReactElement {
  return (
    <FormSection
      title="Using the tailnet"
      info="What the machine takes from the rest of the tailnet."
    >
      <Input
        label="Exit node"
        required={false}
        className={nameFieldClass}
        description="A machine's stable id or one of its addresses. Empty routes the machine's own traffic itself."
        value={draft.exitNode}
        spellCheck={false}
        placeholder="None"
        onChange={(event) => {
          onChange({ exitNode: event.target.value });
        }}
      />
      <SwitchRows
        keys={["exitNodeAllowLanAccess", "acceptRoutes", "acceptDns"]}
        draft={draft}
        onChange={onChange}
      />
    </FormSection>
  );
}

/** What the machine offers the rest of the tailnet: subnet routes, an exit, an app connector. */
function OfferedSection({
  draft,
  onChange,
}: {
  readonly draft: Draft;
  readonly onChange: Change;
}): ReactElement {
  return (
    <FormSection
      title="Offered to the tailnet"
      info="Advertised routes and an offered exit node still need approving before any peer uses them."
    >
      <ListField
        label="Advertised routes"
        description="The prefixes the machine offers to route."
        placeholder="10.0.0.0/24"
        addLabel="Add route"
        optional
        value={draft.routes}
        validate={prefixError}
        onValueChange={(routes) => {
          onChange({ routes });
        }}
      />
      <SwitchRows
        keys={["advertiseExitNode", "advertiseConnector"]}
        draft={draft}
        onChange={onChange}
      />
    </FormSection>
  );
}

/** A section of switches alone. */
function Toggles({
  title,
  keys,
  draft,
  onChange,
}: {
  readonly title: string;
  readonly keys: readonly PreferenceSwitch[];
  readonly draft: Draft;
  readonly onChange: Change;
}): ReactElement {
  return (
    <FormSection title={title}>
      <SwitchRows keys={keys} draft={draft} onChange={onChange} />
    </FormSection>
  );
}

/** Titles that need more words here than beside a value in the Preferences section. */
const titles: Partial<Record<PreferenceSwitch, string>> = {
  advertiseExitNode: "Offer as exit node",
  advertiseConnector: "Offer as app connector",
};

/** The switch rows for the keys, each titled with its label and described by what it does. */
function SwitchRows({
  keys,
  draft,
  onChange,
}: {
  readonly keys: readonly PreferenceSwitch[];
  readonly draft: Draft;
  readonly onChange: Change;
}): ReactElement {
  return (
    <>
      {preferenceSwitches
        .filter(({ key }) => keys.includes(key))
        .map(({ key, label, hint }) => (
          <SettingToggle
            key={key}
            title={titles[key] ?? label}
            description={hint}
            checked={draft[key]}
            onCheckedChange={(on) => {
              onChange({ [key]: on });
            }}
          />
        ))}
    </>
  );
}
