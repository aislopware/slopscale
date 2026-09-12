const hueSteps = 360;
/**
 * FNV-1a's 32-bit offset basis (0x811C9DC5) and prime (0x01000193), in decimal because oxfmt and
 * oxlint disagree on hex case.
 */
const fnvOffset = 2_166_136_261;
const fnvPrime = 16_777_619;
/**
 * MurmurHash3's 32-bit finaliser: shifts and multipliers that spread every input bit to every
 * output bit.
 */
const mixShiftOuter = 16;
const mixShiftInner = 13;
const mixMultiplierFirst = 2_246_822_507; // 0x85EBCA6B
const mixMultiplierSecond = 3_266_489_909; // 0xC2B2AE35

/**
 * FNV-1a over the seed's code points, then MurmurHash3's finaliser, so a one-character change moves
 * every bit of the hash. A plain polynomial hash moved the hue by the difference of the last
 * character alone: "Cong Tran" and "Cong Tram" were one degree apart, and the names people tell
 * apart the least got the marks that differed the least.
 */
/* eslint-disable no-bitwise -- a hash is arithmetic on bits; the operators are the point. */
function hashOf(seed: string): number {
  let hash = fnvOffset;

  for (const char of seed) {
    hash = Math.imul(hash ^ (char.codePointAt(0) ?? 0), fnvPrime) >>> 0;
  }

  hash ^= hash >>> mixShiftOuter;
  hash = Math.imul(hash, mixMultiplierFirst) >>> 0;
  hash ^= hash >>> mixShiftInner;
  hash = Math.imul(hash, mixMultiplierSecond) >>> 0;
  hash ^= hash >>> mixShiftOuter;

  return hash >>> 0;
}
/* eslint-enable no-bitwise */

/**
 * A hash of the seed onto the hue circle: the same seed is the same hue on every page and visit.
 * `steps` spreads the hues out: 12 steps keeps any two different hues at least 30° apart, so two
 * marks that differ read as different at a glance, at the price of a collision one time in twelve.
 */
export function hueOf(seed: string, steps: number = hueSteps): number {
  return (hashOf(seed) % steps) * (hueSteps / steps);
}

/**
 * A mark's colours from its hue: one lightness and chroma per theme, so every mark is as quiet as
 * every other and only the hue tells them apart. `light-dark()` follows Kumo's `color-scheme`, so
 * the pair flips with the theme without a second rule.
 */
export function seededColours(
  seed: string,
  steps?: number,
): { readonly backgroundColor: string; readonly color: string } {
  return hueColours(hueOf(seed, steps));
}

/**
 * The same quiet tint for a hue chosen by hand, so a fixed vocabulary (the roles) matches the
 * marks.
 */
export function hueColours(hue: number): {
  readonly backgroundColor: string;
  readonly color: string;
} {
  return {
    backgroundColor: `light-dark(oklch(0.93 0.045 ${hue}), oklch(0.3 0.05 ${hue}))`,
    color: `light-dark(oklch(0.42 0.11 ${hue}), oklch(0.86 0.07 ${hue}))`,
  };
}

/**
 * The golden angle: each step round the circle lands as far from every earlier step as any sequence
 * can, so ids 1, 2, 3… are 137° apart and never bunch the way hashed names do in a small team
 * (seven people, four of them green).
 */
const goldenAngle = 137.508;

/** True when the id is the server's integer id, the only kind the golden angle may be applied to. */
const integerId = /^\d+$/u;

/**
 * The base hue of a mark: spaced by the golden angle from the integer id when there is one, so the
 * few people on a tailnet are as far apart as they can be; hashed from the seed otherwise.
 */
export function baseHue(seed: string, id?: string): number {
  if (id !== undefined && integerId.test(id)) {
    return Math.round((Number(id) * goldenAngle) % hueSteps);
  }

  return hueOf(seed);
}
