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
    expect(token?.querySelector(".text-kumo-subtle")?.textContent).toBe("*.");
    expect(tint(token)).not.toBe("rgba(0, 0, 0, 0)");
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
