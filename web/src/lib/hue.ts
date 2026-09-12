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

/** The least and the most a companion hue sits from the first, in degrees. */
const spreadMin = 40;
const spreadRange = 60;
/** Where the gradient's angle starts and how far it ranges, in degrees. */
const angleMin = 110;
const angleRange = 140;
/** The hash bits the spread and the angle are read from, so neither repeats the hue's. */
const spreadBits = 4096;
const angleBits = 65_536;

/**
 * A second hue for the same seed, 40° to 100° round the circle from the first, so the pair is
 * analogous: close enough to blend into one colour and far enough to move across the mark.
 */
function companionHue(seed: string): number {
  const spread = spreadMin + (Math.floor(hashOf(seed) / spreadBits) % spreadRange);

  return (hueOf(seed) + spread) % hueSteps;
}

/**
 * Two of the seed's hues blended across a mark at an angle the seed picks, so every person's avatar
 * is its own colour field instead of one of twelve flat tints. The mark is richer than a tag chip
 * (the tags' quiet band left every blue and every green the same pale wash at 24px, so most of a
 * team looked alike), while the ink keeps the same contrast on both themes.
 */
export function seededGradient(seed: string): {
  readonly backgroundImage: string;
  readonly color: string;
} {
  const first = hueOf(seed);
  const second = companionHue(seed);
  const angle = angleMin + (Math.floor(hashOf(seed) / angleBits) % angleRange);

  return {
    backgroundImage: `linear-gradient(${angle}deg, light-dark(oklch(0.84 0.11 ${first}), oklch(0.42 0.11 ${first})), light-dark(oklch(0.72 0.15 ${second}), oklch(0.32 0.11 ${second})))`,
    color: `light-dark(oklch(0.3 0.12 ${first}), oklch(0.92 0.06 ${first}))`,
  };
}
