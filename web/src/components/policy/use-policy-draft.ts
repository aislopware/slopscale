import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";

import { api } from "~/api/client.ts";
import { errorMessage } from "~/api/error.ts";
import { invalidate } from "~/api/queries.ts";
import type { Policy } from "~/api/queries.ts";
import { lintPolicy } from "~/components/policy/lint.ts";
import { serverProblems } from "~/components/policy/server-problems.ts";
import { toast } from "~/components/ui/toast.ts";
import type { EditorProblem } from "~/lib/editor/problems.ts";

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

/** How long typing pauses before the draft goes to the server for the checks only it can do. */
const verifyDelayMs = 800;

export interface PolicyDraft {
  readonly text: string;
  readonly setText: (next: string) => void;
  /** The draft differs from the stored policy, so Save and the leave guard are live. */
  readonly dirty: boolean;
  readonly issue: PolicyIssue | null;
  /** Everything wrong with the draft: what the linter sees as it is typed, plus the server's word. */
  readonly problems: readonly EditorProblem[];
  /** The draft is on its way to the server for the quiet check. */
  readonly verifying: boolean;
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

/** What the server said about a draft, kept with the draft it was about. */
interface Verdict {
  readonly text: string;
  readonly problems: readonly EditorProblem[];
}

const noProblems: readonly EditorProblem[] = [];

function noCleanup(): void {
  // Nothing was started, so there is nothing to stop.
}

/**
 * The draft of the HuJSON policy, with the check and save calls that act on it. The server copy
 * stays the reference the "Unsaved changes" state and the leave guard compare against. The linter
 * runs on every change; once it is satisfied and typing pauses, the draft goes to the server
 * quietly, for the checks that need the tailnet: users that exist, tags in use.
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
  const [verdict, setVerdict] = useState<Verdict | null>(null);
  const dirty = text !== policy.policy;
  const lint = useMemo(() => lintPolicy(text), [text]);
  const clean = lint.problems.every((problem) => problem.severity !== "error");

  const check = api.useMutation("post", "/api/v1/policy/check", {
    onSuccess: () => {
      setIssue(null);
      toast.success("Policy is valid");
    },
    onError: (error) => {
      setIssue({ title: "Policy is not valid", message: errorMessage(error) });
    },
  });

  const verify = api.useMutation("post", "/api/v1/policy/check");
  const { mutate: sendForVerdict } = verify;

  useEffect(() => {
    if (!clean || text.trim() === "") {
      return noCleanup;
    }

    const timer = setTimeout(() => {
      sendForVerdict(
        { body: { policy: text } },
        {
          onSuccess: () => {
            setVerdict({ text, problems: noProblems });
          },
          onError: (error) => {
            setVerdict({ text, problems: serverProblems(text, errorMessage(error)) });
          },
        },
      );
    }, verifyDelayMs);

    return (): void => {
      clearTimeout(timer);
    };
  }, [text, clean, sendForVerdict]);

  const problems = useMemo(
    () => [...lint.problems, ...(verdict?.text === text ? verdict.problems : noProblems)],
    [lint, verdict, text],
  );
  // From the first keystroke until the server has answered about this exact text, the draft is
  // being checked: the pause before the request counts, or "No problems" would show too early.
  const verifying = clean && text.trim() !== "" && verdict?.text !== text;

  const save = api.useMutation("put", "/api/v1/policy", {
    onSuccess: async (saved) => {
      setIssue(null);
      setText(saved.policy);
      toast.success("Policy saved");
      await invalidate(
        queryClient,
        "/api/v1/policy",
        "/api/v1/node",
        "/api/v1/access-rule",
        "/api/v1/network",
        // The graph is read off the rules the policy compiles to.
        "/api/v1/access-graph",
      );
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
    problems,
    verifying,
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
