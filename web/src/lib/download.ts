import { safeParse } from "valibot";

import { ApiError, problemSchema } from "~/api/error.ts";
import type { Problem } from "~/api/error.ts";

/** How long the blob URL outlives the click that started the download. */
const revokeDelayMs = 1000;

/**
 * Saves a body the console already fetched to disk. The download goes through a blob URL rather
 * than a plain link so a refused or failed request surfaces as an error the page can show, instead
 * of the browser navigating away to a problem document.
 */
export function saveBlob(blob: Blob, fileName: string): void {
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");

  link.href = url;
  link.download = fileName;
  document.body.append(link);
  link.click();
  link.remove();

  // The browser reads the blob after the click returns, so the URL is released on the next turn
  // rather than straight away, which cancels the download it just started.
  setTimeout(() => {
    URL.revokeObjectURL(url);
  }, revokeDelayMs);
}

/**
 * The name the server asked the file to be saved as, or `fallback` when it named none. RFC 6266
 * allows both a plain `filename` and a percent-encoded `filename*`; the encoded one wins, since a
 * server that sends both sends the plain one for browsers that cannot read the other.
 */
export function dispositionFileName(disposition: string | null, fallback: string): string {
  if (disposition === null) {
    return fallback;
  }

  const encoded = /filename\*=(?:[^']*'[^']*')?(?<name>[^;]+)/u.exec(disposition)?.groups?.["name"];

  if (encoded !== undefined) {
    return decodeName(encoded.trim(), fallback);
  }

  const plain = /filename="(?<quoted>[^"]*)"|filename=(?<bare>[^;]+)/u.exec(disposition)?.groups;
  const name = plain?.["quoted"] ?? plain?.["bare"]?.trim() ?? "";

  return name === "" ? fallback : name;
}

/** A `filename*` value is percent-encoded; a malformed one is not worth failing the download over. */
function decodeName(value: string, fallback: string): string {
  try {
    return decodeURIComponent(value) || fallback;
  } catch {
    return value;
  }
}

/**
 * Fetches a file with the console's own credentials (the session cookie rides along on a
 * same-origin request) and saves it under the name the server asked for. A refusal is raised as an
 * {@link ApiError} carrying the problem document, so the caller shows what the server said rather
 * than a status code.
 */
export async function downloadFile(url: string, fallbackName: string): Promise<void> {
  const response = await fetch(url);

  if (!response.ok) {
    throw new ApiError(
      response.status,
      await readProblem(response),
      `The server refused the download (${String(response.status)}).`,
    );
  }

  const name = dispositionFileName(response.headers.get("content-disposition"), fallbackName);

  saveBlob(await response.blob(), name);
}

/** The RFC 9457 body of a refusal, or undefined when the server sent something else. */
async function readProblem(response: Response): Promise<Problem | undefined> {
  if (!(response.headers.get("content-type") ?? "").includes("json")) {
    return undefined;
  }

  try {
    const parsed = safeParse(problemSchema, await response.json());

    return parsed.success ? parsed.output : undefined;
  } catch {
    return undefined;
  }
}
