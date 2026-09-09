import type { ITheme } from "@xterm/xterm";
import { describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-react";

import { readTerminalTheme, SSHTerminal } from "~/components/ssh/terminal.tsx";
import type { IPN, IPNSSHSession } from "~/tsconnect/wasm-js.d.ts";

const sshSession: IPNSSHSession = {
  resize: (): boolean => true,
  close: (): boolean => true,
};

/** An in-browser client that hands out a session without touching the network. */
const ipn: IPN = {
  run: (): void => {
    // The page joins the tailnet; the terminal only dials.
  },
  login: (): void => {
    // Never called by the terminal.
  },
  logout: (): void => {
    // Never called by the terminal.
  },
  ssh: (): IPNSSHSession => sshSession,
  fetch: () =>
    Promise.resolve({
      status: 200,
      statusText: "OK",
      text: (): Promise<string> => Promise.resolve(""),
    }),
};

function noop(): void {
  // The terminal reports progress the test does not read.
}

/** Runs `read` against a wrapper carrying the same Kumo tokens as the terminal's own. */
function withWrapper(read: (wrapper: HTMLDivElement) => ITheme): ITheme {
  const wrapper = document.createElement("div");
  wrapper.className = "bg-kumo-base text-kumo-default";
  document.body.append(wrapper);
  try {
    return read(wrapper);
  } finally {
    wrapper.remove();
  }
}

function withMode(mode: string, read: () => ITheme): ITheme {
  const previous = document.documentElement.dataset["mode"];
  document.documentElement.dataset["mode"] = mode;
  try {
    return read();
  } finally {
    if (previous === undefined) {
      delete document.documentElement.dataset["mode"];
    } else {
      document.documentElement.dataset["mode"] = previous;
    }
  }
}

describe(readTerminalTheme, () => {
  it("takes the surface colours from the theme and keeps the ANSI palette", () => {
    const theme = withWrapper(readTerminalTheme);

    expect(theme.background).toMatch(/\S/u);
    expect(theme.foreground).toMatch(/\S/u);
    expect(theme.cursor).toBe(theme.foreground);
    expect(theme.cursorAccent).toBe(theme.background);
    expect(theme.red).toBe("#ef4444");
  });

  it("resolves the selection to a colour xterm can parse", () => {
    const theme = withWrapper(readTerminalTheme);

    expect(theme.selectionBackground).not.toContain("color-mix");
    expect(theme.selectionBackground).not.toBe(theme.background);
  });

  it("follows the mode the console is in", () => {
    const light = withMode("light", () => withWrapper(readTerminalTheme));
    const dark = withMode("dark", () => withWrapper(readTerminalTheme));

    expect(light.background).not.toBe(dark.background);
    expect(light.foreground).not.toBe(dark.foreground);
  });
});

describe(SSHTerminal, () => {
  it("names the terminal after the machine and announces it", async () => {
    const screen = await render(
      <SSHTerminal
        ipn={ipn}
        host="build-box"
        username="root"
        onConnectionProgress={noop}
        onConnected={noop}
        onDone={noop}
        registerSession={noop}
      />,
    );

    await expect
      .element(screen.getByRole("region", { name: "Terminal on build-box" }))
      .toBeVisible();
    expect(document.querySelector(".xterm-accessibility")).not.toBeNull();
  });

  it("repaints when the console switches mode", async () => {
    document.documentElement.dataset["mode"] = "dark";
    await render(
      <SSHTerminal
        ipn={ipn}
        host="build-box"
        username="root"
        onConnectionProgress={noop}
        onConnected={noop}
        onDone={noop}
        registerSession={noop}
      />,
    );

    const viewport = document.querySelector<HTMLElement>(".xterm-scrollable-element");
    expect(viewport).not.toBeNull();
    const dark = viewport?.style.backgroundColor;

    document.documentElement.dataset["mode"] = "light";
    await vi.waitFor(() => {
      expect(viewport?.style.backgroundColor).not.toBe(dark);
    });
  });
});
