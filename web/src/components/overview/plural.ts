/**
 * "1 machine", "2 machines". The overview counts a handful of nouns in its context lines, all of
 * which take a plain -s; anything irregular belongs in its own helper.
 */
export function plural(count: number, noun: string): string {
  return count === 1 ? `${count} ${noun}` : `${count} ${noun}s`;
}
