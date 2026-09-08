import { Banner } from "@cloudflare/kumo/components/banner";
import { Button } from "@cloudflare/kumo/components/button";
import { SignInIcon, WaveformIcon } from "@phosphor-icons/react";
import { createFileRoute, redirect } from "@tanstack/react-router";
import type { ReactElement } from "react";
import { object, optional, string } from "valibot";

import { ApiError } from "~/api/error.ts";
import { consoleAuthQuery, meQuery } from "~/auth/me.ts";
import { consolePath } from "~/auth/session.ts";
import { ThemeToggle } from "~/components/layout/theme-toggle.tsx";
import { Frame, FramePanel } from "~/components/ui/frame.tsx";

const searchSchema = object({
  redirect: optional(string()),
});

export const Route = createFileRoute("/login")({
  validateSearch: searchSchema,
  // Whether the operator is already signed in is the server's answer: the session cookie is
  // invisible to the console. A 401 means "show the page"; anything else is a real error.
  beforeLoad: async ({ context, search }) => {
    try {
      await context.queryClient.query({ ...meQuery, staleTime: "static" });
    } catch (error) {
      if (error instanceof ApiError && error.unauthorized) {
        return;
      }

      throw error;
    }

    throw redirect({ to: search.redirect ?? "/" });
  },
  loader: ({ context }) => context.queryClient.query(consoleAuthQuery),
  component: LoginPage,
});

function LoginPage(): ReactElement {
  const { redirect: target } = Route.useSearch();
  const { oidc } = Route.useLoaderData();

  return (
    <div className="flex min-h-dvh flex-col bg-kumo-canvas">
      <header className="flex items-center justify-end px-6 py-4">
        <ThemeToggle />
      </header>
      <main className="flex flex-1 items-center justify-center px-4 pb-24">
        <div className="flex w-full max-w-sm flex-col gap-4">
          <Frame>
            <FramePanel className="flex flex-col gap-6 px-6 py-6">
              <div className="flex flex-col gap-3">
                <WaveformIcon className="size-8 text-kumo-brand" weight="duotone" />
                <div className="flex flex-col gap-1">
                  <h1 className="text-xl font-semibold text-kumo-strong">Sign in to headscale</h1>
                  <p className="text-kumo-subtle">
                    {oidc === undefined
                      ? "This server has no identity provider, so the console cannot sign anyone in."
                      : "Use the account your administrator gave access to."}
                  </p>
                </div>
              </div>
              {oidc === undefined ? (
                <Banner
                  variant="alert"
                  title="No identity provider"
                  description="Set the oidc section of the server configuration and restart it. The CLI and the API keep working with API keys."
                />
              ) : (
                <Button
                  variant="primary"
                  size="lg"
                  className="w-full"
                  icon={SignInIcon}
                  onClick={() => {
                    // The provider flow is served by headscale, not routed by
                    // the console, so this is a full navigation.
                    globalThis.location.assign(
                      `${oidc.loginPath}?redirect=${encodeURIComponent(consolePath(target ?? "/"))}`,
                    );
                  }}
                >
                  Continue with {oidc.provider}
                </Button>
              )}
              <p className="text-xs text-kumo-subtle">
                {oidc === undefined
                  ? "See the OpenID Connect page of the documentation."
                  : "A sign-in lasts seven days in this browser. Sign out from the account menu to end it sooner."}
              </p>
            </FramePanel>
          </Frame>
          <p className="text-center text-xs text-kumo-subtle">
            <span className="font-mono text-[0.9em]">{globalThis.location.host}</span>
          </p>
        </div>
      </main>
    </div>
  );
}
