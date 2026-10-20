import { Switch } from "@cloudflare/kumo/components/switch";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

/** A palette colour as the browser resolves it, to compare a computed fill with. */
function paletteColour(name: string): string {
  const probe = document.createElement("div");
  probe.style.backgroundColor = `var(--color-${name})`;
  document.body.append(probe);
  const colour = getComputedStyle(probe).backgroundColor;
  probe.remove();

  return colour;
}

function setMode(mode: "light" | "dark"): void {
  document.documentElement.dataset["mode"] = mode;
}

async function switches(): Promise<{ off: Element; on: Element }> {
  const view = await render(
    <>
      <Switch label="Off" checked={false} />
      <Switch label="On" checked />
    </>,
  );

  return {
    off: view.getByRole("switch", { name: "Off" }).element(),
    on: view.getByRole("switch", { name: "On" }).element(),
  };
}

function fill(element: Element | null): string {
  if (element === null) {
    throw new Error("missing element");
  }

  return getComputedStyle(element).backgroundColor;
}

// Kumo's Switch is the one component that colours itself with Tailwind's `dark:` utilities. The
// console switches modes with `data-mode`, not the system preference, so the variant has to follow
// the attribute or a dark console draws white tracks and near-black thumbs (design spec §9).
describe("dark mode", () => {
  it("draws a dark switch with the dark track and thumb", async () => {
    setMode("dark");
    const { off, on } = await switches();

    expect(fill(off)).toBe(paletteColour("neutral-700"));
    expect(fill(on)).toBe(paletteColour("blue-600"));
    expect(fill(on.firstElementChild)).toBe(paletteColour("blue-300"));
    setMode("light");
  });

  it("draws a light switch with the light track", async () => {
    setMode("light");
    const { off, on } = await switches();

    expect(fill(off)).toBe(paletteColour("neutral-200"));
    expect(fill(on)).toBe(paletteColour("blue-500"));
    expect(fill(on.firstElementChild)).not.toBe(paletteColour("blue-300"));
  });
});
