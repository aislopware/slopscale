import { EditorView } from "@codemirror/view";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import type { ReactElement } from "react";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import type { AccessRule, Group } from "~/api/queries.ts";
import { RuleEditor } from "~/components/access/rule-editor.tsx";

const stamp = "2026-01-01T12:00:00Z";

function group(id: string, name: string, builtin = ""): Group {
  return {
    autoApprove: false,
    builtin,
    source: "",
    createdAt: stamp,
    description: "",
    expiries: [],
    id,
    maxRequestSeconds: 0,
    name,
    nodeIds: [],
    requestable: false,
    updatedAt: stamp,
    userIds: [],
  };
}

const groups = [
  group("1", "All machines", "all"),
  group("3", "Engineering"),
  group("4", "Production"),
  group("5", "Staging"),
];

const rule: AccessRule = {
  bidirectional: false,
  builtin: "",
  createdAt: stamp,
  description: "",
  destinationGroupIds: ["4"],
  enabled: true,
  expiresAt: null,
  id: "9",
  name: "Engineers to production",
  ports: "22",
  postureIds: [],
  protocol: "tcp",
  sourceGroupIds: ["3"],
  updatedAt: stamp,
};

function app(): ReactElement {
  const rootRoute = createRootRoute({
    component: () => (
      <RuleEditor
        rule={rule}
        rules={[rule]}
        groups={groups}
        postures={[]}
        policyFileEnforces={false}
        canEdit
      />
    ),
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({ getParentRoute: () => rootRoute, path: "/" }),
      createRoute({ getParentRoute: () => rootRoute, path: "/policy/rules" }),
      createRoute({ getParentRoute: () => rootRoute, path: "/policy/postures/new" }),
    ]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  return (
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  );
}

/** Replaces the snippet the way typing does: a change through the editor, not the prop. */
function typeSnippet(text: string): void {
  const content = document.querySelector<HTMLElement>("[aria-label='Rule as HuJSON']");
  const view = content === null ? null : EditorView.findFromDOM(content);

  if (view === null) {
    throw new Error("the HuJSON editor is not on the page");
  }

  view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: text } });
}

function snippetText(): string {
  return document.querySelector("[aria-label='Rule as HuJSON']")?.textContent ?? "";
}

describe(RuleEditor, () => {
  it("shows the rule as its grant and reads an edit back into the visual rows", async () => {
    const screen = await render(app());

    await screen.getByRole("tab", { name: "HuJSON" }).click();
    await expect.poll(snippetText).toContain('"src": ["accessgroup:Engineering"],');
    expect(snippetText()).toContain('"ip": ["tcp:22"],');

    typeSnippet(
      '{ "src": ["accessgroup:Engineering"], "dst": ["accessgroup:Staging"], "ip": ["udp:53"] }',
    );
    await screen.getByRole("tab", { name: "Visual" }).click();

    await expect
      .element(screen.getByRole("tab", { name: "Visual" }))
      .toHaveAttribute("aria-selected", "true");
    await expect.element(screen.getByLabelText(/Port/v)).toHaveValue("53");
    await expect.element(screen.getByText("Staging", { exact: true }).first()).toBeVisible();
  });

  it("stays in HuJSON and says what is wrong while the snippet does not parse", async () => {
    const screen = await render(app());

    await screen.getByRole("tab", { name: "HuJSON" }).click();
    await expect.poll(snippetText).toContain("accessgroup:Engineering");
    typeSnippet('{ "src": ["accessgroup:Nobody"], "dst": [], "ip": ["*"] }');
    await screen.getByRole("tab", { name: "Visual" }).click();

    await expect
      .element(screen.getByRole("alert"))
      .toHaveTextContent('Line 1: No group is named "Nobody"');
    await expect
      .element(screen.getByRole("tab", { name: "HuJSON" }))
      .toHaveAttribute("aria-selected", "true");
    expect(document.querySelector("[aria-label='Rule as HuJSON']")).not.toBeNull();
  });
});
