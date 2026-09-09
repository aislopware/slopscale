import { describe, expect, it, vi } from "vitest";

import { ApiError } from "~/api/error.ts";
import { onSessionEnd } from "~/auth/ended.ts";
import { dispositionFileName, downloadFile } from "~/lib/download.ts";

const statusConflict = 409;
const statusBadGateway = 502;
const statusGatewayTimeout = 504;
const statusUnauthorized = 401;

const url = "/api/v1/node/1/diagnostics/netmap";

/** Answers the next fetch with a problem document, the way the server refuses. */
function refuseWith(status: number, detail: string): void {
  const problem = Response.json(
    { status, title: "Refused", detail },
    { status, headers: { "content-type": "application/problem+json" } },
  );

  vi.stubGlobal(
    "fetch",
    vi.fn(() => Promise.resolve(problem)),
  );
}

/** The refusal the download raised, or a failure when it did not raise one. */
async function refusalOf(): Promise<ApiError> {
  try {
    await downloadFile(url, "dump.txt");
  } catch (error: unknown) {
    if (error instanceof ApiError) {
      return error;
    }

    throw error;
  }

  throw new Error("the download did not fail");
}

describe(dispositionFileName, () => {
  it("takes the quoted name the server asked for", () => {
    expect(dispositionFileName('attachment; filename="netmap.json"', "fallback.txt")).toBe(
      "netmap.json",
    );
  });

  it("takes an unquoted name too", () => {
    expect(dispositionFileName("attachment; filename=netmap.json", "fallback.txt")).toBe(
      "netmap.json",
    );
  });

  it("prefers the encoded name a server sends for browsers that read it", () => {
    expect(
      dispositionFileName(
        "attachment; filename=\"plain.txt\"; filename*=UTF-8''caf%C3%A9.txt",
        "fallback.txt",
      ),
    ).toBe("café.txt");
  });

  it("falls back to the console's own name when the server named none", () => {
    expect(dispositionFileName(null, "fallback.txt")).toBe("fallback.txt");
    expect(dispositionFileName("attachment", "fallback.txt")).toBe("fallback.txt");
  });
});

describe(downloadFile, () => {
  it.each([statusConflict, statusBadGateway, statusGatewayTimeout])(
    "raises what the server said about a %i",
    async (status) => {
      refuseWith(status, "the machine did not answer in time");

      const error = await refusalOf();

      expect(error.status).toBe(status);
      expect(error.message).toBe("the machine did not answer in time");
    },
  );

  it("falls back to the status when the refusal carries no problem document", async () => {
    const plain = new Response("nope", { status: statusBadGateway });

    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.resolve(plain)),
    );

    const error = await refusalOf();

    expect(error.message).toBe("The server refused the download (502).");
  });

  // A download outliving its session used to leave an expired page up behind a toast; it now ends
  // the session like any other request, so the router sends the operator back to sign-in.
  it("ends the session when the cookie stopped working", async () => {
    const ended = vi.fn<() => void>();
    const stop = onSessionEnd(ended);

    refuseWith(statusUnauthorized, "session expired");

    const error = await refusalOf();

    expect(error.status).toBe(statusUnauthorized);
    await Promise.resolve();

    expect(ended).toHaveBeenCalledOnce();
    stop();
  });
});
