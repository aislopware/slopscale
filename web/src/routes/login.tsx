import { Banner } from "@cloudflare/kumo/components/banner";
import { Button } from "@cloudflare/kumo/components/button";
import { SignInIcon } from "@phosphor-icons/react";
import { createFileRoute, redirect } from "@tanstack/react-router";
import { useEffect } from "react";
import type { ReactElement } from "react";
import { object, optional, string } from "valibot";

import { ApiError } from "~/api/error.ts";
import { consoleAuthQuery, meQuery } from "~/auth/me.ts";
import { consolePath } from "~/auth/session.ts";
import { Mark } from "~/components/layout/mark.tsx";
import { ThemeToggle } from "~/components/layout/theme-toggle.tsx";
import { Callout } from "~/components/ui/callout.tsx";
import { Code } from "~/components/ui/code.tsx";
import { Frame, FramePanel } from "~/components/ui/frame.tsx";

const searchSchema = object({
  redirect: optional(string()),
  /** The token of an invitation link, which the sign-in carries through to the server. */
  invite: optional(string()),
  /** Why a sign-in that already started came back here. */
  error: optional(string()),
});

/**
 * The reasons the server sends someone back to this page. Only these are spelled out: the value
 * comes from the URL, so an unknown one is answered with a sentence of the console's own rather
 * than with whatever it says.
 */
const signInProblems: Record<string, string> = {
  invite_expired: "That invitation has expired. Ask whoever invited you for a new link.",
  invite_revoked: "That invitation was revoked. Ask whoever invited you for a new link.",
  invite_used: "That invitation has already been used. Sign in with the account it created.",
};

const genericProblem = "Sign-in did not finish. Try again.";

/**
 * Where to go after signing in. The value comes from the URL, so only a path of the console's own
 * is followed; a full address or a protocol-relative one would lead out of it.
 */
function safeTarget(target: string | undefined): string {
  return target !== undefined && target.startsWith("/") && !target.startsWith("//") ? target : "/";
}

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

    throw redirect({ to: safeTarget(search.redirect) });
  },
  loader: ({ context }) => context.queryClient.query(consoleAuthQuery),
  component: LoginPage,
});

function signInProblem(error: string | undefined): string | undefined {
  if (error === undefined) {
    return undefined;
  }

  return Object.hasOwn(signInProblems, error) ? signInProblems[error] : genericProblem;
}

function LoginPage(): ReactElement {
  const { redirect: target, invite, error } = Route.useSearch();
  const { oidc } = Route.useLoaderData();
  const problem = signInProblem(error);

  useEffect(() => {
    document.title = "Sign in - Slopscale";
  }, []);

  return (
    <div className="flex min-h-dvh flex-col bg-kumo-canvas">
      <header className="flex items-center justify-end px-6 py-4">
        <ThemeToggle />
      </header>
      <main className="flex flex-1 items-center justify-center px-4 pb-24">
        <div className="flex w-full max-w-sm flex-col gap-4">
          {/* In dark mode the canvas is nearly black, so the card needs a ring and a shadow of its
              own to read as a surface. */}
          <Frame className="shadow-lg ring-kumo-line">
            <FramePanel className="flex flex-col gap-6 px-6 py-6">
              <div className="flex flex-col gap-3">
                <Mark className="size-8 text-kumo-brand" />
                <div className="flex flex-col gap-1">
                  <h1 className="text-xl font-semibold text-kumo-strong">Sign in to slopscale</h1>
                  <p className="text-kumo-subtle">
                    {oidc === undefined
                      ? "This server has no identity provider, so the console cannot sign anyone in."
                      : "Sign in with the account your administrator gave you."}
                  </p>
                </div>
              </div>
              {problem === undefined ? null : (
                <Banner variant="error" title="Sign-in failed" description={problem} />
              )}
              {invite === undefined || invite === "" ? null : (
                <Callout title="Sign in to accept your invitation." />
              )}
              {oidc === undefined ? (
                <Banner
                  variant="alert"
                  title="No identity provider"
                  description={
                    <>
                      Set the <Code>oidc</Code> section of the config file and restart. The CLI and
                      the API keep working with API keys.
                    </>
                  }
                />
              ) : (
                <Button
                  variant="primary"
                  size="lg"
                  className="w-full"
                  icon={SignInIcon}
                  onClick={() => {
                    // The provider flow is served by slopscale, not routed by
                    // the console, so this is a full navigation.
                    globalThis.location.assign(loginUrl(oidc.loginPath, target, invite));
                  }}
                >
                  Continue with {oidc.provider}
                </Button>
              )}
              <p className="text-xs text-kumo-subtle">
                {oidc === undefined
                  ? "See the OpenID Connect documentation."
                  : "A sign-in lasts seven days in this browser."}
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

/**
 * Where the sign-in button sends the browser: the provider flow on the server, carrying where to
 * land afterwards and, when the operator followed an invitation link, its token. The server keeps
 * the token under its own state and consumes it once the identity is known.
 */
function loginUrl(
  loginPath: string,
  target: string | undefined,
  invite: string | undefined,
): string {
  const query = new URLSearchParams({ redirect: consolePath(safeTarget(target)) });

  if (invite !== undefined && invite !== "") {
    query.set("invite", invite);
  }

  return `${loginPath}?${query.toString()}`;
}
