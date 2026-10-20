import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement } from "react";

import { CopyButton } from "~/components/ui/copy-text.tsx";
import { shellTokens } from "~/lib/shell.ts";
import type { ShellTokenKind } from "~/lib/shell.ts";

/**
 * The weights of a shell line: the program is what the reader looks for first, flags give the line
 * its structure, values are what the reader checks, and a placeholder is what the reader replaces.
 */
const tokenClasses: Readonly<Record<ShellTokenKind, string>> = {
  command: "font-semibold text-kumo-default",
  flag: "text-kumo-link",
  name: "text-kumo-default",
  assign: "text-kumo-subtle",
  value: "text-kumo-default",
  placeholder: "text-kumo-warning",
  arg: "text-kumo-default",
  operator: "text-kumo-subtle",
  space: "",
};

/**
 * A wrapped line breaks at the spaces between words, never inside one: a browser would otherwise
 * break a flag after its dashes. Every word is an inline block, so it moves to the next line whole;
 * a value or an argument may be a key or a URL longer than the line, and only that breaks inside.
 */
const wrapClasses: Readonly<Record<ShellTokenKind, string>> = {
  command: "inline-block whitespace-nowrap",
  flag: "inline-block whitespace-nowrap",
  name: "inline-block whitespace-nowrap",
  assign: "inline-block whitespace-nowrap",
  value: "inline-block wrap-anywhere",
  placeholder: "inline-block whitespace-nowrap",
  arg: "inline-block wrap-anywhere",
  operator: "inline-block whitespace-nowrap",
  space: "",
};

/** A shell command line coloured by what each word does. */
export function ShellText({
  command,
  wrap = false,
  className,
}: {
  readonly command: string;
  /** Let the line wrap between words, keeping the line breaks it was written with. */
  readonly wrap?: boolean;
  readonly className?: string;
}): ReactElement {
  return (
    <code className={cn("font-mono", wrap && "break-normal whitespace-pre-wrap", className)}>
      {shellTokens(command).map((token) => (
        <span
          key={token.from}
          className={cn(tokenClasses[token.kind], wrap && wrapClasses[token.kind])}
        >
          {token.text}
        </span>
      ))}
    </code>
  );
}

/** The box's inset: the copy button at the end sits 6px or 8px in from the ring. */
const sizes = {
  sm: "py-1.5 pr-1.5 pl-3",
  base: "py-2 pr-2 pl-3",
} as const;

/**
 * A command line in the box a freshly minted key is shown in: tinted, ringed, and a copy button at
 * its end that is always there, the way the dashboard's install commands have one, so a touch
 * screen, which has no hover to reveal an icon, can copy it too. A wrapped box keeps the line
 * breaks the command was written with, so a multi-line Docker command shows as the lines it will be
 * pasted as, and the button stays on the first line; the words themselves stay whole. A command on
 * one line is cut short with an ellipsis, and the button copies all of it.
 */
export function CommandBox({
  command,
  size = "base",
  wrap = false,
  label = "Copy command",
  className,
}: {
  readonly command: string;
  readonly size?: keyof typeof sizes;
  readonly wrap?: boolean;
  /** The accessible name of the copy button. */
  readonly label?: string;
  readonly className?: string;
}): ReactElement {
  return (
    <div
      className={cn(
        "flex min-w-0 gap-2 rounded-lg bg-kumo-tint ring ring-kumo-line",
        wrap ? "items-start" : "items-center",
        sizes[size],
        className,
      )}
    >
      <ShellText
        command={command}
        wrap={wrap}
        className={cn(
          "block min-w-0 flex-1 text-[0.9em]",
          wrap ? "py-1" : "truncate whitespace-pre",
        )}
      />
      <CopyButton value={command} label={label} />
    </div>
  );
}
