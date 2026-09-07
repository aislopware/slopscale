import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";

import { api } from "~/api/client.ts";
import { errorMessage } from "~/api/error.ts";
import { invalidate } from "~/api/queries.ts";
import type { Policy } from "~/api/queries.ts";
import { toast } from "~/components/ui/toast.ts";

export interface PolicyIssue {
  readonly title: string;
  readonly message: string;
}

/** A permissive first policy that names the building blocks an operator will edit. */
export const starterPolicy = `{
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

export interface PolicyDraft {
  readonly text: string;
  readonly setText: (next: string) => void;
  /** The draft differs from the stored policy, so Save and the leave guard are live. */
  readonly dirty: boolean;
  readonly issue: PolicyIssue | null;
  readonly checking: boolean;
  readonly saving: boolean;
  readonly check: () => void;
  readonly save: () => void;
  readonly discard: () => void;
  readonly fillTemplate: () => void;
}

function isSaveShortcut(event: KeyboardEvent): boolean {
  return event.key.toLowerCase() === "s" && (event.metaKey || event.ctrlKey);
}

/**
 * The draft of the HuJSON policy, with the check and save calls that act on it. The server copy
 * stays the reference the "Unsaved changes" state and the leave guard compare against.
 */
export function usePolicyDraft({
  policy,
  canEdit,
}: {
  readonly policy: Policy;
  readonly canEdit: boolean;
}): PolicyDraft {
  const queryClient = useQueryClient();
  const [text, setText] = useState(policy.policy);
  const [issue, setIssue] = useState<PolicyIssue | null>(null);
  const dirty = text !== policy.policy;

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
      setText(saved.policy);
      toast.success("Policy saved");
      await invalidate(queryClient, "/api/v1/policy");
    },
    onError: (error) => {
      setIssue({ title: "Could not save the policy", message: errorMessage(error) });
    },
  });

  function submit(): void {
    if (dirty && canEdit) {
      save.mutate({ body: { policy: text } });
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

  return {
    text,
    setText,
    dirty,
    issue,
    checking: check.isPending,
    saving: save.isPending,
    check: () => {
      check.mutate({ body: { policy: text } });
    },
    save: submit,
    discard: () => {
      setText(policy.policy);
      setIssue(null);
    },
    fillTemplate: () => {
      setText(starterPolicy);
    },
  };
}
