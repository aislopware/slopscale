import { CheckCircleIcon } from "@phosphor-icons/react";
import type { ReactElement, ReactNode } from "react";

const iconSize = 16;

/** A step as the wizard draws it: a white card with its title and lead, and a footer band. */
export function StepCard({
  title,
  description,
  footer,
  children,
}: {
  readonly title: string;
  readonly description: ReactNode;
  readonly footer: ReactNode;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <section className="flex flex-col rounded-xl bg-kumo-elevated ring ring-kumo-hairline">
      <div className="flex flex-col gap-5 rounded-xl bg-kumo-base px-7 py-6 ring ring-kumo-line">
        <div className="flex flex-col gap-1">
          <h2 className="text-base font-medium text-kumo-default">{title}</h2>
          <p className="text-pretty text-kumo-subtle">{description}</p>
        </div>
        {children}
      </div>
      <div className="flex min-h-12 flex-wrap items-center justify-end gap-2 px-2 py-2 pl-4">
        {footer}
      </div>
    </section>
  );
}

/**
 * A finished step, folded: its title and what it settled, faded so the current step leads. A step
 * that was skipped rather than settled carries no check.
 */
export function Folded({
  title,
  lines,
  settled = true,
}: {
  readonly title: string;
  readonly lines: readonly string[];
  readonly settled?: boolean;
}): ReactElement {
  return (
    <div className="flex flex-col gap-1.5 rounded-xl bg-kumo-base px-7 py-4 opacity-60 ring ring-kumo-line">
      <span className="text-sm font-medium text-kumo-default">{title}</span>
      {lines.map((line) => (
        <span key={line} className="flex items-center gap-2 text-sm text-kumo-subtle">
          {settled ? (
            <CheckCircleIcon
              size={iconSize}
              weight="fill"
              aria-hidden
              className="text-kumo-success"
            />
          ) : null}
          {line}
        </span>
      ))}
    </div>
  );
}
