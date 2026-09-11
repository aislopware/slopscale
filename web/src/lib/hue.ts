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
  const hue = hueOf(seed, steps);

  return {
    backgroundColor: `light-dark(oklch(0.93 0.045 ${hue}), oklch(0.3 0.05 ${hue}))`,
    color: `light-dark(oklch(0.42 0.11 ${hue}), oklch(0.86 0.07 ${hue}))`,
  };
}
