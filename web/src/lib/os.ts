/** The families the console tells apart; anything else is "other" and draws the generic mark. */
export type OsFamily =
  | "macos"
  | "ios"
  | "tvos"
  | "windows"
  | "linux"
  | "android"
  | "freebsd"
  | "other";

/** What the console calls each family. */
export const osLabels: Record<OsFamily, string> = {
  macos: "macOS",
  ios: "iOS",
  tvos: "tvOS",
  windows: "Windows",
  linux: "Linux",
  android: "Android",
  freebsd: "FreeBSD",
  other: "Unknown OS",
};

/**
 * The family of the OS string a client reports (`linux`, `macOS`, `windows`, `iOS`, `android`,
 * `freebsd`, `tvOS`, `illumos`, `openbsd`), compared without case so a spelling change upstream
 * does not lose the mark.
 */
/** Tailscale's spellings, lower-cased, to the family each belongs to. */
const families: Record<string, OsFamily> = {
  macos: "macos",
  darwin: "macos",
  ios: "ios",
  ipados: "ios",
  tvos: "tvos",
  windows: "windows",
  linux: "linux",
  android: "android",
  freebsd: "freebsd",
};

/**
 * The family of the OS string a client reports (`linux`, `macOS`, `windows`, `iOS`, `android`,
 * `freebsd`, `tvOS`, `illumos`, `openbsd`), compared without case so a spelling change upstream
 * does not lose the mark.
 */
export function osFamily(os: string): OsFamily {
  return families[os.trim().toLowerCase()] ?? "other";
}

/** "macOS 15.1", "Linux", or "" when the client has not said. */
export function osLabel(os: string, version = ""): string {
  if (os.trim() === "") {
    return "";
  }

  const family = osFamily(os);
  const name = family === "other" ? os : osLabels[family];

  return version.trim() === "" ? name : `${name} ${version.trim()}`;
}
