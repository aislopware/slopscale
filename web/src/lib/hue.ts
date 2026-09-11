const hueSteps = 360;
const hashMultiplier = 31;
/** Keeps the running hash inside the range multiplication is exact in. */
const hashModulus = 4_294_967_296;

function hashOf(seed: string): number {
  let hash = 0;

  for (const char of seed) {
    hash = (hash * hashMultiplier + (char.codePointAt(0) ?? 0)) % hashModulus;
  }

  return hash;
}

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
 * is its own colour field instead of one of twelve flat tints, while the lightness stays the quiet
 * band of the tags and the ink keeps the same contrast on both themes.
 */
export function seededGradient(seed: string): {
  readonly backgroundImage: string;
  readonly color: string;
} {
  const first = hueOf(seed);
  const second = companionHue(seed);
  const angle = angleMin + (Math.floor(hashOf(seed) / angleBits) % angleRange);

  return {
    backgroundImage: `linear-gradient(${angle}deg, light-dark(oklch(0.9 0.07 ${first}), oklch(0.36 0.08 ${first})), light-dark(oklch(0.82 0.1 ${second}), oklch(0.28 0.08 ${second})))`,
    color: `light-dark(oklch(0.34 0.11 ${first}), oklch(0.9 0.06 ${first}))`,
  };
}
