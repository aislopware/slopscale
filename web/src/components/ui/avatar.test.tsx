import { describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-react";

import { Avatar, initials } from "~/components/ui/avatar.tsx";
import { baseHue, hueColours } from "~/lib/hue.ts";

/** A one-pixel PNG, so the picture loads without a server. */
const pixel =
  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=";

describe(initials, () => {
  it("takes the first letter of the first two words, or two of the only one", () => {
    expect(initials("Alice Nguyen")).toBe("AN");
    expect(initials("jane.doe")).toBe("JD");
    expect(initials("bob")).toBe("BO");
  });
});

describe(Avatar, () => {
  it("tints a person by their id and name", async () => {
    const view = await render(
      <>
        <Avatar name="Alice Nguyen" id="1" />
        <Avatar name="Bob Tran" id="2" />
      </>,
    );
    const alice = view.getByText("AN").element();
    const bob = view.getByText("BT").element();

    expect(getComputedStyle(alice).backgroundColor).not.toBe(getComputedStyle(bob).backgroundColor);
    expect(alice.getAttribute("style")).toContain(
      hueColours(baseHue("Alice Nguyen", "1")).backgroundColor,
    );
  });

  it("shows the picture over the initials once it has loaded", async () => {
    const view = await render(<Avatar name="Alice Nguyen" id="1" src={pixel} />);

    await expect.element(view.getByText("AN")).toBeInTheDocument();
    await vi.waitFor(() => {
      expect(view.container.querySelector("img")?.dataset["loaded"]).toBe("true");
    });
  });

  it("keeps the initials when the picture never loads", async () => {
    const view = await render(
      <Avatar name="Alice Nguyen" id="1" src="https://127.0.0.1:1/nobody.png" />,
    );

    await vi.waitFor(() => {
      expect(view.container.querySelector("img")).toBeNull();
    });
    await expect.element(view.getByText("AN")).toBeInTheDocument();
  });
});
