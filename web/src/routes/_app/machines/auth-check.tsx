import { Banner } from "@cloudflare/kumo/components/banner";
import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { CheckCircleIcon, XCircleIcon } from "@phosphor-icons/react";
import { createFileRoute, redirect } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";
import { fallback, object, optional, string } from "valibot";

import { api } from "~/api/client.ts";
import { errorMessage } from "~/api/error.ts";
import { can } from "~/auth/me.ts";
import { DialogError } from "~/components/ui/dialog.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

const optionalText = optional(fallback(string(), ""), "");
const searchSchema = object({ id: optionalText });

export const Route = createFileRoute("/_app/machines/auth-check")({
  validateSearch: searchSchema,
  beforeLoad: ({ context }) => {
    if (!can(context.me, "devices:core")) {
      throw redirect({ to: "/machines" });
    }
  },
  component: AuthCheckPage,
});

type Verdict = "approved" | "rejected";

/**
 * Decides an SSH check-mode request: a policy rule with check mode sends the person at the source
 * machine to a page carrying this request id, and an operator lets the session through or refuses
 * it here. The request lives only until the source gives up waiting.
 */
function AuthCheckPage(): ReactElement {
  useBreadcrumb("Authentication check");
  const search = Route.useSearch();
  const [authId, setAuthId] = useState(search.id);
  const [verdict, setVerdict] = useState<Verdict | null>(null);
  const approve = api.useMutation("post", "/api/v1/auth/approve", {
    onSuccess: () => {
      setVerdict("approved");
    },
  });
  const reject = api.useMutation("post", "/api/v1/auth/reject", {
    onSuccess: () => {
      setVerdict("rejected");
    },
  });
  const pending = approve.isPending || reject.isPending;
  const failure = [approve, reject].find((mutation) => mutation.isError);

  return (
    <>
      <PageHeader
        title="Authentication check"
        description="An SSH rule in check mode is holding a session until someone approves it. Approve to let this one through, or reject to refuse it."
      />
      <LayerCard className="flex max-w-xl flex-col gap-4 p-5">
        {verdict === null ? (
          <>
            <Input
              label="Request id"
              description="From the page the machine's SSH client opened."
              value={authId}
              required
              spellCheck={false}
              className="font-mono"
              onChange={(event) => {
                setAuthId(event.target.value);
              }}
            />
            <DialogError
              message={failure === undefined ? undefined : errorMessage(failure.error)}
            />
            <div className="flex justify-end gap-2">
              <Button
                variant="secondary"
                icon={XCircleIcon}
                disabled={authId.trim() === "" || pending}
                loading={reject.isPending}
                onClick={() => {
                  reject.mutate({ body: { authId: authId.trim() } });
                }}
              >
                Reject
              </Button>
              <Button
                variant="primary"
                icon={CheckCircleIcon}
                disabled={authId.trim() === "" || pending}
                loading={approve.isPending}
                onClick={() => {
                  approve.mutate({ body: { authId: authId.trim() } });
                }}
              >
                Approve
              </Button>
            </div>
          </>
        ) : (
          <Banner
            icon={
              verdict === "approved" ? (
                <CheckCircleIcon weight="fill" />
              ) : (
                <XCircleIcon weight="fill" />
              )
            }
            title={verdict === "approved" ? "Session approved" : "Session rejected"}
            description={
              verdict === "approved"
                ? "The SSH client waiting on this request continues now."
                : "The SSH client waiting on this request is refused."
            }
          />
        )}
      </LayerCard>
    </>
  );
}
