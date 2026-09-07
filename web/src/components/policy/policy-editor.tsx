import { Badge } from "@cloudflare/kumo/components/badge";
import { Banner } from "@cloudflare/kumo/components/banner";
import { Button } from "@cloudflare/kumo/components/button";
import { WarningCircleIcon, WarningIcon } from "@phosphor-icons/react";
import { useQueryClient } from "@tanstack/react-query";
import { useBlocker } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import type { ReactElement } from "react";

import { api } from "~/api/client.ts";
import { errorMessage } from "~/api/error.ts";
import { invalidate } from "~/api/queries.ts";
import type { Policy } from "~/api/queries.ts";
import { Card, CardHeader } from "~/components/ui/card.tsx";
import { CodeEditor } from "~/components/ui/code-editor.tsx";
import { ConfirmDialog } from "~/components/ui/confirm-dialog.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { toast } from "~/components/ui/toast.ts";
import { parseTime } from "~/lib/time.ts";

interface Issue {
  readonly title: string;
  readonly message: string;
}

function isSaveShortcut(event: KeyboardEvent): boolean {
  return event.key.toLowerCase() === "s" && (event.metaKey || event.ctrlKey);
}

/** Asks before a navigation would drop unsaved edits, and warns on reload too. */
function LeaveGuard({ dirty }: { readonly dirty: boolean }): ReactElement {
  const blocker = useBlocker({
    shouldBlockFn: () => dirty,
    enableBeforeUnload: dirty,
    withResolver: true,
  });

  return (
    <ConfirmDialog
      open={blocker.status === "blocked"}
      onOpenChange={(open) => {
        if (!open) {
          blocker.reset?.();
        }
      }}
      title="Leave without saving?"
      description="The policy has unsaved changes. They are lost if you leave this page."
      confirmLabel="Leave"
      onConfirm={() => {
        blocker.proceed?.();
      }}
    />
  );
}

interface ToolbarProps {
  readonly updatedAt: string;
  readonly dirty: boolean;
  readonly canEdit: boolean;
  readonly checking: boolean;
  readonly saving: boolean;
  readonly onDiscard: () => void;
  readonly onCheck: () => void;
  readonly onSave: () => void;
}

function Toolbar({
  updatedAt,
  dirty,
  canEdit,
  checking,
  saving,
  onDiscard,
  onCheck,
  onSave,
}: ToolbarProps): ReactElement {
  return (
    <CardHeader className="flex-wrap items-center py-2.5">
      <div className="flex items-center gap-2 text-kumo-subtle">
        {parseTime(updatedAt) === null ? (
          <span>Never saved</span>
        ) : (
          <span>
            Updated <RelativeTime value={updatedAt} />
          </span>
        )}
        {dirty ? <Badge variant="warning">Modified</Badge> : null}
      </div>
      <div className="flex items-center gap-2">
        <Button variant="ghost" size="sm" disabled={!dirty} onClick={onDiscard}>
          Discard
        </Button>
        <Button variant="secondary" size="sm" loading={checking} onClick={onCheck}>
          Check
        </Button>
        <Button
          variant="primary"
          size="sm"
          disabled={!dirty || !canEdit}
          loading={saving}
          onClick={onSave}
        >
          Save
        </Button>
      </div>
    </CardHeader>
  );
}

export /** A permissive first policy that names the building blocks an operator will edit. */
const starterPolicy = `{
  // Groups collect users; tags label machines that no user owns.
  "groups": {
    "group:admin": [],
  },
  "tagOwners": {
    "tag:server": ["group:admin"],
  },

  // Grants replace ACLs: who may reach what, on which ports.
  "grants": [
    { "src": ["autogroup:member"], "dst": ["autogroup:self"], "ip": ["*"] },
    { "src": ["group:admin"], "dst": ["*"], "ip": ["*"] },
    { "src": ["autogroup:shared"], "dst": ["autogroup:member"], "ip": ["*"] },
  ],

  // Who may SSH where, when the client runs Tailscale SSH.
  "ssh": [
    { "action": "check", "src": ["group:admin"], "dst": ["tag:server"], "users": ["autogroup:nonroot", "root"] },
  ],
}
`;

interface PolicyEditorProps {
  readonly policy: Policy;
  readonly canEdit: boolean;
}

/**
 * The HuJSON policy editor. The draft lives here; the server copy stays the reference the
 * "Modified" badge and the leave guard compare against.
 */
export function PolicyEditor({ policy, canEdit }: PolicyEditorProps): ReactElement {
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState(policy.policy);
  const [issue, setIssue] = useState<Issue | null>(null);
  const dirty = draft !== policy.policy;

  const check = api.useMutation("post", "/api/v1/policy/check", {
    onSuccess: () => {
      setIssue(null);
      toast.success("Policy is valid");
    },
    onError: (error) => {
      setIssue({ title: "Policy is not valid", message: errorMessage(error) });
    },
  });

  const save = api.useMutation("put", "/api/v1/policy", {
    onSuccess: async (saved) => {
      setIssue(null);
      setDraft(saved.policy);
      toast.success("Policy saved");
      await invalidate(queryClient, "/api/v1/policy");
    },
    onError: (error) => {
      setIssue({ title: "Could not save the policy", message: errorMessage(error) });
    },
  });

  function submit(): void {
    if (dirty && canEdit) {
      save.mutate({ body: { policy: draft } });
    }
  }

  const commit = useRef(submit);

  useEffect(() => {
    commit.current = submit;
  });

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent): void {
      if (isSaveShortcut(event)) {
        event.preventDefault();
        commit.current();
      }
    }

    globalThis.addEventListener("keydown", onKeyDown);

    return (): void => {
      globalThis.removeEventListener("keydown", onKeyDown);
    };
  }, []);

  return (
    <div className="flex flex-col gap-4">
      {issue === null ? null : (
        <Banner
          variant="error"
          icon={<WarningCircleIcon />}
          title={issue.title}
          description={issue.message}
        />
      )}
      {policy.policy === "" ? (
        <Banner
          variant="alert"
          icon={<WarningIcon />}
          title="No policy is set"
          description="Every user can reach every device until a policy is saved."
          {...(canEdit && draft === ""
            ? {
                action: (
                  <Banner.Action
                    variant="secondary"
                    onClick={() => {
                      setDraft(starterPolicy);
                    }}
                  >
                    Start from a template
                  </Banner.Action>
                ),
              }
            : {})}
        />
      ) : null}
      <Card className="overflow-hidden">
        <Toolbar
          updatedAt={policy.updatedAt}
          dirty={dirty}
          canEdit={canEdit}
          checking={check.isPending}
          saving={save.isPending}
          onDiscard={() => {
            setDraft(policy.policy);
            setIssue(null);
          }}
          onCheck={() => {
            check.mutate({ body: { policy: draft } });
          }}
          onSave={submit}
        />
        <div className="h-[60vh] min-h-96">
          <CodeEditor
            value={draft}
            onChange={setDraft}
            readOnly={!canEdit}
            aria-label="Tailnet policy"
          />
        </div>
      </Card>
      <LeaveGuard dirty={dirty} />
    </div>
  );
}
