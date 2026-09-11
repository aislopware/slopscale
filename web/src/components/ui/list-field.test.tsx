import { useState } from "react";
import type { ReactElement } from "react";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";
import { userEvent } from "vitest/browser";

import { connectorTagError, normalizeConnectorTag } from "~/components/apps/model.ts";
import { ListField } from "~/components/ui/list-field.tsx";
import { listError, listValues } from "~/lib/list.ts";

const noRows: readonly string[] = [];

/** The list as a form uses it: the rows it holds, what they are worth and whether one is wrong. */
function ConnectorsField({
  initial = noRows,
}: {
  readonly initial?: readonly string[];
}): ReactElement {
  const [rows, setRows] = useState<readonly string[]>(initial);
  const values = listValues(rows, normalizeConnectorTag);

  return (
    <>
      <ListField
        label="Connectors"
        description="The tags of the machines that serve the app."
        placeholder="tag:connector"
        addLabel="Add connector"
        value={rows}
        normalize={normalizeConnectorTag}
        validate={connectorTagError}
        onValueChange={setRows}
      />
      <p>{`Values: ${values.join(" ")}`}</p>
      <p>{listError(rows, connectorTagError) === null ? "All good" : "Something wrong"}</p>
    </>
  );
}

describe(ListField, () => {
  it("names the group and each row after the label", async () => {
    const screen = await render(<ConnectorsField initial={["tag:connector"]} />);

    await expect.element(screen.getByRole("group", { name: "Connectors" })).toBeVisible();
    await expect
      .element(screen.getByRole("textbox", { name: "Connectors 1" }))
      .toHaveValue("tag:connector");
    await expect
      .element(screen.getByText("The tags of the machines that serve the app."))
      .toBeVisible();
  });

  it("adds a row with the button, tidies it on leaving and removes it with its button", async () => {
    const screen = await render(<ConnectorsField />);

    await screen.getByRole("button", { name: "Add connector" }).click();

    const row = screen.getByRole("textbox", { name: "Connectors 1" });

    await expect.element(row).toHaveValue("");
    await row.fill("app");
    await expect.element(screen.getByText("Values: tag:app")).toBeVisible();

    await userEvent.tab();
    await expect.element(row).toHaveValue("tag:app");

    await screen.getByRole("button", { name: "Remove tag:app" }).click();
    await expect.element(row).not.toBeInTheDocument();
    await expect.element(screen.getByText("Values:")).toBeVisible();
  });

  it("adds the next row on Enter in a valid row, not for an empty one", async () => {
    const screen = await render(<ConnectorsField initial={["tag:app"]} />);

    await screen.getByRole("textbox", { name: "Connectors 1" }).click();
    await userEvent.keyboard("{Enter}");

    const next = screen.getByRole("textbox", { name: "Connectors 2" });

    await expect.element(next).toBeVisible();
    await expect.element(next).toHaveFocus();

    await userEvent.keyboard("{Enter}");
    await expect
      .element(screen.getByRole("textbox", { name: "Connectors 3" }))
      .not.toBeInTheDocument();
  });

  it("says why a row is wrong once the operator leaves it", async () => {
    const screen = await render(<ConnectorsField initial={["tag:"]} />);
    const row = screen.getByRole("textbox", { name: "Connectors 1" });

    await expect.element(screen.getByText("Something wrong")).toBeVisible();
    await expect.element(row).not.toHaveAttribute("aria-invalid");

    await row.click();
    await userEvent.tab();

    await expect.element(row).toHaveAttribute("aria-invalid", "true");
    await expect.element(screen.getByText("Enter a valid tag name (e.g. tag:app).")).toBeVisible();
  });

  it("takes an empty row away on Backspace", async () => {
    const screen = await render(<ConnectorsField initial={["tag:app", ""]} />);

    await screen.getByRole("textbox", { name: "Connectors 2" }).click();
    await userEvent.keyboard("{Backspace}");

    await expect
      .element(screen.getByRole("textbox", { name: "Connectors 2" }))
      .not.toBeInTheDocument();
    await expect.element(screen.getByRole("textbox", { name: "Connectors 1" })).toHaveFocus();
  });
});
