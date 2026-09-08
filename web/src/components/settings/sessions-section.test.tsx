import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactElement } from "react";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import type { ConsoleSession } from "~/api/queries.ts";
import { SessionsBody } from "~/components/settings/sessions-section.tsx";

const stamp = "2026-09-07T00:00:00Z";

const ada: ConsoleSession["user"] = {
  id: "1",
  name: "ada",
  displayName: "Ada Lovelace",
  email: "ada@example.com",
  provider: "oidc",
  providerId: "ada",
  profilePicUrl: "",
  role: "owner",
  approved: true,
  approvedAt: stamp,
  createdAt: stamp,
};

const chrome =
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36";

const mine: ConsoleSession = {
  id: "10",
  user: ada,
  current: true,
  createdAt: stamp,
  expiresAt: "2026-09-14T00:00:00Z",
  lastSeenAt: stamp,
  remoteAddr: "10.0.0.9",
  userAgent: chrome,
};

const older: ConsoleSession = {
  ...mine,
  id: "11",
  current: false,
  remoteAddr: "",
  userAgent: "",
};

function app(body: ReactElement): ReactElement {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  return <QueryClientProvider client={client}>{body}</QueryClientProvider>;
}

describe(SessionsBody, () => {
  it("shows each session with its browser, address and an end action", async () => {
    const screen = await render(app(<SessionsBody sessions={[mine, older]} error={undefined} />));

    await expect.element(screen.getByText("Chrome on macOS")).toBeVisible();
    await expect.element(screen.getByText("10.0.0.9")).toBeVisible();
    await expect.element(screen.getByText("This browser")).toBeVisible();
    await expect
      .element(screen.getByRole("button", { name: "End the session of Ada Lovelace" }).first())
      .toBeVisible();
  });

  it("says so when a session carries no address or browser", async () => {
    const screen = await render(app(<SessionsBody sessions={[older]} error={undefined} />));

    await expect.element(screen.getByText("Not recorded")).toBeVisible();
    await expect.element(screen.getByText("Unknown")).toBeVisible();
    await expect.element(screen.getByText("This browser")).not.toBeInTheDocument();
  });

  it("warns that ending the current session signs this browser out", async () => {
    const screen = await render(app(<SessionsBody sessions={[mine]} error={undefined} />));

    await screen.getByRole("button", { name: "End the session of Ada Lovelace" }).click();

    await expect.element(screen.getByRole("alertdialog")).toBeVisible();
    await expect.element(screen.getByText(/lands back on the sign-in page/u)).toBeVisible();
  });

  it("reports a list that could not be read, and an empty one", async () => {
    const failed = await render(
      app(<SessionsBody sessions={undefined} error={new Error("Forbidden")} />),
    );

    await expect.element(failed.getByText("Forbidden")).toBeVisible();

    const none = await render(app(<SessionsBody sessions={[]} error={undefined} />));

    await expect.element(none.getByText("No console session is open.")).toBeVisible();
  });
});
