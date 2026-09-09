import { Banner } from "@cloudflare/kumo/components/banner";
import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { cn } from "@cloudflare/kumo/utils";
import { ArrowSquareOutIcon, WarningCircleIcon, WarningIcon } from "@phosphor-icons/react";
import { useRef, useState } from "react";
import type { ReactElement } from "react";

import type { Policy } from "~/api/queries.ts";
import { policyBlocks } from "~/components/policy/blocks.ts";
import { BuildingBlocks } from "~/components/policy/building-blocks.tsx";
import { LeaveGuard } from "~/components/policy/leave-guard.tsx";
import { PolicyEditor } from "~/components/policy/policy-editor.tsx";
import type { PolicyEditorHandle } from "~/components/policy/policy-editor.tsx";
import { usePolicyDraft } from "~/components/policy/use-policy-draft.ts";
import type { PolicyDraft } from "~/components/policy/use-policy-draft.ts";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { Status } from "~/components/ui/status.tsx";
import { parseTime } from "~/lib/time.ts";

const referenceUrl = "https://aislopware.github.io/slopscale/stable/ref/policy/";

/** The external-link glyph sits after the label, where a link leaving the console shows it. */
const externalIconSize = 14;

/** The one-line state of the draft: a dot and what it means, beside the actions. */
function DraftState({
  dirty,
  updatedAt,
}: {
  readonly dirty: boolean;
  readonly updatedAt: string;
}): ReactElement {
  if (dirty) {
    return <Status tone="warning">Unsaved changes</Status>;
  }

  if (parseTime(updatedAt) === null) {
    return <Status tone="neutral">Never saved</Status>;
  }

  return (
    <Status tone="success">
      Saved <RelativeTime value={updatedAt} />
    </Status>
  );
}

function Actions({
  draft,
  canEdit,
}: {
  readonly draft: PolicyDraft;
  readonly canEdit: boolean;
}): ReactElement {
  return (
    <>
      <LinkButton href={referenceUrl} external variant="ghost">
        Policy reference
        <ArrowSquareOutIcon size={externalIconSize} aria-hidden />
      </LinkButton>
      <Button
        variant="secondary"
        loading={draft.checking}
        onClick={() => {
          draft.check();
        }}
      >
        Check
      </Button>
      <Button
        variant="primary"
        disabled={!draft.dirty || !canEdit}
        loading={draft.saving}
        onClick={() => {
          draft.save();
        }}
      >
        Save
      </Button>
    </>
  );
}

/**
 * The HuJSON policy editor. Rules from the Rules page are added to whatever this file grants, so an
 * empty file is no longer the only way to leave the tailnet open.
 */
export function PolicyFileTab({
  policy,
  canEdit,
  hasRules,
  enforces,
  users,
}: {
  readonly policy: Policy;
  readonly canEdit: boolean;
  /** Whether an enabled rule already restricts the tailnet, which changes what "no policy" means. */
  readonly hasRules: boolean;
  /** Whether the stored file has acls or grants; an empty or tags-only file does not. */
  readonly enforces: boolean;
  /** The users of the tailnet as the policy names them; empty when the caller may not list them. */
  readonly users: readonly string[];
}): ReactElement {
  const draft = usePolicyDraft({ policy, canEdit });
  const unset = policy.policy === "";
  const editor = useRef<PolicyEditorHandle>(null);
  const [blocksOpen, setBlocksOpen] = useState(true);

  return (
    <>
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        <div className="flex min-w-0 flex-col gap-1">
          <p className="text-sm text-kumo-subtle">
            Grants, tags, SSH rules and autogroups written by hand. Rules from the Rules page are
            added on top.
          </p>
          <DraftState dirty={draft.dirty} updatedAt={policy.updatedAt} />
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <Actions draft={draft} canEdit={canEdit} />
        </div>
      </div>
      {draft.issue === null ? null : (
        <Banner
          size="sm"
          variant="error"
          icon={<WarningCircleIcon />}
          title={draft.issue.title}
          description={draft.issue.message}
        />
      )}
      {!enforces && !hasRules ? (
        <Banner
          size="sm"
          variant="alert"
          icon={<WarningIcon />}
          title={unset ? "No policy is set" : "The policy file restricts nothing"}
          description="Every machine can reach every other machine until a rule is enabled or this file adds acls or grants."
          {...(canEdit && draft.text === ""
            ? {
                action: (
                  <Banner.Action
                    variant="ghost"
                    onClick={() => {
                      draft.fillTemplate();
                    }}
                  >
                    Start from a template
                  </Banner.Action>
                ),
              }
            : {})}
        />
      ) : null}
      <div
        className={cn(
          "grid min-w-0 gap-4",
          // Closed, the panel shrinks to its title so the editor takes the width back.
          blocksOpen ? "xl:grid-cols-[minmax(0,1fr)_18rem]" : "xl:grid-cols-[minmax(0,1fr)_auto]",
        )}
      >
        <PolicyEditor
          ref={editor}
          value={draft.text}
          onChange={(next) => {
            draft.setText(next);
          }}
          readOnly={!canEdit}
          dirty={draft.dirty}
          problems={draft.problems}
          verifying={draft.verifying}
          users={users}
          onDiscard={() => {
            draft.discard();
          }}
        />
        <BuildingBlocks
          blocks={policyBlocks(draft.text)}
          open={blocksOpen}
          onOpenChange={setBlocksOpen}
          onSelect={(name) => {
            editor.current?.reveal(name);
          }}
        />
      </div>
      <LeaveGuard dirty={draft.dirty} />
    </>
  );
}
