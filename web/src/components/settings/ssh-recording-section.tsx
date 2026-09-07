import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { Switch } from "@cloudflare/kumo/components/switch";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import type { Settings } from "~/api/queries.ts";
import { useSettingsMutation } from "~/components/settings/mutations.ts";
import { SettingRow } from "~/components/settings/setting-row.tsx";
import { Section } from "~/components/ui/section.tsx";
import { toast } from "~/components/ui/toast.ts";

/** Comma or whitespace separated recorder names, blanks dropped. */
export function parseRecorders(text: string): string[] {
  return text
    .split(/[\s,]+/v)
    .map((name) => name.trim())
    .filter((name) => name !== "");
}

export function SSHRecordingSection({
  settings,
  canEdit,
}: {
  readonly settings: Settings;
  readonly canEdit: boolean;
}): ReactElement {
  const update = useSettingsMutation();
  const stored = settings.sshRecorders.join(", ");
  const [draft, setDraft] = useState(stored);
  const dirty = draft !== stored;

  const save = (event: SubmitEvent<HTMLFormElement>): void => {
    event.preventDefault();

    const recorders = parseRecorders(draft);

    update.mutate(
      { body: { sshRecorders: recorders } },
      {
        onSuccess: () => {
          toast.success(
            recorders.length === 0 ? "Default recorders cleared" : "Default recorders saved",
          );
        },
      },
    );
  };

  const embedded = settings.embeddedRecorder
    ? "The server runs the embedded recorder, which is always a default; these are added to it."
    : "The server does not run the embedded recorder (ssh_recording.enabled in the config file).";

  return (
    <Section
      title="SSH session recording"
      description="Where sessions admitted by SSH rules are recorded, unless a rule names its own recorder."
      bodyClassName="p-0"
    >
      <SettingRow
        title="Default recorders"
        description={`Tags or tailnet addresses of recorder nodes, comma separated. ${embedded}`}
        control={
          <form onSubmit={save} className="flex items-center gap-2">
            <Input
              aria-label="Default recorders"
              className="w-72"
              value={draft}
              placeholder="tag:recorder"
              spellCheck={false}
              autoComplete="off"
              disabled={!canEdit}
              onChange={(event) => {
                setDraft(event.target.value);
              }}
            />
            <Button
              type="submit"
              variant="secondary"
              size="sm"
              disabled={!canEdit || !dirty}
              loading={update.isPending}
            >
              Save
            </Button>
          </form>
        }
      />
      <SettingRow
        title="Require recording"
        description="Reject a session when no default recorder can be reached, and end one whose recording breaks off. Off, the session goes on unrecorded and the failure is logged."
        control={
          <Switch
            aria-label="Require recording"
            checked={settings.sshRecordingEnforce}
            disabled={!canEdit || update.isPending}
            transitioning={update.isPending}
            onCheckedChange={(on) => {
              update.mutate(
                { body: { sshRecordingEnforce: on } },
                {
                  onSuccess: () => {
                    toast.success(`Recording ${on ? "required" : "optional"}`);
                  },
                },
              );
            }}
          />
        }
      />
    </Section>
  );
}
