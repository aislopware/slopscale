import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import type { Node } from "~/api/queries.ts";
import { GetStarted } from "~/components/overview/get-started.tsx";
import { MetricTiles, preferredGlobalExitNode } from "~/components/overview/metric-tiles.tsx";
import { NeedsAttention } from "~/components/overview/needs-attention.tsx";
import {
  admin,
  alice,
  app,
  gateway,
  laptop,
  newcomer,
  reader,
} from "~/components/overview/overview-fixtures.tsx";

describe(MetricTiles, () => {
  it("counts machines, approvals, users and exit nodes", async () => {
    const screen = await render(
      app(
        <MetricTiles
          nodes={[laptop, gateway]}
          users={[alice, newcomer]}
          requests={undefined}
          nodesLoading={false}
          usersLoading={false}
        />,
      ),
    );

    await expect.element(screen.getByRole("link", { name: /Machines/u })).toBeVisible();
    await expect.element(screen.getByText("1 connected")).toBeVisible();
    await expect.element(screen.getByText("1 machine, 1 user")).toBeVisible();
    await expect.element(screen.getByText("1 waiting")).toBeVisible();
    await expect.element(screen.getByText("No global exit node")).toBeVisible();
  });

  it("names the global exit node clients take first", async () => {
    const office: Node = {
      ...gateway,
      id: "10",
      givenName: "office",
      name: "office",
      globalExitNode: true,
      exitNodePriority: 20,
    };
    const dc: Node = {
      ...gateway,
      id: "11",
      givenName: "dc",
      name: "dc",
      globalExitNode: true,
      exitNodePriority: 10,
    };

    expect(preferredGlobalExitNode([laptop, dc, office])?.id).toBe("10");
    expect(preferredGlobalExitNode([laptop])).toBeUndefined();
    expect(
      preferredGlobalExitNode([office, { ...dc, exitNodePriority: 20 }]),
      "a shared top priority names nobody",
    ).toBeUndefined();

    const screen = await render(
      app(
        <MetricTiles
          nodes={[laptop, dc, office]}
          users={[alice]}
          requests={undefined}
          nodesLoading={false}
          usersLoading={false}
        />,
      ),
    );

    await expect.element(screen.getByText("office first of 2 global")).toBeVisible();
  });

  it("leaves out the tiles the caller may not read", async () => {
    const screen = await render(
      app(
        <MetricTiles
          nodes={[laptop]}
          users={undefined}
          requests={undefined}
          nodesLoading={false}
          usersLoading={false}
        />,
      ),
    );

    await expect.element(screen.getByText("Machines")).toBeVisible();
    await expect.element(screen.getByText("Users")).not.toBeInTheDocument();
  });
});

describe(NeedsAttention, () => {
  it("stays one quiet row without a heading when nothing is waiting", async () => {
    const screen = await render(
      app(<NeedsAttention nodes={[laptop]} users={[alice]} requests={[]} groups={[]} me={admin} />),
    );

    await expect.element(screen.getByText("Nothing is waiting for a decision")).toBeVisible();
    await expect.element(screen.getByRole("link", { name: "Approval settings" })).toBeVisible();
    await expect.element(screen.getByText("Needs attention")).not.toBeInTheDocument();
    await expect.element(screen.getByRole("button", { name: "Approve" })).not.toBeInTheDocument();
  });

  it("lists what is waiting with the action it needs", async () => {
    const screen = await render(
      app(
        <NeedsAttention
          nodes={[laptop, gateway]}
          users={[alice, newcomer]}
          requests={[]}
          groups={[]}
          me={admin}
        />,
      ),
    );

    await expect.element(screen.getByRole("link", { name: "pi-gateway" })).toBeVisible();
    await expect.element(screen.getByRole("link", { name: "Bob" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Approve" }).elements()).toHaveLength(2);
  });

  it("offers no approval to a caller who cannot approve", async () => {
    const screen = await render(
      app(<NeedsAttention nodes={[gateway]} users={[]} requests={[]} groups={[]} me={reader} />),
    );

    await expect.element(screen.getByText("pi-gateway")).toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Approve" })).not.toBeInTheDocument();
  });

  it("keeps its heading and its warning while something waits", async () => {
    const screen = await render(
      app(
        <NeedsAttention nodes={[gateway]} users={[alice]} requests={[]} groups={[]} me={admin} />,
      ),
    );

    await expect.element(screen.getByText("Needs attention")).toBeVisible();
    await expect
      .element(
        screen.getByText(
          "Machines and users cannot reach the tailnet, and requesters cannot reach what they asked for, until these are decided",
        ),
      )
      .toBeVisible();
  });
});

describe(GetStarted, () => {
  it("fills the server URL into the command", async () => {
    const screen = await render(
      app(
        <GetStarted
          me={admin}
          onAddMachine={() => {
            // The dialog belongs to the page.
          }}
        />,
      ),
    );

    await expect
      .element(
        screen.getByText(
          `tailscale up --login-server=${globalThis.location.origin} --accept-routes --authkey=<key>`,
          {
            exact: false,
          },
        ),
      )
      .toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Create key" })).toBeEnabled();
  });

  it("offers the sign-in command without the scope", async () => {
    const screen = await render(
      app(
        <GetStarted
          me={reader}
          onAddMachine={() => {
            // The dialog belongs to the page.
          }}
        />,
      ),
    );

    await expect
      .element(
        screen.getByText(
          `tailscale up --login-server=${globalThis.location.origin} --accept-routes`,
          {
            exact: true,
          },
        ),
      )
      .toBeVisible();
    await expect
      .element(screen.getByRole("button", { name: "Create key" }))
      .not.toBeInTheDocument();
  });
});
