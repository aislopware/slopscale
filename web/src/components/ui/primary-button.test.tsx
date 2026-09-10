import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

/**
 * App.css turns Kumo's blue primary button into ink by redefining `--color-kumo-brand` on the
 * button and picks the primary variant out by the inline emphasis style Kumo writes from that
 * token. Both are assumptions about Kumo's markup, so they are pinned here: a Kumo upgrade that
 * stops writing the token into the style attribute would silently bring the blue back. The label
 * rules are painted rather than declared, so these read the colour the browser settled on.
 */

/** The colour an expression resolves to in this element's own context, in the browser's own format. */
function resolve(element: Element, expression: string): string {
  const probe = document.createElement("span");

  probe.style.color = expression;
  element.append(probe);
  const value = globalThis.getComputedStyle(probe).color;
  probe.remove();

  return value;
}

function setMode(mode: "light" | "dark"): void {
  document.documentElement.dataset["mode"] = mode;
}

describe("primary button", () => {
  it("mixes its emphasis colour from the brand token on the element", async () => {
    const screen = await render(<Button variant="primary">Save</Button>);
    const button = screen.getByRole("button", { name: "Save" }).element();

    expect(button.getAttribute("style")).toContain("var(--color-kumo-brand)");
    expect(button.dataset["kumoComponent"]).toBe("Button");
  });

  it("mixes a destructive button from the danger token, so it keeps its colour", async () => {
    const screen = await render(<Button variant="destructive">Delete</Button>);
    const button = screen.getByRole("button", { name: "Delete" }).element();

    expect(button.getAttribute("style")).toContain("var(--color-kumo-danger)");
    expect(button.getAttribute("style")).not.toContain("var(--color-kumo-brand)");
  });

  it("is ink, not blue", async () => {
    const screen = await render(<Button variant="primary">Save</Button>);
    const button = screen.getByRole("button", { name: "Save" }).element();
    const root = globalThis.getComputedStyle(document.documentElement);

    expect(globalThis.getComputedStyle(button).getPropertyValue("--color-kumo-brand").trim()).toBe(
      root.getPropertyValue("--color-kumo-contrast").trim(),
    );
  });

  it("keeps a light-mode label white, where the ink behind it is near-black", async () => {
    setMode("light");
    const screen = await render(<Button variant="primary">Save</Button>);
    const button = screen.getByRole("button", { name: "Save" }).element();

    expect(globalThis.getComputedStyle(button).color).toBe(resolve(button, "#fff"));
  });

  it("turns a dark-mode label to canvas, because the ink behind it is near-white", async () => {
    setMode("dark");
    const screen = await render(<Button variant="primary">Save</Button>);
    const button = screen.getByRole("button", { name: "Save" }).element();

    expect(globalThis.getComputedStyle(button).color).toBe(
      resolve(button, "var(--color-kumo-canvas)"),
    );
    expect(globalThis.getComputedStyle(button).color).not.toBe(resolve(button, "#fff"));
    setMode("light");
  });

  it("turns a dark-mode primary link's label too, since Kumo paints it the same way", async () => {
    setMode("dark");
    const screen = await render(
      <LinkButton variant="primary" href="/">
        Save
      </LinkButton>,
    );
    const link = screen.getByRole("link", { name: "Save" }).element();

    expect(link.getAttribute("style")).toContain("var(--color-kumo-brand)");
    expect(globalThis.getComputedStyle(link).color).toBe(resolve(link, "var(--color-kumo-canvas)"));
    setMode("light");
  });

  it("drains the colour out of a disabled primary button rather than fading it", async () => {
    const screen = await render(
      <Button variant="primary" disabled>
        Save
      </Button>,
    );
    const button = screen.getByRole("button", { name: "Save" }).element();

    expect(globalThis.getComputedStyle(button).filter).toBe("grayscale(1)");
  });
});
