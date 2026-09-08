import { Banner } from "@cloudflare/kumo/components/banner";
import { Button } from "@cloudflare/kumo/components/button";
import { Dialog } from "@cloudflare/kumo/components/dialog";
import { cn } from "@cloudflare/kumo/utils";
import { XIcon } from "@phosphor-icons/react";
import type { ComponentProps, ReactElement, ReactNode } from "react";

export const DialogRoot = Dialog.Root;
export const DialogTrigger = Dialog.Trigger;
export const DialogClose = Dialog.Close;

export interface DialogContentProps {
  readonly title: string;
  readonly description?: string | undefined;
  readonly size?: "sm" | "base" | "lg" | "xl";
  readonly className?: string;
  readonly children: ReactNode;
}

/**
 * Kumo's dialog in the same shape as its DeleteResource block, framed: the dialog surface is the
 * tinted band and the header, body and {@link DialogFooter} sit on an inset panel, like every other
 * framed surface in the console. Always render it, driven by `open` on the root, so the open/close
 * animation plays.
 */
export function DialogContent({
  title,
  description,
  size = "base",
  className,
  children,
}: DialogContentProps): ReactElement {
  return (
    <Dialog
      size={size}
      // Kumo pins the dialog below the top edge with no height limit, so a long form on a short
      // screen would lose its footer; the body scrolls instead.
      className={cn(
        "flex max-h-[calc(100svh-3rem)] flex-col bg-kumo-elevated p-1 ring-kumo-hairline sm:max-h-[calc(100svh-5rem)]",
        className,
      )}
    >
      <div className="flex min-h-0 flex-col overflow-hidden rounded-lg bg-kumo-base shadow-xs ring ring-kumo-line">
        <div className="flex items-center justify-between gap-4 border-b border-kumo-line px-6 py-4">
          <Dialog.Title className="text-lg font-semibold text-kumo-default">{title}</Dialog.Title>
          <Dialog.Close
            render={
              <Button
                variant="ghost"
                shape="square"
                size="sm"
                icon={XIcon}
                aria-label="Close"
                className="-m-1"
              />
            }
          />
        </div>
        <div className="flex min-h-0 flex-col gap-4 overflow-y-auto p-6">
          {description === undefined ? null : (
            <Dialog.Description className="max-w-prose text-pretty text-kumo-subtle">
              {description}
            </Dialog.Description>
          )}
          {children}
        </div>
      </div>
    </Dialog>
  );
}

export function DialogFooter({ className, ...props }: ComponentProps<"div">): ReactElement {
  return (
    <div
      className={cn(
        "-mx-6 mt-2 -mb-6 flex justify-end gap-3 border-t border-kumo-line px-6 py-4",
        className,
      )}
      {...props}
    />
  );
}

/** The request error shown inside a dialog, above its footer. */
export function DialogError({
  message,
}: {
  readonly message: string | undefined;
}): ReactElement | null {
  if (message === undefined || message === "") {
    return null;
  }

  return <Banner variant="error" title="Request failed" description={message} />;
}
