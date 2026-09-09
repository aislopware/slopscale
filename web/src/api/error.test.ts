import { describe, expect, it } from "vitest";

import { ApiError, errorMessage } from "~/api/error.ts";

describe(ApiError, () => {
  it("prefers the problem detail and lists field errors", () => {
    const error = new ApiError(
      422,
      {
        type: "about:blank",
        title: "Unprocessable Entity",
        detail: "validation failed",
        errors: [{ message: "expected string" }, { message: "" }, { location: "body.x" }],
      },
      "422 Unprocessable Entity",
    );

    expect(error.message).toBe("validation failed. expected string");
    expect(error.status).toBe(422);
    expect(error.unauthorized).toBe(false);
  });

  it("falls back to the status line without a problem body", () => {
    const error = new ApiError(401, undefined, "401 Unauthorized");

    expect(error.message).toBe("401 Unauthorized");
    expect(error.unauthorized).toBe(true);
  });
});

describe(errorMessage, () => {
  it("reads any thrown value", () => {
    expect(errorMessage(new Error("boom"))).toBe("boom");
    expect(errorMessage("text")).toBe("text");
    expect(errorMessage(42)).toBe("The request failed.");
  });
});
