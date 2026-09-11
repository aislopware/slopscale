import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import { Avatar, hueOf, initials, seededColours } from "~/components/ui/avatar.tsx";

describe(initials, () => {
  it("takes the first letter of the first two words, or two of the only one", () => {
    expect(initials("Alice Nguyen")).toBe("AN");
    expect(initials("jane.doe")).toBe("JD");
    expect(initials("bob")).toBe("BO");
  });
});

describe(hueOf, () => {
  // The colour is a property of the name, not of the render, so a person is the same colour in
  // every list and on every visit.
  it("is the same for the same name and spread out for different ones", () => {
    expect(hueOf("alice@example.com")).toBe(hueOf("alice@example.com"));

    const hues = ["alice", "bob", "carol", "dave", "erin"].map((name) => hueOf(name));

    expect(new Set(hues).size).toBe(hues.length);
    for (const hue of hues) {
      expect(hue).toBeGreaterThanOrEqual(0);
      expect(hue).toBeLessThan(360);
    }
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

    expect(getComputedStyle(alice).backgroundColor).not.toBe(getComputedStyle(bob).backgroundColor);
    expect(getComputedStyle(alice).backgroundColor).not.toBe("rgba(0, 0, 0, 0)");
    expect(alice.getAttribute("style")).toContain(seededColours("Alice Nguyen").backgroundColor);
  });
});
