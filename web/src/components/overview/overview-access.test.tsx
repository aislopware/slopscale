import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import type { Me } from "~/auth/me.ts";
import { MetricTiles } from "~/components/overview/metric-tiles.tsx";
import { NeedsAttention } from "~/components/overview/needs-attention.tsx";
import {
  alice,
  admin,
  app,
  approver,
  laptop,
  ops,
  waitingRequest,
} from "~/components/overview/overview-fixtures.tsx";

describe("access requests on the overview", () => {
  it("lists a waiting request beside the machines and users", async () => {
    const screen = await render(
      app(
        <NeedsAttention
          nodes={[laptop]}
          users={[alice]}
          requests={[waitingRequest]}
          groups={[ops]}
          me={approver}
        />,
      ),
    );

    await expect.element(screen.getByRole("link", { name: "Access to Ops" })).toBeVisible();
    // An approver decides on what the row says, so it says all of it: who asked, for how long,
    // which machines, and why.
    await expect
      .element(screen.getByText(/Access request · .* · 1 h · every machine they own · on call/u))
      .toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Approve" })).toBeVisible();
  });

  it("offers no approval on the requester's own request", async () => {
    // The server refuses a self-decision, so the row does not hold out a button that would fail.
    const self: Me = { ...approver, user: { ...alice, id: waitingRequest.userId } };
    const screen = await render(
      app(
        <NeedsAttention
          nodes={[]}
          users={[alice]}
          requests={[waitingRequest]}
          groups={[ops]}
          me={self}
        />,
      ),
    );

    await expect.element(screen.getByRole("link", { name: "Access to Ops" })).toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Approve" })).not.toBeInTheDocument();
  });

  it("offers no approval for a request to a caller without the policy scope", async () => {
    const screen = await render(
      app(
        <NeedsAttention
          nodes={[laptop]}
          users={[alice]}
          requests={[waitingRequest]}
          groups={[ops]}
          me={admin}
        />,
      ),
    );

    await expect.element(screen.getByText("Access to Ops")).toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Approve" })).not.toBeInTheDocument();
  });

  it("counts the grants in effect and what waits on the tile", async () => {
    const screen = await render(
      app(
        <MetricTiles
          nodes={[laptop]}
          users={[alice]}
          requests={[waitingRequest]}
          nodesLoading={false}
          usersLoading={false}
        />,
      ),
    );

    await expect.element(screen.getByRole("link", { name: /Temporary access/u })).toBeVisible();
    await expect.element(screen.getByText("1 request waiting")).toBeVisible();
  });
});
