/**
 * Numbers as Go's strconv.ParseFloat reads them, which is what the server's posture parser calls:
 * decimal with an optional fraction and exponent, hexadecimal with a mandatory p exponent, inf,
 * infinity and nan in any case, underscores between digits. JavaScript's Number() takes binary and
 * octal and refuses hex floats and underscores, so it cannot stand in.
 */

const digits = String.raw`\d(?:_?\d)*`;
const hexDigits = String.raw`[0-9a-f](?:_?[0-9a-f])*`;
const decimal = String.raw`(?:${digits}(?:\.(?:${digits})?)?|\.${digits})(?:e[+\-]?${digits})?`;
const hex = String.raw`0x_?(?:${hexDigits}(?:\.(?:${hexDigits})?)?|\.${hexDigits})p[+\-]?${digits}`;
const goFloat = new RegExp(String.raw`^[+\-]?(?:inf(?:inity)?|nan|${decimal}|${hex})$`, "iv");
const hexBase = 16;
const binaryBase = 2;

export function isGoFloat(word: string): boolean {
  return goFloat.test(word);
}

function hexFloat(text: string): number {
  const [mantissa = "", exponent = "0"] = text.split("p");
  const [whole = "", fraction = ""] = mantissa.split(".");
  const value =
    Number.parseInt(whole === "" ? "0" : whole, hexBase) +
    Number.parseInt(fraction === "" ? "0" : fraction, hexBase) / hexBase ** fraction.length;

  return value * binaryBase ** Number(exponent);
}

/** The value of a Go float literal, or null when the word is not one. */
export function parseGoFloat(word: string): number | null {
  if (!goFloat.test(word)) {
    return null;
  }

  const bare = word.replaceAll("_", "").toLowerCase();
  const sign = bare.startsWith("-") ? -1 : 1;
  const unsigned = bare.replace(/^[+\-]/v, "");

  if (unsigned.startsWith("inf")) {
    return sign * Number.POSITIVE_INFINITY;
  }

  if (unsigned === "nan") {
    return Number.NaN;
  }

  if (unsigned.startsWith("0x")) {
    return sign * hexFloat(unsigned.slice("0x".length));
  }

  return sign * Number(unsigned);
}
