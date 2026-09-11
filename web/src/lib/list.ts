/**
 * The rows of a list field as a request wants them: each one trimmed and normalized, the blank ones
 * left out, and a value entered twice kept once.
 */
export function listValues(
  rows: readonly string[],
  normalize: (row: string) => string = (row) => row,
): string[] {
  const values: string[] = [];

  for (const row of rows) {
    const trimmed = row.trim();

    if (trimmed !== "") {
      const value = normalize(trimmed);

      if (!values.includes(value)) {
        values.push(value);
      }
    }
  }

  return values;
}

/** The first problem among the rows that hold text; a blank row is nothing yet, not a mistake. */
export function listError(
  rows: readonly string[],
  validate: (row: string) => string | null,
): string | null {
  for (const row of rows) {
    const trimmed = row.trim();
    const issue = trimmed === "" ? null : validate(trimmed);

    if (issue !== null) {
      return issue;
    }
  }

  return null;
}
