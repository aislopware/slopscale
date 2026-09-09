import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement } from "react";

import { CopyText } from "~/components/ui/copy-text.tsx";
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

/** The box's own inset plus the control's 4px puts the text where the key box puts its key. */
const sizes = {
  sm: "px-2 py-2",
  base: "px-2 py-3",
} as const;

/**
 * A command line in the box a freshly minted key is shown in: tinted, ringed, the copy icon on the
 * first line. A wrapped box keeps the line breaks the command was written with, so a multi-line
 * Docker command shows as the lines it will be pasted as; the words themselves stay whole.
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
  /** The accessible name of the control. */
  readonly label?: string;
  readonly className?: string;
}): ReactElement {
  return (
    <div className={cn("rounded-lg bg-kumo-tint ring ring-kumo-line", sizes[size], className)}>
      <CopyText
        value={command}
        display={<ShellText command={command} wrap={wrap} />}
        label={label}
        wrap={wrap}
        // The control's negative margin would take its width out of the column, so a line that
        // fits the column would still be cut short. On a wrapped command the icon stays on the
        // first line; on one line it sits on the line's centre like every other copy icon.
        className={cn("mx-0 w-full justify-between gap-2 text-left", wrap && "items-start")}
      />
    </div>
  );
}
