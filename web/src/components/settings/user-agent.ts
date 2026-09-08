/**
 * Reads a browser and an operating system out of a user agent string. Every browser lies about
 * being every other browser in that header, so the order below matters: the ones that carry more
 * than one name are matched before the names they carry.
 */

interface Rule {
  readonly label: string;
  readonly pattern: RegExp;
}

/** Browsers, most specific first: Edge and Opera claim Chrome, and Chrome claims Safari. */
const browsers: readonly Rule[] = [
  { label: "Edge", pattern: /\bEdg(?:e|A|iOS)?\//v },
  { label: "Opera", pattern: /\bOPR\/|\bOpera\//v },
  { label: "Samsung Internet", pattern: /\bSamsungBrowser\//v },
  { label: "Brave", pattern: /\bBrave\//v },
  { label: "Vivaldi", pattern: /\bVivaldi\//v },
  { label: "Firefox", pattern: /\bFirefox\/|\bFxiOS\//v },
  { label: "Chrome", pattern: /Chrome\/|\bCriOS\/|\bChromium\//v },
  { label: "Safari", pattern: /\bSafari\//v },
  { label: "curl", pattern: /^curl\//v },
];

/** Operating systems, most specific first: Android and Chrome OS both mention Linux. */
const systems: readonly Rule[] = [
  { label: "Android", pattern: /\bAndroid\b/v },
  { label: "Chrome OS", pattern: /\bCrOS\b/v },
  { label: "iPadOS", pattern: /\biPad\b/v },
  { label: "iOS", pattern: /\biPhone\b|\biPod\b/v },
  { label: "macOS", pattern: /\bMac OS X\b|\bMacintosh\b/v },
  { label: "Windows", pattern: /\bWindows\b/v },
  { label: "Linux", pattern: /\bLinux\b|\bX11\b/v },
];

function match(rules: readonly Rule[], agent: string): string | undefined {
  return rules.find((rule) => rule.pattern.test(agent))?.label;
}

/**
 * "Chrome on macOS" for a header the parser recognises. Anything it does not recognise is handed
 * back as it came, because a raw header still tells an operator which sign-in is theirs; an empty
 * header (an older session, before the console recorded it) reads as "Unknown".
 */
export function describeUserAgent(agent: string): string {
  const trimmed = agent.trim();

  if (trimmed === "") {
    return "Unknown";
  }

  const browser = match(browsers, trimmed);

  if (browser === undefined) {
    return trimmed;
  }

  const system = match(systems, trimmed);

  return system === undefined ? browser : `${browser} on ${system}`;
}
