import type { ExpressionError } from "~/lib/posture/expression.ts";

/** Thrown inside the parser to unwind to parseExpression, which turns it into a result. */
export class ExpressionFailureError extends Error {
  readonly detail: ExpressionError;

  constructor(detail: ExpressionError) {
    super(detail.message);
    this.name = "ExpressionFailureError";
    this.detail = detail;
  }
}
