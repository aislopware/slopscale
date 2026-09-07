import { Badge } from "@cloudflare/kumo/components/badge";
import { Banner } from "@cloudflare/kumo/components/banner";
import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { cn } from "@cloudflare/kumo/utils";
import { ArrowSquareOutIcon, WarningCircleIcon, WarningIcon } from "@phosphor-icons/react";
import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useRef, useState } from "react";
import type { ReactElement } from "react";

import { policyQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { policyBlocks } from "~/components/policy/blocks.ts";
import { BuildingBlocks } from "~/components/policy/building-blocks.tsx";
import { LeaveGuard } from "~/components/policy/leave-guard.tsx";
import { PolicyEditor } from "~/components/policy/policy-editor.tsx";
import type { PolicyEditorHandle } from "~/components/policy/policy-editor.tsx";
import { usePolicyDraft } from "~/components/policy/use-policy-draft.ts";
import type { PolicyDraft } from "~/components/policy/use-policy-draft.ts";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { parseTime } from "~/lib/time.ts";

const referenceUrl = "https://headscale.net/stable/ref/policy/";

export const Route = createFileRoute("/_app/policy")({
  loader: async ({ context }) => {
    await context.queryClient.query(policyQuery);
  },
  component: PolicyPage,
});

/** The one-line state of the draft: a dot and what it means, under the title. */
function DraftState({
  dirty,
  updatedAt,
}: {
  readonly dirty: boolean;
  readonly updatedAt: string;
}): ReactElement {
  if (dirty) {
    return (
      <Badge appearance="dot" variant="warning">
        Unsaved changes
      </Badge>
    );
  }

  if (parseTime(updatedAt) === null) {
    return (
      <Badge appearance="dot" variant="neutral">
        Never saved
      </Badge>
    );
  }

  return (
    <Badge appearance="dot" variant="success">
      Saved <RelativeTime value={updatedAt} />
    </Badge>
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
      <LinkButton href={referenceUrl} external variant="ghost" icon={ArrowSquareOutIcon}>
        Policy reference
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

function PolicyPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const policy = useSuspenseQuery(policyQuery).data;
  const canEdit = can(me, "policy_file");
  const draft = usePolicyDraft({ policy, canEdit });
  const unset = policy.policy === "";
  const editor = useRef<PolicyEditorHandle>(null);
  const [blocksOpen, setBlocksOpen] = useState(true);

  return (
    <>
      <PageHeader
        title="Access controls"
        description="The tailnet policy in HuJSON: grants, groups, tags, SSH rules and autogroups."
        meta={<DraftState dirty={draft.dirty} updatedAt={policy.updatedAt} />}
        actions={<Actions draft={draft} canEdit={canEdit} />}
      />
      {draft.issue === null ? null : (
        <Banner
          size="sm"
          variant="error"
          icon={<WarningCircleIcon />}
          title={draft.issue.title}
          description={draft.issue.message}
        />
      )}
      {unset ? (
        <Banner
          size="sm"
          variant="alert"
          icon={<WarningIcon />}
          title="No policy is set"
          description="Every user can reach every device until a policy is saved."
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
