import { array, nullish, number, object, optional, string, unknown } from "valibot";
import type { InferOutput } from "valibot";

export const problemDetailSchema = object({
  location: optional(string()),
  message: optional(string()),
  value: optional(unknown()),
});

export const problemSchema = object({
  type: optional(string(), "about:blank"),
  title: optional(string()),
  status: optional(number()),
  detail: optional(string()),
  instance: optional(string()),
  errors: nullish(array(problemDetailSchema)),
});

/** RFC 9457 problem details as the server sends them. */
export type Problem = InferOutput<typeof problemSchema>;

export const statusUnauthorized = 401;
const statusForbidden = 403;
const statusNotFound = 404;

/**
 * A failed API call. The server answers every error as RFC 9457 problem details, so the message
 * shown to the operator is the problem's `detail` (falling back to its title) plus any field-level
 * errors.
 */
export class ApiError extends Error {
  readonly status: number;
  readonly problem: Problem | undefined;

  constructor(status: number, problem: Problem | undefined, fallback: string) {
    super(describe(problem) ?? fallback);
    this.name = "ApiError";
    this.status = status;
    this.problem = problem;
  }

  get unauthorized(): boolean {
    return this.status === statusUnauthorized;
  }

  get forbidden(): boolean {
    return this.status === statusForbidden;
  }

  get notFound(): boolean {
    return this.status === statusNotFound;
  }
}

function describe(problem: Problem | undefined): string | undefined {
  if (problem === undefined) {
    return undefined;
  }

  const headline = problem.detail ?? problem.title;
  const details = (problem.errors ?? [])
    .map((detail) => detail.message)
    .filter((message): message is string => message !== undefined && message !== "");

  if (details.length === 0) {
    return headline;
  }

  return headline === undefined ? details.join("; ") : `${headline}: ${details.join("; ")}`;
}

/** Turns anything a query or mutation rejected with into a sentence. */
export function errorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }

  if (typeof error === "string") {
    return error;
  }

  return "Something went wrong.";
}
