import makeQr from "qrcode-generator";
import type { ReactElement } from "react";

/** Quiet zone around the symbol, in modules; the QR spec asks for four. */
const quietZone = 4;

/**
 * The symbol as an SVG data URL, so it scales with its box. Scanners want dark modules on a light
 * ground whatever the theme, so it keeps plain black on white instead of the semantic tokens.
 */
export function qrDataUrl(text: string): string {
  const symbol = makeQr(0, "M");
  symbol.addData(text, "Byte");
  symbol.make();

  const count = symbol.getModuleCount();
  const size = count + quietZone * 2;
  const cells: string[] = [];

  for (let row = 0; row < count; row += 1) {
    for (let col = 0; col < count; col += 1) {
      if (symbol.isDark(row, col)) {
        cells.push(`M${col + quietZone} ${row + quietZone}h1v1h-1z`);
      }
    }
  }

  const svg =
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${size} ${size}" shape-rendering="crispEdges">` +
    `<rect width="${size}" height="${size}" fill="#fff"/><path d="${cells.join("")}" fill="#000"/></svg>`;

  return `data:image/svg+xml,${encodeURIComponent(svg)}`;
}

/** A QR code of the text. The symbol is rebuilt on every render; a few hundred bytes encode fast. */
export function QrCode({
  text,
  label,
  className,
}: {
  readonly text: string;
  /** The accessible name, since the picture itself says nothing to a screen reader. */
  readonly label: string;
  readonly className?: string;
}): ReactElement {
  return <img src={qrDataUrl(text)} alt={label} className={className} />;
}
