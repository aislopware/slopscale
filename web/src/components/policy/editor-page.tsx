import { Button } from "@cloudflare/kumo/components/button";
import { TrashIcon } from "@phosphor-icons/react";
import { useId, useState } from "react";
import type { ReactElement, ReactNode, SubmitEvent } from "react";

import { LeaveGuard } from "~/components/policy/leave-guard.tsx";
import { Callout } from "~/components/ui/callout.tsx";
import { FullBleed } from "~/components/ui/page.tsx";

/**
 * The save bar at the foot of a full-page editor, as the dashboard's policy and posture check
 * editors have it: a line across the page, the canvas behind a light blur, and the actions pinned
 * to the bottom of the window while the form scrolls under them. The primary action sits at the
 * right with Cancel before it; `start` holds what goes on the left, such as Delete on an existing
 * record. It spans the page edge to edge and lines its content up with the page's 1200px column.
 */
export function EditorFooter({
  start,
  children,
}: {
  readonly start?: ReactNode;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <FullBleed className="sticky bottom-0 z-10 -mb-8 border-t border-kumo-line bg-kumo-canvas/95 backdrop-blur-sm">
      <div className="grid grid-cols-[minmax(var(--page-padding-x),1fr)_minmax(0,var(--page-max-width))_minmax(var(--page-padding-x),1fr)] py-4">
        <div className="col-start-2 flex flex-wrap items-center gap-2">
          {start}
          <div className="ms-auto flex flex-wrap items-center gap-2">{children}</div>
        </div>
      </div>
    </FullBleed>
  );
}

/**
 * The server's answer to a save that failed, held under the top bar while the form scrolls, the way
 * the dashboard's full-page forms keep it in view. The editor clears it on the next edit, so it
 * never outlives the draft it was about.
 */
export function EditorBanner({
  title = "Not saved",
  message,
}: {
  readonly title?: string | undefined;
  readonly message: string | undefined;
}): ReactElement | null {
  if (message === undefined || message === "") {
    return null;
  }

  return (
    <div
      role="alert"
      className="sticky top-[calc(var(--app-header-height,0px)+1rem)] z-10 rounded-lg bg-kumo-base shadow-lg"
    >
      <Callout tone="error" title={title} description={message} />
    </div>
  );
}

/** Two fields side by side from the small breakpoint up, as the dashboard's Details cards lay them. */
export function FieldGrid({ children }: { readonly children: ReactNode }): ReactElement {
  return <div className="grid items-start gap-4 sm:grid-cols-2">{children}</div>;
}

/** The draft of a full-page editor and what the page needs to know about it. */
export interface DraftState<Draft> {
  readonly draft: Draft;
  /** Whether the draft differs from what the page opened with. */
  readonly dirty: boolean;
  /** Whether a save was tried, after which each field says what is wrong with it. */
  readonly submitted: boolean;
  readonly update: (patch: Partial<Draft>) => void;
  readonly markSubmitted: () => void;
}

/**
 * Holds an editor's draft against the record it opened with. An edit clears the last failed save,
 * so the banner never speaks about a draft that is gone.
 */
export function useDraft<Draft extends object>(
  initial: Draft,
  mutation: { readonly isError: boolean; readonly reset: () => void },
): DraftState<Draft> {
  const [draft, setDraft] = useState<Draft>(initial);
  const [submitted, setSubmitted] = useState(false);

  return {
    draft,
    dirty: JSON.stringify(draft) !== JSON.stringify(initial),
    submitted,
    update: (patch) => {
      setDraft((current) => ({ ...current, ...patch }));

      if (mutation.isError) {
        mutation.reset();
      }
    },
    markSubmitted: () => {
      setSubmitted(true);
    },
  };
}

const focusableSelector = "input, textarea, [contenteditable='true'], button";

/**
 * Moves to the first field that says what is wrong, so a long form never fails out of sight. Every
 * field with an error carries `aria-invalid` (`fieldError`, the pickers, the expression editor), so
 * that is what is looked for. A message that belongs to no single field (a whole condition group
 * left empty) is found by its danger colour as a last resort, and the first control beside it takes
 * the focus.
 */
function focusFirstError(form: HTMLFormElement): void {
  requestAnimationFrame(() => {
    const invalid = form.querySelector<HTMLElement>("[aria-invalid='true']");

    if (invalid !== null) {
      invalid.scrollIntoView({ block: "center" });
      invalid.focus({ preventScroll: true });

      return;
    }

    const message = form.querySelector<HTMLElement>(".text-kumo-danger");

    message?.scrollIntoView({ block: "center" });
    message?.parentElement?.querySelector<HTMLElement>(focusableSelector)?.focus({
      preventScroll: true,
    });
  });
}

export interface EditorPageProps {
  /** The record kind in lower case, as the buttons and the read-only note name it. */
  readonly noun: string;
  /** Whether the page creates a record rather than editing one. */
  readonly creating: boolean;
  readonly canEdit: boolean;
  /** The server's refusal of the last save, if it refused. */
  readonly error: string | undefined;
  readonly pending: boolean;
  readonly dirty: boolean;
  /** Saves the draft, or answers false when a field is wrong so the page moves to it. */
  readonly onSubmit: () => boolean;
  readonly onCancel: () => void;
  /** Opens the delete dialog; the button shows only on an existing record the role may change. */
  readonly onDelete: () => void;
  /** The form's sections. */
  readonly children: ReactNode;
  /** The delete dialog, kept mounted beside the form. */
  readonly dialog?: ReactNode;
  readonly leaveDescription?: string;
}

/**
 * A record on a page of its own, as the dashboard's full-page editors are: the failed save in a
 * banner that stays in view, a note when the role may only look, the sections of the form, and the
 * save bar with Delete on the left and Cancel and the save on the right. Leaving with unsaved edits
 * asks first.
 */
export function EditorPage({
  noun,
  creating,
  canEdit,
  error,
  pending,
  dirty,
  onSubmit,
  onCancel,
  onDelete,
  children,
  dialog,
  leaveDescription,
}: EditorPageProps): ReactElement {
  const formId = useId();

  const submit = (event: SubmitEvent<HTMLFormElement>): void => {
    event.preventDefault();

    if (!onSubmit()) {
      focusFirstError(event.currentTarget);
    }
  };

  return (
    <>
      <EditorBanner message={error} />
      {canEdit ? null : (
        <Callout
          title="Read only"
          description={`Your role can see this ${noun} but not change it.`}
        />
      )}
      <form id={formId} noValidate onSubmit={submit} className="flex min-w-0 flex-col gap-7">
        {children}
      </form>
      <EditorFooter
        start={
          creating || !canEdit ? null : (
            <Button variant="destructive" icon={TrashIcon} onClick={onDelete}>
              Delete {noun}
            </Button>
          )
        }
      >
        <Button variant="secondary" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" form={formId} variant="primary" disabled={!canEdit} loading={pending}>
          {creating ? `Create ${noun}` : `Save ${noun}`}
        </Button>
      </EditorFooter>
      {dialog}
      <LeaveGuard
        dirty={dirty && canEdit}
        {...(leaveDescription === undefined ? {} : { description: leaveDescription })}
      />
    </>
  );
}
