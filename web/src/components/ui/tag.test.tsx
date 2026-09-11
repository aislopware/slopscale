import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import { Tag, TagList } from "~/components/ui/tag.tsx";

function tint(chip: Element | null | undefined): string {
  return chip instanceof Element ? getComputedStyle(chip).backgroundColor : "";
}

function radiusOf(chip: Element | null): string {
  return chip instanceof Element ? getComputedStyle(chip).borderRadius : "";
}

describe(Tag, () => {
  it("reads as the whole tag, with the prefix stepped back from the name", async () => {
    const view = await render(<Tag tag="tag:server" />);
    const chip = view.container.querySelector('[title="tag:server"]');

    expect(chip).not.toBeNull();
    expect(chip?.textContent).toBe("tag:server");
    expect(chip?.querySelector(".opacity-60")?.textContent).toBe("tag:");
    expect(radiusOf(chip)).not.toBe("9999px");
  });

  // The colour is a property of the name, so the same tag is the same colour on every page and two
  // tags tell apart before they are read.
  it("tints each tag by its name, the same name the same way", async () => {
    const view = await render(
      <>
        <Tag tag="tag:web" />
        <Tag tag="tag:ci" />
        <Tag tag="tag:web" />
      </>,
    );
    const [web, ci, webAgain] = view.container.querySelectorAll("span[title]");

    expect(tint(web)).toBe(tint(webAgain));
    expect(tint(web)).not.toBe(tint(ci));
    expect(tint(web)).not.toBe("rgba(0, 0, 0, 0)");
  });
});

describe(TagList, () => {
  it("shows up to the limit and counts the rest, named on hover", async () => {
    const view = await render(<TagList tags={["tag:a", "tag:b", "tag:c"]} max={2} />);

    await expect.element(view.getByText("tag:a")).toBeVisible();
    await expect.element(view.getByText("tag:b")).toBeVisible();
    expect(view.container.querySelector('[title="tag:c"]')).toBeNull();
    await expect.element(view.getByText("+1 more")).toBeVisible();
  });

  it("says when there is nothing", async () => {
    const view = await render(<TagList tags={[]} empty="No tags" />);

    await expect.element(view.getByText("No tags")).toBeVisible();
  });
});
