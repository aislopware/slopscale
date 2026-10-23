import { Banner } from "@cloudflare/kumo/components/banner";
import { Button } from "@cloudflare/kumo/components/button";
import { cn } from "@cloudflare/kumo/utils";
import { useRef, useState } from "react";
import type { ReactElement } from "react";

import type { Policy } from "~/api/queries.ts";
import { policyBlocks } from "~/components/policy/blocks.ts";
import { BuildingBlocks } from "~/components/policy/building-blocks.tsx";
import { PolicyEditor } from "~/components/policy/policy-editor.tsx";
import type { PolicyEditorHandle } from "~/components/policy/policy-editor.tsx";
import { usePolicyDraft } from "~/components/policy/use-policy-draft.ts";
import type { PolicyDraft } from "~/components/policy/use-policy-draft.ts";
import { Callout } from "~/components/ui/callout.tsx";
import { FormError } from "~/components/ui/form-page.tsx";
import { LeaveGuard } from "~/components/ui/leave-guard.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { Status } from "~/components/ui/status.tsx";
import { parseTime } from "~/lib/time.ts";

/** The one-line state of the draft: a dot and what it means, under the title. */
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
 * The HuJSON policy as the dashboard lays out a code-editing page: Check and Save in the page
 * header with the state of the draft under the title, the file in an editor card with its toolbar
 * and its Problems list, and the names it defines beside it. Rules from the Rules page are added to
 * whatever this file grants, so an empty file is no longer the only way to leave the tailnet open.
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
      <PageHeader
        title="Policy file"
        docs="ref/policy"
        description="Grants, tags, SSH rules and autogroups written by hand in HuJSON. Rules from the Rules page are added on top."
        meta={<DraftState dirty={draft.dirty} updatedAt={policy.updatedAt} />}
        actions={<Actions draft={draft} canEdit={canEdit} />}
      />
      <FormError title={draft.issue?.title} message={draft.issue?.message} />
      {!enforces && !hasRules ? (
        <Callout
          tone="warning"
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
      <LeaveGuard
        dirty={draft.dirty}
        description="The policy has changes that are not saved. Leaving drops them."
      />
    </>
  );
}
