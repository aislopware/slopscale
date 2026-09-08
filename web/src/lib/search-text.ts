import { object, optional, pipe, transform, unknown } from "valibot";

function toText(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

/** A free-text parameter: absent means empty, so an untouched search box never reaches the URL. */
export const optionalText = optional(pipe(unknown(), transform(toText)));

/** The search of a page whose only parameter is its search box, `q`. */
export const textSearchSchema = object({ q: optionalText });
