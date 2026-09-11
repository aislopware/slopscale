import { describe, expect, it } from "vitest";

import { osFamily, osLabel } from "~/lib/os.ts";

describe(osFamily, () => {
  it.each([
    ["macOS", "macos"],
    ["darwin", "macos"],
    ["iOS", "ios"],
    ["Windows", "windows"],
    ["linux", "linux"],
    ["android", "android"],
    ["freebsd", "freebsd"],
    ["illumos", "other"],
    ["", "other"],
  ])("reads %j as %s, whatever the case", (os, family) => {
    expect(osFamily(os)).toBe(family);
  });
});

describe(osLabel, () => {
  it("names the family and adds the version when there is one", () => {
    expect(osLabel("macOS", "15.1")).toBe("macOS 15.1");
    expect(osLabel("linux")).toBe("Linux");
    expect(osLabel("illumos", "5.11")).toBe("illumos 5.11");
    expect(osLabel("")).toBe("");
  });
});
