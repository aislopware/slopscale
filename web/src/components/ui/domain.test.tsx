import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import { Domain, DomainList, splitDomain } from "~/components/ui/domain.tsx";

function tint(token: Element | null): string {
  return token instanceof Element ? getComputedStyle(token).backgroundColor : "";
}

describe(Domain, () => {
  it("reads as the whole domain, with a wildcard stepped back from the name", async () => {
    const view = await render(<Domain domain="*.example.com" />);
    const token = view.container.querySelector('[title="*.example.com"]');

    expect(token).not.toBeNull();
    expect(token?.textContent).toBe("*.example.com");
    expect(token?.querySelector(".opacity-60")?.textContent).toBe("*.");
    expect(token?.querySelector(".rounded-full")).toBeNull();
  });

  it("tints the token by the registered site, so hosts under one site match", async () => {
    const view = await render(
      <>
        <Domain domain="a.example.com" />
        <Domain domain="b.example.com" />
        <Domain domain="c.example.net" />
      </>,
    );
    const shades = ["a.example.com", "b.example.com", "c.example.net"].map((domain) =>
      tint(view.container.querySelector(`[title="${domain}"]`)),
    );

    expect(shades[0]).not.toBe("rgba(0, 0, 0, 0)");
    expect(shades[0]).toBe(shades[1]);
    expect(shades[0]).not.toBe(shades[2]);
  });
});

describe(splitDomain, () => {
  it("finds the registered site under a plain or a country registry suffix", () => {
    expect(splitDomain("kibana-prod.jmango360.dev")).toStrictEqual({
      sub: "kibana-prod.",
      site: "jmango360.dev",
    });
    expect(splitDomain("ifconfig.me")).toStrictEqual({ sub: "", site: "ifconfig.me" });
    expect(splitDomain("www.bbc.co.uk")).toStrictEqual({ sub: "www.", site: "bbc.co.uk" });
    expect(splitDomain("a.b.example.com")).toStrictEqual({ sub: "a.b.", site: "example.com" });
  });
});

describe(DomainList, () => {
  it("shows up to the limit and counts the rest", async () => {
    const view = await render(
      <DomainList domains={["a.example.com", "b.example.com", "c.example.com"]} max={2} />,
    );

    await expect.element(view.getByText("a.example.com")).toBeVisible();
    expect(view.container.querySelector('[title="c.example.com"]')).toBeNull();
    await expect.element(view.getByText("+1 more")).toBeVisible();
  });
});
