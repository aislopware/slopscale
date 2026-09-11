import { ApiError, errorMessage } from "~/api/error.ts";

/**
 * What went wrong, in the terms the page speaks: not the HTTP status but what it means for the
 * operator and what they can do about it.
 */
export type TroubleKind =
  | "unreachable"
  | "session"
  | "forbidden"
  | "missing"
  | "server"
  | "console";

export interface Trouble {
  readonly kind: TroubleKind;
  /** A word or two under the sentence: the HTTP status, or where the error came from. */
  readonly code: string;
  readonly title: string;
  /** What it means and what to do, in a sentence or two. */
  readonly description: string;
  /** The message as the code produced it, for the details fold. */
  readonly message: string;
  /** The request the server named as the one that failed, when it did. */
  readonly instance: string | undefined;
}

const statusUnauthorized = 401;
const statusForbidden = 403;
const statusNotFound = 404;
const statusServerError = 500;
const statusBadGateway = 502;
const statusServiceUnavailable = 503;
const statusGatewayTimeout = 504;
/** The proxy in front of slopscale answered for it: the server itself was not reached. */
const gatewayStatuses = new Set([statusBadGateway, statusServiceUnavailable, statusGatewayTimeout]);

const unreachable = {
  kind: "unreachable",
  title: "The server did not answer",
  description:
    "slopscale is not responding. It may be restarting, or the network in between is down. Try again.",
} as const;

const console = {
  kind: "console",
  code: "Console error",
  title: "The console hit an error",
  description:
    "The console failed, not your tailnet. Try again, or report it with the details below.",
} as const;

/** The page the address names does not exist; the router says so, not the server. */
export const pageNotFound: Trouble = {
  kind: "missing",
  code: "Not found",
  title: "Page not found",
  description: "There is no page at this address. It may have moved when the console was updated.",
  message: "",
  instance: undefined,
};

/** The server's detail as a sentence: it writes "node not found", the page says "Node not found." */
export function sentence(text: string): string {
  const trimmed = text.trim();

  if (trimmed === "") {
    return trimmed;
  }

  const capitalised = trimmed.charAt(0).toUpperCase() + trimmed.slice(1);

  return /[.!?]$/v.test(capitalised) ? capitalised : `${capitalised}.`;
}

function byStatus(error: ApiError): Omit<Trouble, "message" | "instance"> {
  const code = `HTTP ${error.status}`;
  const raw = error.problem?.detail;
  const detail = raw === undefined ? undefined : sentence(raw);

  if (error.status === statusUnauthorized) {
    return {
      kind: "session",
      code,
      title: "Your session has ended",
      description: "Sign in again to continue.",
    };
  }

  if (error.status === statusForbidden) {
    return {
      kind: "forbidden",
      code,
      title: "You do not have access",
      description:
        detail ??
        "Your account cannot open this page. An administrator can grant the role or scope it needs.",
    };
  }

  // slopscale answers every error as problem details. A bare 404 is the proxy in front of it
  // finding no backend, which is what a restart looks like from the browser.
  if (error.status === statusNotFound && error.problem === undefined) {
    return { ...unreachable, code };
  }

  if (error.status === statusNotFound) {
    return {
      kind: "missing",
      code,
      title: "Not found",
      description: detail ?? "This address no longer points to anything.",
    };
  }

  if (gatewayStatuses.has(error.status)) {
    return { ...unreachable, code };
  }

  if (error.status >= statusServerError) {
    return {
      kind: "server",
      code,
      title: "The server hit an error",
      description: detail ?? "slopscale could not finish the request. The server log has more.",
    };
  }

  return {
    kind: "server",
    code,
    title: "The server refused the request",
    description: detail ?? sentence(errorMessage(error)),
  };
}

/** Whether the browser never reached a server at all: fetch fails with a TypeError then. */
function isNetworkFailure(error: unknown): boolean {
  return error instanceof TypeError;
}

/** Reads anything a loader or a component threw as a Trouble. */
export function describeTrouble(error: unknown): Trouble {
  const message = errorMessage(error);

  if (error instanceof ApiError) {
    return { ...byStatus(error), message, instance: error.problem?.instance };
  }

  if (isNetworkFailure(error)) {
    return { ...unreachable, code: "No connection", message, instance: undefined };
  }

  return { ...console, message, instance: undefined };
}
