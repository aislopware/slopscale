import { Button } from "@cloudflare/kumo/components/button";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { SensitiveInput } from "@cloudflare/kumo/components/sensitive-input";
import { KeyIcon } from "@phosphor-icons/react";
import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";
import { object, optional, string } from "valibot";

import { fetchClient } from "~/api/client.ts";
import { errorMessage } from "~/api/error.ts";
import { session } from "~/auth/session.ts";
import { ThemeToggle } from "~/components/layout/theme-toggle.tsx";

const searchSchema = object({
  redirect: optional(string()),
});

export const Route = createFileRoute("/login")({
  validateSearch: searchSchema,
  beforeLoad: ({ search }) => {
    if (session.get() !== null) {
      throw redirect({ to: search.redirect ?? "/" });
    }
  },
  component: LoginPage,
});

function LoginPage(): ReactElement {
  const { redirect: target } = Route.useSearch();
  const navigate = useNavigate();
  const [apiKey, setApiKey] = useState("");
  const [message, setMessage] = useState<string>();
  const [busy, setBusy] = useState(false);

  async function submit(event: SubmitEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    const key = apiKey.trim();

    if (key === "") {
      setMessage("Paste an API key first.");

      return;
    }

    setBusy(true);
    setMessage(undefined);
    session.set(key);

    try {
      await fetchClient.GET("/api/v1/whoami");
    } catch (error) {
      session.clear();
      setMessage(errorMessage(error));
      setBusy(false);

      return;
    }

    setBusy(false);
    await navigate({ to: target ?? "/" });
  }

  return (
    <div className="flex min-h-dvh flex-col bg-kumo-canvas">
      <header className="flex items-center justify-end p-4">
        <ThemeToggle />
      </header>
      <main className="flex flex-1 items-center justify-center px-4 pb-24">
        <div className="flex w-full max-w-sm flex-col gap-6">
          <div className="flex flex-col items-center gap-3 text-center">
            <span className="flex size-11 items-center justify-center rounded-xl bg-kumo-contrast text-kumo-inverse">
              <KeyIcon size={22} weight="bold" />
            </span>
            <div className="flex flex-col gap-1">
              <h1 className="text-xl font-semibold text-kumo-default">Sign in to headscale</h1>
              <p className="text-kumo-subtle">
                Use an API key. The role of its owner decides what this console can show and change.
              </p>
            </div>
          </div>
          <LayerCard className="px-5 py-4">
            <form
              onSubmit={(event) => {
                void submit(event);
              }}
              className="flex flex-col gap-4"
            >
              <SensitiveInput
                label="API key"
                description={
                  <>
                    Create one with{" "}
                    <span className="font-mono text-[0.9em]">headscale apikeys create</span>.
                  </>
                }
                autoComplete="current-password"
                spellCheck={false}
                value={apiKey}
                onValueChange={setApiKey}
                {...(message === undefined ? {} : { error: message, variant: "error" as const })}
              />
              <Button type="submit" variant="primary" loading={busy} className="w-full">
                Continue
              </Button>
            </form>
          </LayerCard>
          <p className="text-center text-xs text-kumo-subtle">
            The key stays in this browser only and is sent as a bearer token to this server.
          </p>
        </div>
      </main>
    </div>
  );
}
