import { Button } from "@cloudflare/kumo/components/button";
import { Checkbox } from "@cloudflare/kumo/components/checkbox";
import { Switch } from "@cloudflare/kumo/components/switch";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

/**
 * App.css sets the Cloudflare dashboard's type and otherwise leaves Kumo's look alone: the blue
 * gradient primary, the blue squircle switch and the near-black checkbox are Kumo's own. These pin
 * both halves, so a stray override that brings back an ink accent, or a Kumo upgrade that changes
 * the token a control paints from, shows up here rather than on a page.
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

describe("app.css", () => {
  it("sets Inter with the dashboard's alternates and tracking", () => {
    const body = globalThis.getComputedStyle(document.body);

    expect(body.fontFamily).toContain("Inter Variable");
    expect(body.fontFeatureSettings).toContain('"cv02"');
    expect(body.fontFeatureSettings).toContain('"cv04"');
    expect(body.letterSpacing).toBe("-0.16px");
  });

  it("sets Paper Mono for monospaced text", async () => {
    const screen = await render(<code className="font-mono">100.64.0.1</code>);
    const code = screen.getByText("100.64.0.1").element();

    expect(globalThis.getComputedStyle(code).fontFamily).toContain("Paper Mono");
  });

  it("keeps the brand token Kumo's blue, not the contrast colour", async () => {
    const screen = await render(<Button variant="primary">Save</Button>);
    const button = screen.getByRole("button", { name: "Save" }).element();
    const style = globalThis.getComputedStyle(button);

    expect(button.getAttribute("style")).toContain("var(--color-kumo-brand)");
    expect(resolve(button, "var(--color-kumo-brand)")).not.toBe(
      resolve(button, "var(--color-kumo-contrast)"),
    );
    expect(style.color).toBe(resolve(button, "#fff"));
  });

  it("fades a disabled primary instead of draining its colour", async () => {
    const screen = await render(
      <Button variant="primary" disabled>
        Save
      </Button>,
    );
    const button = screen.getByRole("button", { name: "Save" }).element();
    const style = globalThis.getComputedStyle(button);

    expect(style.filter).toBe("none");
    expect(style.opacity).toBe("0.5");
  });

  it("paints a checked switch from Kumo's blue, not ink", async () => {
    const screen = await render(<Switch checked label="Enabled" />);
    const track = screen.getByRole("switch").element();
    const background = globalThis.getComputedStyle(track).backgroundColor;

    expect(background).not.toBe(resolve(track, "var(--color-kumo-contrast)"));
    expect(background).not.toBe(resolve(track, "var(--color-kumo-canvas)"));
  });

  it("fills a checked checkbox near-black", async () => {
    const screen = await render(<Checkbox checked label="Select" />);
    const box = screen.getByRole("checkbox").element();

    expect(globalThis.getComputedStyle(box).backgroundColor).toBe(
      resolve(box, "var(--color-kumo-contrast)"),
    );
  });
});
