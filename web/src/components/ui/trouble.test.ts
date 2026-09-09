import { describe, expect, it } from "vitest";

import { ApiError } from "~/api/error.ts";
import { describeTrouble, sentence } from "~/components/ui/trouble.ts";

describe(describeTrouble, () => {
  it("reads a gateway status as the server being unreachable", () => {
    const trouble = describeTrouble(new ApiError(502, undefined, "502 Bad Gateway"));

    expect(trouble).toMatchObject({
      kind: "unreachable",
      eyebrow: "HTTP 502",
      title: "The server did not answer",
      message: "502 Bad Gateway",
    });
  });

  it("reads a failed fetch the same way", () => {
    expect(describeTrouble(new TypeError("Failed to fetch"))).toMatchObject({
      kind: "unreachable",
      eyebrow: "No connection",
    });
  });

  it("uses the server's detail for access and missing things", () => {
    const forbidden = new ApiError(
      403,
      { type: "about:blank", detail: "Only an owner can do that." },
      "403",
    );
    const missing = new ApiError(
      404,
      { type: "about:blank", detail: "No machine with id 9.", instance: "/x" },
      "404",
    );

    expect(describeTrouble(forbidden)).toMatchObject({
      kind: "forbidden",
      title: "You do not have access",
      description: "Only an owner can do that.",
    });
    expect(describeTrouble(missing)).toMatchObject({
      kind: "missing",
      description: "No machine with id 9.",
      instance: "/x",
    });
  });

  it("writes the server's detail as a sentence", () => {
    expect(sentence("node not found")).toBe("Node not found.");
    expect(sentence("Already done!")).toBe("Already done!");
    expect(
      describeTrouble(new ApiError(404, { type: "about:blank", detail: "node not found" }, "404"))
        .description,
    ).toBe("Node not found.");
  });

  it("tells a session that ended from a console fault", () => {
    expect(describeTrouble(new ApiError(401, undefined, "401")).kind).toBe("session");
    expect(describeTrouble(new Error("x is not a function"))).toMatchObject({
      kind: "console",
      eyebrow: "Console error",
      message: "x is not a function",
    });
  });
});
