import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import type { ITheme } from "@xterm/xterm";
import { useEffect, useRef } from "react";
import type { ReactElement } from "react";

import "@xterm/xterm/css/xterm.css";
import type { IPN, IPNSSHSession } from "~/tsconnect/wasm-js.d.ts";

const terminalFontFamily =
  'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace';
const terminalFontSize = 14;
const terminalLineHeight = 1.25;

/**
 * The ANSI sixteen are literals on purpose: a terminal palette is program output rather than UI
 * chrome, and a program that asks for colour 1 expects red in either mode. The surface around them
 * comes from the Kumo theme, so the terminal is light in the light theme.
 */
const ansiPalette = {
  black: "#18181b",
  red: "#ef4444",
  green: "#22c55e",
  yellow: "#eab308",
  blue: "#3b82f6",
  magenta: "#a855f7",
  cyan: "#06b6d4",
  white: "#f4f4f5",
  brightBlack: "#71717a",
  brightRed: "#f87171",
  brightGreen: "#4ade80",
  brightYellow: "#fde047",
  brightBlue: "#60a5fa",
  brightMagenta: "#c084fc",
  brightCyan: "#22d3ee",
  brightWhite: "#ffffff",
};

/** How much of the foreground a selection lays over the terminal's background. */
const selectionShare = 25;

/** Only for a browser that cannot mix colours; grey reads as a selection in either mode. */
const fallbackSelection = "rgba(128, 128, 128, 0.35)";

/**
 * Mixes two resolved colours into an opaque one. xterm reads an alpha channel only out of `rgba()`
 * or an eight digit hex, and Kumo's tokens are `oklch()`, so the browser does the blending against
 * the background instead of the selection carrying alpha of its own.
 */
function mixColors(wrapper: HTMLElement, top: string, bottom: string): string {
  const probe = document.createElement("span");
  probe.style.backgroundColor = `color-mix(in srgb, ${top} ${selectionShare}%, ${bottom})`;
  if (probe.style.backgroundColor === "") {
    return fallbackSelection;
  }

  wrapper.append(probe);
  const mixed = globalThis.getComputedStyle(probe).backgroundColor;
  probe.remove();
  return mixed;
}

/**
 * Reads the terminal's colours back off its wrapper: the Kumo tokens on it are custom properties
 * that only the browser can resolve, and xterm's theme takes literal colours.
 */
export function readTerminalTheme(wrapper: HTMLElement): ITheme {
  const style = globalThis.getComputedStyle(wrapper);
  const background = style.backgroundColor;
  const foreground = style.color;

  return {
    ...ansiPalette,
    background,
    foreground,
    cursor: foreground,
    cursorAccent: background,
    selectionBackground: mixColors(wrapper, foreground, background),
  };
}

export interface SSHTerminalProps {
  readonly ipn: IPN;
  readonly host: string;
  readonly username: string;
  readonly onConnectionProgress: (message: string) => void;
  readonly onConnected: () => void;
  readonly onDone: () => void;
  readonly registerSession: (session: IPNSSHSession | null) => void;
}

/** The live terminal and the teardown that closes the SSH session behind it. */
interface OpenTerminal {
  readonly term: Terminal;
  readonly close: () => void;
}

/** Opens the terminal in `container` and dials the SSH session it is bound to. */
function openTerminal(
  container: HTMLElement,
  theme: ITheme,
  props: SSHTerminalProps,
): OpenTerminal {
  const { ipn, host, username, onConnectionProgress, onConnected, onDone, registerSession } = props;

  const term = new Terminal({
    cursorBlink: true,
    fontFamily: terminalFontFamily,
    fontSize: terminalFontSize,
    lineHeight: terminalLineHeight,
    // Announces output and exposes the buffer as text, which the canvas alone never is.
    screenReaderMode: true,
    theme,
  });

  const fitAddon = new FitAddon();
  term.loadAddon(fitAddon);
  term.open(container);

  const fit = (): void => {
    if (container.clientWidth === 0 || container.clientHeight === 0) {
      return;
    }
    fitAddon.fit();
  };

  const frame = requestAnimationFrame(fit);

  let onDataHook: ((data: string) => void) | undefined;
  const dataSubscription = term.onData((data: string): void => {
    onDataHook?.(data);
  });

  term.focus();

  const sshSession = ipn.ssh(host, username, {
    writeFn: (input: string): void => {
      term.write(input);
    },
    writeErrorFn: (error: string): void => {
      term.write(error);
    },
    setReadFn: (hook: (data: string) => void): void => {
      onDataHook = hook;
    },
    rows: term.rows,
    cols: term.cols,
    onConnectionProgress,
    onConnected,
    onDone,
  });

  registerSession(sshSession);

  const resizeObserver = new ResizeObserver(fit);
  resizeObserver.observe(container);

  const resizeSubscription = term.onResize(({ rows, cols }): void => {
    sshSession.resize(rows, cols);
  });

  return {
    term,
    close: (): void => {
      registerSession(null);
      cancelAnimationFrame(frame);
      resizeSubscription.dispose();
      dataSubscription.dispose();
      resizeObserver.disconnect();
      sshSession.close();
      term.dispose();
    },
  };
}

/**
 * An xterm bound to one SSH session over the in-browser Tailscale client. The session starts when
 * the terminal mounts and closes when it unmounts, so a caller asks for a new one by remounting
 * this component with a fresh `key`.
 *
 * The callbacks must be stable: any change to them tears the session down and dials again.
 */
export function SSHTerminal({
  ipn,
  host,
  username,
  onConnectionProgress,
  onConnected,
  onDone,
  registerSession,
}: SSHTerminalProps): ReactElement {
  const wrapperRef = useRef<HTMLDivElement | null>(null);
  const containerRef = useRef<HTMLElement | null>(null);
  const termRef = useRef<Terminal | null>(null);

  useEffect(() => {
    const wrapper = wrapperRef.current;
    const container = containerRef.current;
    if (wrapper === null || container === null) {
      return (): void => {
        // Nothing was opened, so there is nothing to tear down.
      };
    }

    const opened = openTerminal(container, readTerminalTheme(wrapper), {
      ipn,
      host,
      username,
      onConnectionProgress,
      onConnected,
      onDone,
      registerSession,
    });
    termRef.current = opened.term;

    return (): void => {
      termRef.current = null;
      opened.close();
    };
  }, [ipn, host, username, onConnectionProgress, onConnected, onDone, registerSession]);

  // Repainting is deliberately out of the session effect: switching the theme must recolour the
  // terminal, never hang up on it.
  useEffect(() => {
    const wrapper = wrapperRef.current;
    if (wrapper === null) {
      return (): void => {
        // Nothing to observe without a wrapper to read the colours off.
      };
    }

    const repaint = (): void => {
      const term = termRef.current;
      if (term !== null) {
        term.options.theme = readTerminalTheme(wrapper);
      }
    };

    const observer = new MutationObserver(repaint);
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-mode"],
    });

    return (): void => {
      observer.disconnect();
    };
  }, []);

  return (
    <div
      ref={wrapperRef}
      className="h-full w-full flex-1 overflow-hidden bg-kumo-base p-3 font-mono text-kumo-default"
    >
      {/* A named region so a screen reader says which machine the output belongs to. */}
      <section
        ref={containerRef}
        aria-label={`Terminal on ${host}`}
        className="h-full w-full overflow-hidden"
      />
    </div>
  );
}
