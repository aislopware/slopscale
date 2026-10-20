import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { Switch } from "@cloudflare/kumo/components/switch";
import { useId, useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import type { Settings } from "~/api/queries.ts";
import { useSettingsMutation } from "~/components/settings/mutations.ts";
import { SettingRow } from "~/components/settings/setting-row.tsx";
import { SettingsGroup } from "~/components/settings/settings-page.tsx";
import { Code } from "~/components/ui/code.tsx";
import { SettingsCard, SettingsRow } from "~/components/ui/settings-card.tsx";
import { toast } from "~/components/ui/toast.ts";

/** Comma or whitespace separated recorder names, blanks dropped. */
export function parseRecorders(text: string): string[] {
  return text
    .split(/[\s,]+/v)
    .map((name) => name.trim())
    .filter((name) => name !== "");
}

/**
 * What is wrong with the typed recorders, in the words the field shows under it, or null. The
 * server takes a tag, a host name from the policy or a tailnet address or prefix; users, groups and
 * autogroups it refuses, so they are caught here before the round trip.
 */
export function recordersIssue(recorders: readonly string[]): string | null {
  for (const name of recorders) {
    if (name === "tag:") {
      return "A tag needs a name after tag:.";
    }

    if (name.includes("@") || name.startsWith("group:") || name.startsWith("autogroup:")) {
      return `${name} is not a recorder. Use a tag, a host name or a tailnet address.`;
    }
  }

  return null;
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
  const [issue, setIssue] = useState<string | null>(null);
  const issueId = useId();

  const save = (event: SubmitEvent<HTMLFormElement>): void => {
    event.preventDefault();

    const recorders = parseRecorders(draft);
    const problem = recordersIssue(recorders);

    setIssue(problem);

    if (problem !== null) {
      return;
    }

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

  const embedded = settings.embeddedRecorder ? (
    "The server always uses the embedded recorder. These are added to it."
  ) : (
    <>
      The server does not run the embedded recorder (<Code>ssh_recording.enabled</Code> in the
      config file).
    </>
  );

  return (
    <SettingsGroup
      id="ssh-recording"
      title="SSH session recording"
      docs="ref/ssh-recording"
      description="Where sessions admitted by SSH rules are recorded, unless a rule names its own recorder."
    >
      <SettingsCard>
        <SettingsRow
          title="Default recorders"
          description={
            <>Tags or tailnet addresses of the recorder machines, comma separated. {embedded}</>
          }
        >
          <form
            onSubmit={save}
            className="grid max-w-[560px] grid-cols-[1fr_auto] gap-x-2 gap-y-1.5"
          >
            <Input
              aria-label="Default recorders"
              aria-invalid={issue === null ? undefined : true}
              aria-describedby={issue === null ? undefined : issueId}
              variant={issue === null ? "default" : "error"}
              className="min-w-0"
              value={draft}
              placeholder="tag:recorder"
              spellCheck={false}
              autoComplete="off"
              disabled={!canEdit}
              onChange={(event) => {
                setDraft(event.target.value);
                setIssue(null);
              }}
            />
            <Button
              type="submit"
              variant="secondary"
              disabled={!canEdit || !dirty}
              loading={update.isPending}
            >
              Save
            </Button>
            {issue === null ? null : (
              <p id={issueId} className="col-span-full text-sm leading-snug text-kumo-danger">
                {issue}
              </p>
            )}
          </form>
        </SettingsRow>
        <SettingRow
          title="Require recording"
          description="Refuse a session when no recorder can be reached, and end it if the recording stops. Off, the session continues unrecorded and the failure is logged."
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
      </SettingsCard>
    </SettingsGroup>
  );
}
