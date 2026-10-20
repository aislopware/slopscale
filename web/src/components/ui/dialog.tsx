import { Banner } from "@cloudflare/kumo/components/banner";
import { Button } from "@cloudflare/kumo/components/button";
import { Dialog } from "@cloudflare/kumo/components/dialog";
import { cn } from "@cloudflare/kumo/utils";
import { XIcon } from "@phosphor-icons/react";
import { createContext, use, useEffect, useId, useMemo, useState } from "react";
import type { ComponentProps, ReactElement, ReactNode } from "react";
import { createPortal } from "react-dom";

export const DialogRoot = Dialog.Root;
export const DialogTrigger = Dialog.Trigger;
export const DialogClose = Dialog.Close;

/**
 * Kumo's dialog as the Cloudflare dashboard's form dialog (Add DNS record): a white panel 64px from
 * the top over the light recessed wash Kumo already draws, 12px corners, 32px of padding. Kumo sets
 * no height limit, so a long form on a short screen would run off the bottom; the body scrolls
 * instead, and the dialog stops the same distance from the bottom edge as it starts from the top.
 */
export const dialogFrameClass =
  "flex max-h-[calc(100svh-4rem)] flex-col sm:max-h-[calc(100svh-8rem)]";

/** The header and the body, above the actions. */
export const dialogPanelClass = "flex min-h-0 flex-col";

/** The 24px title with the close button at its right, 32px in from the panel's edges. */
export const dialogHeaderClass = "flex items-start justify-between gap-4 px-8 pt-8 pb-2";

/**
 * The actions, under the body and outside its scroll: left-aligned, the primary first and Cancel
 * after it, each at least 96px wide, as the dashboard's form dialogs have them.
 */
export const dialogActionsClass = "flex flex-wrap justify-start gap-2 px-8 pt-4 pb-8 *:min-w-24";

export interface DialogContentProps {
  readonly title: string;
  readonly description?: ReactNode;
  readonly size?: "sm" | "base" | "lg" | "xl";
  readonly className?: string;
  readonly children: ReactNode;
}

/** Where a {@link DialogFooter} lands: the actions row under the body, once it exists. */
const FooterSlotContext = createContext<HTMLDivElement | null>(null);

/**
 * Kumo's dialog with the dashboard's form-dialog layout: the title and the close button, the body,
 * and the {@link DialogFooter} under the body where a long form cannot scroll it away. Always render
 * it, driven by `open` on the root, so the open/close animation plays.
 */
export function DialogContent({
  title,
  description,
  size = "base",
  className,
  children,
}: DialogContentProps): ReactElement {
  // The slot exists before the first render, so the footer is in the first committed tree rather
  // than one render later; the actions row adopts it on mount.
  const slot = useMemo(() => {
    const node = document.createElement("div");

    node.className = "empty:hidden";

    return node;
  }, []);

  return (
    <Dialog size={size} className={cn(dialogFrameClass, className)}>
      <div className={dialogPanelClass}>
        <div className={dialogHeaderClass}>
          <Dialog.Title className="text-2xl font-semibold text-kumo-default">{title}</Dialog.Title>
          <Dialog.Close
            render={<Button variant="secondary" shape="square" icon={XIcon} aria-label="Close" />}
          />
        </div>
        <DialogBody description={description}>
          <FooterSlotContext value={slot}>{children}</FooterSlotContext>
        </DialogBody>
      </div>
      {/* The footer is written inside the body and lands here, out of its scroll, through a portal. */}
      <div
        ref={(actions) => {
          actions?.append(slot);
        }}
      />
    </Dialog>
  );
}

/**
 * The scrolling part of a dialog. When the form is taller than the screen, a soft shadow under the
 * header and above the actions says so, because a body clipped flush against an edge looks like the
 * end of the form.
 */
function DialogBody({
  description,
  children,
}: {
  readonly description?: ReactNode;
  readonly children: ReactNode;
}): ReactElement {
  const { ref, top, bottom } = useScrollEdges();

  return (
    <div className="relative flex min-h-0 flex-col">
      <div ref={ref} className="flex min-h-0 flex-col gap-4 overflow-y-auto px-8 py-4">
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
 * Gives every submit button in the actions row the form the anchor belongs to. A button that left
 * its form when it moved to the actions row has no form owner, so the browser would not submit for
 * it; with the `form` attribute it is the form's own button again, submitter, validation and all.
 * The click listener catches a button that appeared after the effect ran, before its activation.
 */
function useFormOwner(
  actions: HTMLDivElement | null,
  anchor: HTMLButtonElement | null,
  fallbackId: string,
): void {
  useEffect((): (() => void) | undefined => {
    const form = anchor?.form ?? null;

    if (actions === null || form === null) {
      return undefined;
    }

    if (form.id === "") {
      form.setAttribute("id", fallbackId);
    }

    const adopt = (button: Element): void => {
      if (!button.hasAttribute("form")) {
        button.setAttribute("form", form.id);
      }
    };

    for (const button of actions.querySelectorAll("button[type=submit]")) {
      adopt(button);
    }

    const onClick = (event: MouseEvent): void => {
      const button =
        event.target instanceof Element ? event.target.closest("button[type=submit]") : null;

      if (button !== null) {
        adopt(button);
      }
    };

    actions.addEventListener("click", onClick, { capture: true });

    return () => {
      actions.removeEventListener("click", onClick, { capture: true });
    };
  }, [actions, anchor, fallbackId]);
}

export interface DialogFooterProps extends ComponentProps<"div"> {
  /**
   * Whether the footer's submit button is disabled, so the hidden submitter is too: a disabled
   * default button stops implicit submission, and one that is not would let Enter in a text input
   * submit a form the visible button refuses.
   */
  readonly submitDisabled?: boolean;
}

/**
 * The actions of a dialog, primary first and Cancel after it. It is written inside the body, at the
 * end of the form when there is one, and shows under the body. A hidden submit button stays where
 * it was written: as the form's first submit button it is what Enter presses, and it says which
 * form the buttons on the actions row submit.
 */
export function DialogFooter({
  className,
  children,
  submitDisabled = false,
  ...props
}: DialogFooterProps): ReactElement {
  const slot = use(FooterSlotContext);
  const [anchor, setAnchor] = useState<HTMLButtonElement | null>(null);
  const [actions, setActions] = useState<HTMLDivElement | null>(null);
  const formId = useId();

  useFormOwner(actions, anchor, formId);

  return (
    <>
      <button
        ref={setAnchor}
        type="submit"
        hidden
        tabIndex={-1}
        aria-hidden
        disabled={submitDisabled}
      />
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
