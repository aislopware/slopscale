import { errorMessage } from "~/api/error.ts";

/**
 * A request the browser never got an answer to: the connection dropped, the server is down, or the
 * network in between is. The fetch layer raises this in place of the browser's bare TypeError so
 * that a TypeError elsewhere in the console (a bug) is never mistaken for an outage.
 */
export class NetworkError extends Error {
  constructor(cause: unknown) {
    super(errorMessage(cause), { cause });
    this.name = "NetworkError";
  }
}
