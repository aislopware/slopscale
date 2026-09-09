import { Input } from "@cloudflare/kumo/components/input";
import { Switch } from "@cloudflare/kumo/components/switch";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { api } from "~/api/client.ts";
import { errorMessage } from "~/api/error.ts";
import { invalidate } from "~/api/queries.ts";
import type { Node } from "~/api/queries.ts";
import type { NodePreferences } from "~/api/schema.gen.ts";
import { TagInput, usePendingLists } from "~/components/apps/tag-input.tsx";
import { FormFooter } from "~/components/machines/dialogs.tsx";
import {
  canSavePreferences,
  preferenceChanges,
  preferenceSwitches,
} from "~/components/machines/preferences-model.ts";
import { prefixError } from "~/components/networks/model.ts";
import { DialogContent, DialogError, DialogRoot } from "~/components/ui/dialog.tsx";
import { toast } from "~/components/ui/toast.ts";
import { useBaseline } from "~/lib/use-baseline.ts";

export interface PreferencesDialogProps {
  readonly node: Node;
  /** What the machine last answered; the dialog opens on a copy of it. */
  readonly preferences: NodePreferences | undefined;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
}

/**
 * Changes the machine's own preferences from the console. Only the fields the operator touched are
 * sent, so a setting the machine's owner changed while the dialog was open is left alone.
 */
export function PreferencesDialog({
  node,
  preferences,
  open,
  onOpenChange,
}: PreferencesDialogProps): ReactElement {
  return (
    <DialogRoot open={open} onOpenChange={onOpenChange}>
      <DialogContent
        size="lg"
        title="Edit preferences"
        description="These go straight to the machine's own client, which applies them at once and may refuse one it does not allow."
      >
        {preferences === undefined ? null : (
          // Keyed by the machine alone: the form starts from what the machine last answered and
          // keeps it, so a later answer does not remount it under the operator.
          <PreferencesForm
            key={node.id}
            node={node}
            preferences={preferences}
            onOpenChange={onOpenChange}
          />
        )}
      </DialogContent>
    </DialogRoot>
  );
}

function PreferencesForm({
  node,
  preferences,
  onOpenChange,
}: {
  readonly node: Node;
  readonly preferences: NodePreferences;
  readonly onOpenChange: (open: boolean) => void;
}): ReactElement {
  // The answer the form opened on, kept as it was. The machine is asked again while the dialog is
  // open, and diffing against a fresher answer would send back fields nobody touched.
  const baseline = useBaseline(preferences, node.id);
  const [draft, setDraft] = useState<NodePreferences>(baseline);
  const lists = usePendingLists();
  const queryClient = useQueryClient();
  const save = api.useMutation("patch", "/api/v1/node/{nodeId}/preferences", {
    onSuccess: async () => {
      toast.success("Preferences changed");
      onOpenChange(false);
      await invalidate(queryClient, "/api/v1/node");
    },
  });
  const changes = preferenceChanges(draft, baseline);
  const incomplete = !canSavePreferences(changes, lists.pending);

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();

    // Enter in a text input submits the form whatever the buttons say, so the guard is here too.
    if (incomplete || save.isPending) {
      return;
    }

    save.mutate({ params: { path: { nodeId: node.id } }, body: changes });
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <Input
        label="Hostname"
        required={false}
        description="What the machine calls itself; the tailnet name follows it unless the machine was renamed here. Leave it empty to use the machine's own operating system hostname."
        placeholder="Its own hostname"
        value={draft.hostname}
        spellCheck={false}
        onChange={(event) => {
          setDraft({ ...draft, hostname: event.target.value });
        }}
      />
      <Input
        label="Exit node"
        required={false}
        description="A machine's stable id or one of its addresses. Empty routes the machine's own traffic itself."
        value={draft.exitNode}
        spellCheck={false}
        placeholder="None"
        onChange={(event) => {
          setDraft({ ...draft, exitNode: event.target.value });
        }}
      />
      <TagInput
        label="Advertised routes"
        description="The prefixes the machine offers to route. They still need approving before any peer uses them."
        placeholder="10.0.0.0/24"
        value={draft.advertiseRoutes}
        validate={prefixError}
        onPendingChange={lists.track("routes")}
        onValueChange={(routes) => {
          setDraft({ ...draft, advertiseRoutes: routes });
        }}
      />
      <Switches draft={draft} onChange={setDraft} />
      <DialogError message={save.isError ? errorMessage(save.error) : undefined} />
      <FormFooter label="Save preferences" pending={save.isPending} disabled={incomplete} />
    </form>
  );
}

/** The switches, in the order the section lists them, so the two read the same way. */
function Switches({
  draft,
  onChange,
}: {
  readonly draft: NodePreferences;
  readonly onChange: (draft: NodePreferences) => void;
}): ReactElement {
  return (
    <div className="flex flex-col gap-3 border-t border-kumo-line pt-4">
      {preferenceSwitches.map(({ key, label, hint }) => (
        <Switch
          key={key}
          label={label}
          labelTooltip={hint}
          controlFirst
          checked={draft[key]}
          onCheckedChange={(on) => {
            onChange({ ...draft, [key]: on });
          }}
        />
      ))}
    </div>
  );
}
