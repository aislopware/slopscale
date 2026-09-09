import { Banner } from "@cloudflare/kumo/components/banner";
import { Button } from "@cloudflare/kumo/components/button";
import { Dialog } from "@cloudflare/kumo/components/dialog";
import { cn } from "@cloudflare/kumo/utils";
import { XIcon } from "@phosphor-icons/react";
import { createContext, use, useEffect, useState } from "react";
import type { ComponentProps, ReactElement, ReactNode } from "react";
import { createPortal } from "react-dom";

export const DialogRoot = Dialog.Root;
export const DialogTrigger = Dialog.Trigger;
export const DialogClose = Dialog.Close;

/**
 * The dialog is a Frame: Kumo's dialog surface is the tinted band, with the 4px inset and the
 * 12px/8px radii of a Frame. Kumo pins it 2rem (4rem from sm) below the top edge with no height
 * limit, so a long form on a short screen would run off the bottom; the panel's body scrolls
 * instead, and the dialog stops the same distance from the bottom edge as it starts from the top.
 */
export const dialogFrameClass =
  "flex max-h-[calc(100svh-4rem)] flex-col bg-kumo-elevated p-1 ring-kumo-hairline sm:max-h-[calc(100svh-8rem)]";

/** The inset panel: the header and the body, on the band. */
export const dialogPanelClass =
  "flex min-h-0 flex-col overflow-hidden rounded-lg bg-kumo-base shadow-xs ring ring-kumo-line";

export const dialogHeaderClass =
  "flex items-center justify-between gap-4 border-b border-kumo-line px-6 py-4";

/**
 * The actions, on the band under the panel, the way a Frame's band carries what belongs to the
 * panel but is not its content. px-5 plus the band's 4px puts the last button's edge under the edge
 * of the panel's text.
 */
export const dialogActionsClass = "flex justify-end gap-3 px-5 pt-3 pb-2";

export interface DialogContentProps {
  readonly title: string;
  readonly description?: string | undefined;
  readonly size?: "sm" | "base" | "lg" | "xl";
  readonly className?: string;
  readonly children: ReactNode;
}

/** Where a {@link DialogFooter} lands: the band under the panel, once it exists. */
const FooterSlotContext = createContext<HTMLDivElement | null>(null);

/**
 * Kumo's dialog framed: the header and the body on an inset panel, the {@link DialogFooter} on the
 * band under it. Always render it, driven by `open` on the root, so the open/close animation
 * plays.
 */
export function DialogContent({
  title,
  description,
  size = "base",
  className,
  children,
}: DialogContentProps): ReactElement {
  const [slot, setSlot] = useState<HTMLDivElement | null>(null);

  return (
    <Dialog size={size} className={cn(dialogFrameClass, className)}>
      <div className={dialogPanelClass}>
        <div className={dialogHeaderClass}>
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
        <DialogBody description={description}>
          <FooterSlotContext value={slot}>{children}</FooterSlotContext>
        </DialogBody>
      </div>
      {/* The footer is written inside the body and lands here through a portal. */}
      <div ref={setSlot} className="empty:hidden" />
    </Dialog>
  );
}

/**
 * The scrolling part of a dialog. When the form is taller than the screen, a hairline shadow under
 * the header and above the panel's bottom edge says so, because a body clipped flush against an
 * edge looks like the end of the form.
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

/**
 * Submits the form the anchor belongs to when a submit button on the band is clicked. The button
 * left its form when it moved to the band, so the browser would not submit for it.
 */
function useSubmitFromBand(actions: HTMLDivElement | null, anchor: HTMLButtonElement | null): void {
  useEffect((): (() => void) | undefined => {
    if (actions === null) {
      return undefined;
    }

    const onClick = (event: MouseEvent): void => {
      const button =
        event.target instanceof Element ? event.target.closest("button[type=submit]") : null;

      if (button !== null && anchor?.form) {
        anchor.form.requestSubmit();
      }
    };

    actions.addEventListener("click", onClick);

    return () => {
      actions.removeEventListener("click", onClick);
    };
  }, [actions, anchor]);
}

/**
 * The actions of a dialog. It is written inside the body, at the end of the form when there is one,
 * and shows on the band under the panel. A hidden submit button stays where it was written: as the
 * form's first submit button it is what Enter presses, and it says which form the buttons on the
 * band submit.
 */
export function DialogFooter({
  className,
  children,
  ...props
}: ComponentProps<"div">): ReactElement {
  const slot = use(FooterSlotContext);
  const [anchor, setAnchor] = useState<HTMLButtonElement | null>(null);
  const [actions, setActions] = useState<HTMLDivElement | null>(null);

  useSubmitFromBand(actions, anchor);

  return (
    <>
      <button ref={setAnchor} type="submit" hidden tabIndex={-1} aria-hidden />
      {slot === null
        ? null
        : createPortal(
            <div ref={setActions} className={cn(dialogActionsClass, className)} {...props}>
              {children}
            </div>,
            slot,
          )}
    </>
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
