/** Where the slopscale documentation is published (GitHub Pages, built from `docs/`). */
export const docsBase = "https://aislopware.github.io/slopscale/";

/**
 * The address of a documentation page, from its path under `docs/content` without the extension:
 * `docsUrl("ref/dns")` is the DNS reference.
 */
export function docsUrl(path: string): string {
  return `${docsBase}${path.replaceAll(/^\/+|\/+$/gu, "")}/`;
}
