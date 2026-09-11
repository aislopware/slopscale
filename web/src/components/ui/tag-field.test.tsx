import { useState } from "react";
import type { ReactElement } from "react";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";
import { userEvent } from "vitest/browser";

import { TagField, tagItems } from "~/components/ui/tag-field.tsx";

const known = ["tag:ci", "tag:prod", "tag:production", "tag:web"];

const none: readonly string[] = [];

function Field({ initial = none }: { readonly initial?: readonly string[] }): ReactElement {
  const [tags, setTags] = useState<readonly string[]>(initial);

  return (
    <>
      <TagField value={tags} suggestions={known} onValueChange={setTags} />
      <p>{`Values: ${tags.join(" ")}`}</p>
    </>
  );
}

describe(tagItems, () => {
  it("ranks an exact match first, then names that start with the text, then the rest", () => {
    expect(tagItems(known, [], "prod").map((item) => item.value)).toStrictEqual([
      "tag:prod",
      "tag:production",
    ]);
    // "o" is also a name nobody carries yet, so it is offered last.
    expect(tagItems(known, [], "o").map((item) => item.value)).toStrictEqual([
      "tag:prod",
      "tag:production",
      "tag:o",
    ]);
  });

  it("offers the typed tag as new only when nothing known is it, and never a chosen one", () => {
    expect(tagItems(known, [], "Staging")).toStrictEqual([{ value: "tag:staging", create: true }]);
    expect(tagItems(known, [], "tag:ci")).toStrictEqual([{ value: "tag:ci", create: false }]);
    expect(tagItems(known, ["tag:ci"], "ci")).toStrictEqual([]);
    expect(tagItems(known, [], "1bad")).toStrictEqual([]);
  });

  it("lists every open tag before anything is typed", () => {
    expect(tagItems(known, ["tag:web"], "").map((item) => item.value)).toStrictEqual([
      "tag:ci",
      "tag:prod",
      "tag:production",
    ]);
  });
});

describe(TagField, () => {
  it("shows the chosen tags as chips that can be removed", async () => {
    const screen = await render(<Field initial={["tag:ci", "tag:web"]} />);

    await expect.element(screen.getByText("Values: tag:ci tag:web")).toBeVisible();
    await screen.getByRole("button", { name: "Remove tag:ci" }).click();
    await expect.element(screen.getByText("Values: tag:web")).toBeVisible();
  });

  it("picks a known tag as the operator types and takes Enter for the first match", async () => {
    const screen = await render(<Field />);
    const input = screen.getByRole("combobox");

    await input.click();
    await userEvent.keyboard("prod");
    await expect.element(screen.getByRole("option", { name: /tag:prod$/u })).toBeVisible();
    await userEvent.keyboard("{Enter}");
    await expect.element(screen.getByText("Values: tag:prod")).toBeVisible();
    await expect.element(input).toHaveValue("");
  });

  it("takes a name the tailnet does not know yet, prefixed and folded", async () => {
    const screen = await render(<Field />);

    await screen.getByRole("combobox").click();
    await userEvent.keyboard("Staging");
    await expect.element(screen.getByText("New tag")).toBeVisible();
    await userEvent.keyboard("{Enter}");
    await expect.element(screen.getByText("Values: tag:staging")).toBeVisible();
  });

  it("says why a name is wrong instead of taking it", async () => {
    const screen = await render(<Field />);

    await screen.getByRole("combobox").click();
    await userEvent.keyboard("1bad");
    await expect.element(screen.getByText("A tag name starts with a letter.")).toBeVisible();
    await userEvent.keyboard("{Enter}");
    await expect.element(screen.getByText("Values: ")).toBeVisible();
  });

  it("lands a pasted list as chips at once", async () => {
    const screen = await render(<Field />);

    await screen.getByRole("combobox").fill("tag:ci, prod web");
    await expect.element(screen.getByText("Values: tag:ci tag:prod tag:web")).toBeVisible();
  });
});
