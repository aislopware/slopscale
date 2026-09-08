import { describe, expect, it } from "vitest";

import { describeUserAgent } from "~/components/settings/user-agent.ts";

describe(describeUserAgent, () => {
  it("names the browser and the system a sign-in came from", () => {
    expect(
      describeUserAgent(
        "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36",
      ),
    ).toBe("Chrome on macOS");
    expect(
      describeUserAgent(
        "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/150.0.0.0 Safari/537.36",
      ),
    ).toBe("Chrome on macOS");
    expect(
      describeUserAgent(
        "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:143.0) Gecko/20100101 Firefox/143.0",
      ),
    ).toBe("Firefox on Windows");
    expect(
      describeUserAgent(
        "Mozilla/5.0 (iPhone; CPU iPhone OS 18_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.4 Mobile/15E148 Safari/604.1",
      ),
    ).toBe("Safari on iOS");
  });

  it("prefers the browser that claims to be another over the one it claims", () => {
    expect(
      describeUserAgent(
        "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36 Edg/141.0.0.0",
      ),
    ).toBe("Edge on Windows");
    expect(
      describeUserAgent(
        "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36 OPR/121.0.0.0",
      ),
    ).toBe("Opera on Linux");
    expect(
      describeUserAgent(
        "Mozilla/5.0 (Linux; Android 15; Pixel 9) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Mobile Safari/537.36",
      ),
    ).toBe("Chrome on Android");
  });

  it("hands back a header it does not recognise, and says so when there is none", () => {
    expect(describeUserAgent("headscale-cli/0.28")).toBe("headscale-cli/0.28");
    expect(describeUserAgent("")).toBe("Unknown");
    expect(describeUserAgent("   ")).toBe("Unknown");
  });

  it("names a browser whose system it cannot place", () => {
    expect(describeUserAgent("Mozilla/5.0 Firefox/143.0")).toBe("Firefox");
  });
});
