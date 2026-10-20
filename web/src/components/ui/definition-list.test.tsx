import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import { CopyText } from "~/components/ui/copy-text.tsx";
import { DefinitionList } from "~/components/ui/definition-list.tsx";

function middle(element: Element): number {
  const box = element.getBoundingClientRect();

  return (box.top + box.bottom) / 2;
}

describe(DefinitionList, () => {
  // A copy button is a box of its own, smaller than the label's line; on the baseline it sat
  // three pixels lower than the label and read as off centre.
  it("centres a control in the value on the label", async () => {
    const view = await render(
      <div className="w-[40rem]">
        <DefinitionList
          items={[
            { label: "Identity provider", value: <CopyText value="http://127.0.0.1:9100/oidc" /> },
          ]}
        />
      </div>,
    );
    const label = view.getByText("Identity provider").element();
    const button = view.getByRole("button").element();

    expect(Math.abs(middle(label) - middle(button))).toBeLessThan(1);
    expect(button.scrollWidth).toBeLessThanOrEqual(button.clientWidth);
  });

  // Design spec §6 / §9: an identifier keeps a copy control in view, not only on hover.
  it("shows a copy button beside a copyable value without a hover, at the row's height", async () => {
    const view = await render(
      <div className="w-[40rem]">
        <DefinitionList
          items={[
            { label: "Version", value: "v1.2.3" },
            {
              label: "Commit",
              value: "9226d41e335f",
              copy: "9226d41e335fecab",
              copyLabel: "Copy commit",
            },
          ]}
        />
      </div>,
    );
    const button = view.getByRole("button", { name: "Copy commit" });
    const [plain, copyable] = view.container.querySelectorAll("dl > div");

    await expect.element(button).toBeVisible();
    expect(getComputedStyle(button.element()).opacity).toBe("1");
    expect(copyable?.getBoundingClientRect().height).toBe(plain?.getBoundingClientRect().height);
  });

  it("cuts a long node value with an ellipsis instead of pushing the label", async () => {
    const view = await render(
      <div className="w-64">
        <DefinitionList
          items={[{ label: "Path", value: <span>{"a-very-long-value-".repeat(10)}</span> }]}
        />
      </div>,
    );
    const value = view.getByText(/a-very-long-value/u).element();

    expect(getComputedStyle(value).textOverflow).toBe("ellipsis");
    expect(value.scrollWidth).toBeGreaterThan(value.clientWidth);
    expect(view.getByText("Path").element().getBoundingClientRect().left).toBeLessThan(100);
  });
});
