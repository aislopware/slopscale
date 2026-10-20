import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import { Tag, TagList } from "~/components/ui/tag.tsx";

function tint(chip: Element | null | undefined): string {
  return chip instanceof Element ? getComputedStyle(chip).backgroundColor : "";
}

function styleOf(element: Element | null | undefined): CSSStyleDeclaration | undefined {
  return element instanceof Element ? getComputedStyle(element) : undefined;
}

function radiusOf(chip: Element | null): string {
  return chip instanceof Element ? getComputedStyle(chip).borderRadius : "";
}

describe(Tag, () => {
  // Design spec §7.1: the dashboard's chip draws its text in one colour, so the prefix is not faded.
  it("reads as the whole tag in one colour", async () => {
    const view = await render(<Tag tag="tag:server" />);
    const chip = view.container.querySelector('[title="tag:server"]');

    expect(chip).not.toBeNull();
    expect(chip?.textContent).toBe("tag:server");
    expect(chip?.querySelectorAll("*")).toHaveLength(1);
    expect(styleOf(chip?.firstElementChild)?.opacity).toBe("1");
    expect(Number(radiusOf(chip).replace("px", ""))).toBeGreaterThanOrEqual(9);
  });

  // The dashboard draws every tag as the same blue pill; the name tells two tags apart, not a hue.
  it("tints every tag the same info blue", async () => {
    const view = await render(
      <>
        <Tag tag="tag:web" />
        <Tag tag="tag:ci" />
        <Tag tag="tag:web" />
      </>,
    );
    const [web, ci, webAgain] = view.container.querySelectorAll("span[title]");

    expect(tint(web)).toBe(tint(webAgain));
    expect(tint(web)).toBe(tint(ci));
    expect(tint(web)).not.toBe("rgba(0, 0, 0, 0)");
  });
});

describe("a tag chip's size", () => {
  // Design spec §7.1: the chips are 20px with 13px text, a dense table's included.
  it.each(["sm", "base"] as const)("draws a %s chip 20px tall at 13px", async (size) => {
    const view = await render(<Tag tag="tag:server" size={size} />);
    const chip = view.container.querySelector('[title="tag:server"]');

    expect(chip?.getBoundingClientRect().height).toBe(20);
    expect(styleOf(chip)?.fontSize).toBe("13px");
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
