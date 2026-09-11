import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import { Avatar, initials } from "~/components/ui/avatar.tsx";
import { seededGradient } from "~/lib/hue.ts";

describe(initials, () => {
  it("takes the first letter of the first two words, or two of the only one", () => {
    expect(initials("Alice Nguyen")).toBe("AN");
    expect(initials("jane.doe")).toBe("JD");
    expect(initials("bob")).toBe("BO");
  });
});

describe(Avatar, () => {
  it("tints a person by their name and leaves an icon mark neutral", async () => {
    const view = await render(
      <>
        <Avatar name="Alice Nguyen" />
        <Avatar name="Bob Tran" />
      </>,
    );
    const alice = view.getByText("AN").element();
    const bob = view.getByText("BT").element();

    expect(getComputedStyle(alice).backgroundImage).not.toBe(getComputedStyle(bob).backgroundImage);
    expect(getComputedStyle(alice).backgroundImage).toContain("linear-gradient");
    expect(alice.getAttribute("style")).toContain(seededGradient("Alice Nguyen").backgroundImage);
  });
});
