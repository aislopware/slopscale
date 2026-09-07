/** The address a client registers against: whatever origin this console is being served from. */
export function serverUrl(): string {
  return globalThis.location.origin;
}

/**
 * The single command that puts a machine on this tailnet. With a key in hand it needs no further
 * input, so the "Add machine" dialog can hand over a line that is ready to paste; without one the
 * placeholder shows where the key goes.
 */
export function connectCommand(key: string): string {
  return `tailscale up --login-server=${serverUrl()} --authkey=${key}`;
}

export const platforms = ["linux", "macos", "windows", "docker", "mobile"] as const;
export type Platform = (typeof platforms)[number];

export const platformLabels: Record<Platform, string> = {
  linux: "Linux",
  macos: "macOS",
  windows: "Windows",
  docker: "Docker",
  mobile: "iOS & Android",
};

export function isPlatform(value: string): value is Platform {
  return platforms.some((known) => known === value);
}

/** What the join builder hands over for one platform. */
export interface JoinInstructions {
  /** The line to paste, or null when the platform has no shell (the phone apps). */
  readonly command: string | null;
  /** What the QR code encodes: the command, or the server address for the phone apps. */
  readonly qr: string;
  /** One sentence on where to run the command, or the steps a phone app needs. */
  readonly note: string;
}

type Builder = (command: string, key: string, server: string) => JoinInstructions;

const builders: Record<Platform, Builder> = {
  linux: (command) => {
    const line = `curl -fsSL https://tailscale.com/install.sh | sh && sudo ${command}`;

    return {
      command: line,
      qr: line,
      note: "Installs Tailscale with the official script, then joins. Drop the first half if it is already installed.",
    };
  },
  macos: (command) => {
    const line = `brew install tailscale && sudo brew services start tailscale && sudo ${command}`;

    return {
      command: line,
      qr: line,
      note: "For the Homebrew build: installs the formula, starts the tailscaled service, then joins. The Mac App Store app instead takes the server address under the menu bar icon: hold Option, choose Debug, then Custom Login Server.",
    };
  },
  windows: (command) => {
    const line = `winget install --id Tailscale.Tailscale -e; ${command}`;

    return {
      command: line,
      qr: line,
      note: "Run in PowerShell as administrator. Drop the winget half if Tailscale is already installed.",
    };
  },
  docker: (_command, key, server) => {
    const parts = [
      "docker run -d --name tailscale --hostname my-container",
      "-v tailscale-state:/var/lib/tailscale -e TS_STATE_DIR=/var/lib/tailscale",
      "--cap-add NET_ADMIN --device /dev/net/tun",
      `-e TS_AUTHKEY=${key} -e TS_EXTRA_ARGS=--login-server=${server}`,
      "tailscale/tailscale",
    ];

    return {
      command: parts.join(" \\\n  "),
      qr: parts.join(" "),
      note: "Change the hostname to the name the container should have in the machine list. The volume, with TS_STATE_DIR pointing at it, keeps the identity across restarts; without the variable the container registers anew each time.",
    };
  },
  mobile: (_command, _key, server) => ({
    command: null,
    qr: server,
    note: `Open the Tailscale app, go to Settings, choose Use an alternate server (Android) or Alternate coordination server (iOS), paste ${server}, then sign in with the key or the web login.`,
  }),
};

/**
 * The join command for each platform. Every one carries the key and this server's address, so the
 * machine registers without signing in; the shape follows what the platform's Tailscale build
 * expects. The phone apps take no command, so they get the server address and the menu path.
 */
export function joinInstructions(platform: Platform, key: string): JoinInstructions {
  return builders[platform](connectCommand(key), key, serverUrl());
}
