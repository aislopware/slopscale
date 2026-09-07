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
 * Kumo's dialog in the same shape as its DeleteResource block: a bordered header with the
 * sentence-case title and close button, the body with an optional description first, and a bordered
 * {@link DialogFooter}. Always render it, driven by `open` on the root, so the open/close animation
 * plays.
 */
export function DialogContent({
  title,
  description,
  size = "base",
  className,
  children,
}: DialogContentProps): ReactElement {
  return (
    <Dialog size={size} className={cn("flex flex-col p-0", className)}>
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
      <div className="flex flex-col gap-4 p-6">
        {description === undefined ? null : (
          <Dialog.Description className="max-w-prose text-pretty text-kumo-subtle">
            {description}
          </Dialog.Description>
        )}
        {children}
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
