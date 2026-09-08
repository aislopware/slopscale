import { Banner } from "@cloudflare/kumo/components/banner";
import { Button } from "@cloudflare/kumo/components/button";
import { Dialog } from "@cloudflare/kumo/components/dialog";
import { cn } from "@cloudflare/kumo/utils";
import { XIcon } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
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
 * Kumo's dialog in the same shape as its DeleteResource block, framed. The dialog surface is the
 * tinted band, and the header, the body and the {@link DialogFooter} sit on an inset panel with the
 * same 4px gap and 12px/8px radii as a Frame. Always render it, driven by `open` on the root, so
 * the open/close animation plays.
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
        <DialogBody description={description}>{children}</DialogBody>
      </div>
    </Dialog>
  );
}

/**
 * The scrolling part of a dialog. When the form is taller than the screen, a hairline shadow under
 * the header and above the footer says so, because a body clipped flush against the divider looks
 * like the end of the form.
 */
function DialogBody({
  description,
  children,
}: {
  readonly description: string | undefined;
  readonly children: ReactNode;
}): ReactElement {
  const { ref, top, bottom } = useScrollEdges();

  return (
    <div className="relative flex min-h-0 flex-col">
      <div ref={ref} className="flex min-h-0 flex-col gap-4 overflow-y-auto p-6">
        {description === undefined ? null : (
          <Dialog.Description className="max-w-prose text-pretty text-kumo-subtle">
            {description}
          </Dialog.Description>
        )}
        {children}
      </div>
      {top ? <ScrollEdge side="top" /> : null}
      {bottom ? <ScrollEdge side="bottom" /> : null}
    </div>
  );
}

function ScrollEdge({ side }: { readonly side: "top" | "bottom" }): ReactElement {
  return (
    <div
      aria-hidden
      className={cn(
        "pointer-events-none absolute inset-x-0 h-4 from-kumo-contrast/12 to-transparent",
        side === "top" ? "top-0 bg-linear-to-b" : "bottom-0 bg-linear-to-t",
      )}
    />
  );
}

/** A scroll container is within a pixel of an edge often enough to treat that as being at it. */
const edgeSlack = 1;

interface ScrollEdges {
  readonly ref: (node: HTMLDivElement | null) => void;
  /** Whether content is hidden above and below what the container shows. */
  readonly top: boolean;
  readonly bottom: boolean;
}

function useScrollEdges(): ScrollEdges {
  const [node, setNode] = useState<HTMLDivElement | null>(null);
  const [edges, setEdges] = useState({ top: false, bottom: false });

  useEffect((): (() => void) => {
    const measure = (): void => {
      if (node !== null) {
        setEdges({
          top: node.scrollTop > edgeSlack,
          bottom: node.scrollTop + node.clientHeight < node.scrollHeight - edgeSlack,
        });
      }
    };

    // The body grows and shrinks with the form inside it, so measuring on scroll alone would miss a
    // section opening up.
    const observer = new ResizeObserver(measure);

    if (node !== null) {
      observer.observe(node);

      for (const child of node.children) {
        observer.observe(child);
      }

      node.addEventListener("scroll", measure, { passive: true });
    }

    return () => {
      observer.disconnect();
      node?.removeEventListener("scroll", measure);
    };
  }, [node]);

  return { ref: setNode, top: edges.top, bottom: edges.bottom };
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
