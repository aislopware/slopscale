import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement, ReactNode } from "react";

/**
 * A CLI flag, config key or other literal inside a sentence. Monospace at 0.9em so it sits on the
 * line without towering over it, and never broken across lines: a flag split after its dashes reads
 * as two words.
 */
export function Code({
  children,
  className,
}: {
  readonly children: ReactNode;
  readonly className?: string;
}): ReactElement {
  return (
    <code className={cn("font-mono text-[0.9em] whitespace-nowrap", className)}>{children}</code>
  );
}
