import { Button } from "@cloudflare/kumo/components/button";
import { Dialog } from "@cloudflare/kumo/primitives/dialog";
import { cn } from "@cloudflare/kumo/utils";
import { XIcon } from "@phosphor-icons/react";
import type { ReactElement, ReactNode } from "react";

/**
 * The Zero Trust log's detail sheet: pinned to the right edge at full height, 432px wide (at most
 * three quarters of a tablet's width, the whole of a phone's), sliding in from the right over 300ms
 * instead of scaling up in the middle.
 */
const popupClass = cn(
  "fixed inset-y-0 right-0 flex h-dvh flex-col bg-kumo-base text-kumo-default outline-none",
  "w-full max-w-full sm:w-[432px] md:max-w-[75%]",
  "border-l border-kumo-line shadow-lg transition-[translate] duration-300",
  "data-ending-style:translate-x-full data-starting-style:translate-x-full",
);

/**
 * A side sheet over the page, as Zero Trust opens a log entry or a record's details: a 432px panel
 * at the right edge over a 40% black scrim. The scrim is darker than a centred dialog's light wash
 * on purpose (§13.7): the sheet keeps the page in view beside it, so the page has to step further
 * back. The header carries the 24px title, an optional line under it and the close button; the body
 * scrolls on its own.
 */
export function Sheet({
  open,
  onOpenChange,
  title,
  description,
  actions,
  children,
}: {
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly title: ReactNode;
  /** One line under the title, such as what the entry is. */
  readonly description?: ReactNode;
  /** Small buttons under the title, such as a link to the full record. */
  readonly actions?: ReactNode;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 bg-black/40 transition-opacity duration-300 data-ending-style:opacity-0 data-starting-style:opacity-0" />
        <Dialog.Popup className={popupClass}>
          <div className="flex items-start justify-between gap-4 border-b border-kumo-line px-6 py-5">
            <div className="flex min-w-0 flex-col gap-1">
              <Dialog.Title className="text-2xl font-semibold text-kumo-default">
                {title}
              </Dialog.Title>
              {description === undefined ? null : (
                <Dialog.Description className="truncate text-kumo-subtle">
                  {description}
                </Dialog.Description>
              )}
              {actions === undefined ? null : (
                <div className="mt-2 flex flex-wrap items-center gap-2">{actions}</div>
              )}
            </div>
            <Dialog.Close
              render={<Button variant="ghost" shape="square" icon={XIcon} aria-label="Close" />}
            />
          </div>
          <div className="flex min-h-0 flex-1 flex-col gap-8 overflow-y-auto px-6 py-6">
            {children}
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

/** A titled part of a sheet: an 18px semibold heading over its facts. */
export function SheetSection({
  title,
  children,
}: {
  readonly title: string;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <section className="flex flex-col gap-1">
      <h3 className="text-lg font-semibold text-kumo-default">{title}</h3>
      <dl className="flex flex-col">{children}</dl>
    </section>
  );
}

/** One fact in a sheet: a 12px semibold label over its 14px value, a line under it. */
export function SheetFact({
  label,
  children,
}: {
  readonly label: string;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <div className="flex min-w-0 flex-col gap-1.5 border-b border-kumo-line py-3">
      <dt className="text-xs font-semibold text-kumo-default">{label}</dt>
      <dd className="min-w-0 [overflow-wrap:anywhere] text-kumo-default">{children}</dd>
    </div>
  );
}
