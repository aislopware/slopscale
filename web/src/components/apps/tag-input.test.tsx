import { useState } from "react";
import type { ReactElement } from "react";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";
import { userEvent } from "vitest/browser";

import { connectorTagError, normalizeConnectorTag } from "~/components/apps/model.ts";
import { TagInput, usePendingLists } from "~/components/apps/tag-input.tsx";

const noChips: readonly string[] = [];

/** The chip list as a form uses it: the value it holds and whether an entry is still uncommitted. */
function ConnectorsField({
  initial = noChips,
}: {
  readonly initial?: readonly string[];
}): ReactElement {
  const [connectors, setConnectors] = useState<readonly string[]>(initial);
  const lists = usePendingLists();

  return (
    <>
      <TagInput
        label="Connectors"
        description="The tags of the machines that serve the app."
        placeholder="tag:connector"
        value={connectors}
        onPendingChange={lists.track("connectors")}
        normalize={normalizeConnectorTag}
        validate={connectorTagError}
        onValueChange={setConnectors}
      />
      <p>{lists.pending ? "Entry waiting" : "Nothing waiting"}</p>
    </>
  );
}

describe(TagInput, () => {
  it("labels its input with the field's label", async () => {
    const screen = await render(<ConnectorsField initial={["tag:connector"]} />);
    const input = screen.getByRole("textbox", { name: "Connectors" });

    // The label names the control even once a chip has taken the placeholder away.
    await expect.element(input).toBeVisible();
    await expect
      .element(input)
      .toHaveAccessibleDescription("The tags of the machines that serve the app.");
  });

  it("turns an entry into a chip and takes it back", async () => {
    const screen = await render(<ConnectorsField />);

    await screen.getByRole("textbox", { name: "Connectors" }).fill("app");
    await userEvent.keyboard("{Enter}");

    await expect.element(screen.getByText("tag:app")).toBeVisible();
    await expect.element(screen.getByText("Nothing waiting")).toBeVisible();

    await screen.getByRole("button", { name: "Remove tag:app" }).click();
    await expect.element(screen.getByText("tag:app")).not.toBeInTheDocument();
  });

  it("says an entry is waiting until it is committed or refused", async () => {
    const screen = await render(<ConnectorsField />);

    await screen.getByRole("textbox", { name: "Connectors" }).fill("tag:app");
    await expect.element(screen.getByText("Entry waiting")).toBeVisible();

    await userEvent.keyboard("{Enter}");
    await expect.element(screen.getByText("Nothing waiting")).toBeVisible();
  });

  it("marks the input invalid and says why an entry was refused", async () => {
    const screen = await render(<ConnectorsField />);
    const input = screen.getByRole("textbox", { name: "Connectors" });

    await input.fill("tag:");
    await userEvent.keyboard("{Enter}");

    await expect.element(input).toHaveAttribute("aria-invalid", "true");
    await expect.element(screen.getByText("Entry waiting")).toBeVisible();
  });
});
