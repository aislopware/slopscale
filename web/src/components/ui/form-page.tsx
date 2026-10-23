import { Banner } from "@cloudflare/kumo/components/banner";
import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { Radio } from "@cloudflare/kumo/primitives/radio";
import { RadioGroup } from "@cloudflare/kumo/primitives/radio-group";
import { cn } from "@cloudflare/kumo/utils";
import type { Icon } from "@phosphor-icons/react";
import { CheckIcon, InfoIcon, TrashIcon } from "@phosphor-icons/react";
import { useId, useState } from "react";
import type { ReactElement, ReactNode, SubmitEvent } from "react";

import { Callout } from "~/components/ui/callout.tsx";
import { HoverPopover } from "~/components/ui/hover-popover.tsx";
import { LeaveGuard } from "~/components/ui/leave-guard.tsx";
import { FullBleed, pageGridClass } from "~/components/ui/page.tsx";

/**
 * The column a full-page form sits in, as the dashboard sizes its create pages: 576px for a form of
 * one or two fields ("Create a Tunnel"), 768px for an ordinary form ("Create API token") and 896px
 * for a rule builder ("New custom rule"). The page header, the form and the save bar all take the
 * column, centred in the page, so the title lines up with the fields under it.
 */
const formWidthClass = {
  narrow: "[--page-max-width:576px]!",
  form: "[--page-max-width:768px]!",
  wide: "[--page-max-width:896px]!",
} as const;

export type FormWidth = keyof typeof formWidthClass;

/**
 * Lays a form page out on a narrower page grid. It takes the whole page, so a route renders it as
 * its only block with the `PageHeader` and the form inside.
 */
export function FormLayout({
  width = "form",
  children,
}: {
  readonly width?: FormWidth;
  readonly children: ReactNode;
}): ReactElement {
  // -my-8 takes back the shell grid's own padding; the inner grid puts it back around the form.
  return (
    <div data-full-bleed className={cn(pageGridClass, "-my-8", formWidthClass[width])}>
      {children}
    </div>
  );
}

/**
 * One part of a create page, as the dashboard's "Create API token" page builds it: Kumo's layered
 * card, its grey band carrying the part's name in 14px medium subtle text with an info icon that
 * holds the longer explanation, and the white panel with the fields resting in it. The cards stack
 * 16px apart. `flush` drops the panel's padding for rows that draw their own (settings rows), and
 * `actions` puts small buttons on the band's right.
 */
export function FormSection({
  title,
  info,
  optional = false,
  actions,
  flush = false,
  children,
}: {
  readonly title: string;
  /** What the part decides, one hover or focus away on the info icon. */
  readonly info?: ReactNode;
  /** Says "(optional)" after the title, the dashboard's mark for a part that may stay empty. */
  readonly optional?: boolean;
  readonly actions?: ReactNode;
  readonly flush?: boolean;
  readonly children: ReactNode;
}): ReactElement {
  const headingId = useId();

  return (
    <section
      aria-labelledby={headingId}
      className="flex min-w-0 flex-col rounded-lg bg-kumo-elevated ring ring-kumo-hairline"
    >
      <div className="flex min-h-13 items-center gap-2 px-4 py-2 font-medium text-kumo-subtle">
        <h2 id={headingId}>
          {title}
          {optional ? <span className="font-normal"> (optional)</span> : null}
        </h2>
        {info === undefined ? null : (
          <HoverPopover
            title={title}
            detail={info}
            triggerLabel={`About ${title.toLowerCase()}`}
            triggerClassName="inline-flex rounded-full text-kumo-subtle outline-none hover:text-kumo-default focus-visible:ring-2 focus-visible:ring-kumo-focus"
          >
            <InfoIcon aria-hidden className="size-4" />
          </HoverPopover>
        )}
        {actions === undefined ? null : (
          <div className="ms-auto flex shrink-0 items-center gap-2">{actions}</div>
        )}
      </div>
      <div
        className={cn(
          "min-w-0 overflow-clip rounded-lg bg-kumo-base shadow-xs ring ring-kumo-line",
          !flush && "flex flex-col gap-4 p-4",
        )}
      >
        {children}
      </div>
    </section>
  );
}

/** Two fields side by side from the small breakpoint up, as the dashboard's Details cards lay them. */
export function FieldGrid({ children }: { readonly children: ReactNode }): ReactElement {
  return <div className="grid items-start gap-4 sm:grid-cols-2">{children}</div>;
}

/** The width the dashboard caps a free-text name field at, even in a wide card: 448px. */
export const nameFieldClass = "w-full max-w-md";

/**
 * The request's error over the page, as the dashboard's full-page forms show one: the danger banner
 * held under the top bar so it is in view wherever the submit button was, in plain words, gone
 * again on the next edit.
 */
export function FormError({
  title = "Not saved",
  message,
}: {
  /** What did not happen, in the banner's title: "The key was not created". */
  readonly title?: string | undefined;
  readonly message: string | undefined;
}): ReactNode {
  if (message === undefined || message === "") {
    return null;
  }

  return (
    <div
      role="alert"
      className="sticky top-[calc(var(--app-header-height,0px)+1rem)] z-10 rounded-lg bg-kumo-base shadow-lg"
    >
      <Banner variant="error" title={title} description={message} />
    </div>
  );
}

/**
 * The save bar at the foot of a full-page form, as the dashboard's long Zero Trust forms have it: a
 * line across the page, the canvas behind a light blur, and the actions pinned to the bottom of the
 * window while the form scrolls under them; on a short form it simply ends the page. The primary
 * action sits at the right with Cancel before it; `start` holds what goes on the left, such as
 * Delete on an existing record. It spans the page edge to edge and lines its content up with the
 * page's column.
 */
export function FormBar({
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

/** The draft of a full-page form and what the page needs to know about it. */
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
 * Holds a form's draft against the record it opened with. An edit clears the last failed save, so
 * the banner never speaks about a draft that is gone.
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
      invalid.scrollIntoView({ block: "center", behavior: "smooth" });
      invalid.focus({ preventScroll: true });

      return;
    }

    const message = form.querySelector<HTMLElement>(".text-kumo-danger");

    message?.scrollIntoView({ block: "center", behavior: "smooth" });
    message?.parentElement?.querySelector<HTMLElement>(focusableSelector)?.focus({
      preventScroll: true,
    });
  });
}

export interface FormPageProps {
  /** The record kind in lower case, as the buttons and the read-only note name it. */
  readonly noun: string;
  /** Whether the page creates a record rather than editing one. */
  readonly creating: boolean;
  readonly canEdit: boolean;
  /** The server's refusal of the last save, if it refused. */
  readonly error: string | undefined;
  /** The banner's title over that refusal; "Not saved" when absent. */
  readonly errorTitle?: string;
  readonly pending: boolean;
  readonly dirty: boolean;
  /** Saves the draft, or answers false when a field is wrong so the page moves to it. */
  readonly onSubmit: () => boolean;
  /** Where Cancel goes: the list the page was opened from. */
  readonly cancelTo: string;
  /** Opens the delete dialog; the button shows only on an existing record the role may change. */
  readonly onDelete?: (() => void) | undefined;
  /** The primary button's words when "Create <noun>" / "Save <noun>" do not fit. */
  readonly submitLabel?: string;
  /** Buttons before Cancel, such as "Test connection". */
  readonly extraActions?: ReactNode;
  /** The form's sections. */
  readonly children: ReactNode;
  /** Dialogs kept mounted beside the form, such as the delete confirmation. */
  readonly dialog?: ReactNode;
  readonly leaveDescription?: string;
}

/**
 * A record on a page of its own, as the dashboard's full-page forms are: the failed save in a
 * banner that stays in view, a note when the role may only look, the sections of the form, and the
 * save bar with Delete on the left and Cancel and the save on the right. Save stays enabled on a
 * clean form, as the dashboard's are; a save that a field stops scrolls to that field. Leaving with
 * unsaved edits asks first.
 */
export function FormPage({
  noun,
  creating,
  canEdit,
  error,
  errorTitle,
  pending,
  dirty,
  onSubmit,
  cancelTo,
  onDelete,
  submitLabel,
  extraActions,
  children,
  dialog,
  leaveDescription,
}: FormPageProps): ReactElement {
  const formId = useId();

  const submit = (event: SubmitEvent<HTMLFormElement>): void => {
    event.preventDefault();

    if (pending) {
      return;
    }

    if (!onSubmit()) {
      focusFirstError(event.currentTarget);
    }
  };

  return (
    <>
      <FormError title={errorTitle} message={error} />
      {canEdit ? null : (
        <Callout
          title="Read only"
          description={`Your role can see this ${noun} but not change it.`}
        />
      )}
      <form id={formId} noValidate onSubmit={submit} className="flex min-w-0 flex-col gap-4">
        {children}
      </form>
      <FormBar
        start={
          creating || !canEdit || onDelete === undefined ? null : (
            <Button variant="secondary-destructive" icon={TrashIcon} onClick={onDelete}>
              Delete {noun}
            </Button>
          )
        }
      >
        {extraActions}
        <LinkButton href={cancelTo} variant="secondary">
          Cancel
        </LinkButton>
        <Button type="submit" form={formId} variant="primary" disabled={!canEdit} loading={pending}>
          {submitLabel ?? (creating ? `Create ${noun}` : `Save ${noun}`)}
        </Button>
      </FormBar>
      {dialog}
      <LeaveGuard
        dirty={dirty && canEdit}
        {...(leaveDescription === undefined ? {} : { description: leaveDescription })}
      />
    </>
  );
}

export interface SelectionCard<Value extends string> {
  readonly value: Value;
  readonly title: string;
  readonly description: string;
  readonly icon: Icon;
  /** A word beside the title, such as "Custom". */
  readonly badge?: string;
}

/**
 * A choice between a few kinds of thing, as the dashboard's permission templates: a 2-column grid
 * of 60px white cards, each a 36px ringed icon tile, a 13px medium title and a 12px subtle line.
 * They are one radio group, so arrow keys move between them; the chosen card takes the tint fill, a
 * brand ring and a check.
 */
export function SelectionCards<Value extends string>({
  label,
  value,
  cards,
  onValueChange,
}: {
  /** The group's accessible name. */
  readonly label: string;
  readonly value: Value;
  readonly cards: readonly SelectionCard<Value>[];
  readonly onValueChange: (value: Value) => void;
}): ReactElement {
  return (
    <RadioGroup
      aria-label={label}
      value={value}
      onValueChange={(next: unknown) => {
        const card = cards.find((candidate) => candidate.value === next);

        if (card !== undefined) {
          onValueChange(card.value);
        }
      }}
      className="grid gap-4 md:grid-cols-2"
    >
      {cards.map((card) => (
        <Radio.Root
          key={card.value}
          value={card.value}
          className={cn(
            "group/card flex min-h-15 cursor-pointer items-center gap-3 rounded-lg bg-kumo-base px-3 py-2.5 text-left shadow-xs ring ring-kumo-line outline-none",
            "hover:bg-kumo-tint focus-visible:ring-2 focus-visible:ring-kumo-focus",
            "data-checked:bg-kumo-tint data-checked:ring-kumo-brand",
          )}
        >
          <span className="flex size-9 shrink-0 items-center justify-center rounded-md bg-kumo-base text-kumo-subtle ring-1 ring-kumo-line">
            <card.icon aria-hidden className="size-4" />
          </span>
          <span className="flex min-w-0 flex-1 flex-col gap-0.5">
            <span className="flex items-center gap-2 text-sm font-medium text-kumo-default">
              {card.title}
              {card.badge === undefined ? null : (
                <span className="rounded-full bg-kumo-info-tint px-2 py-0.5 text-xs text-kumo-info">
                  {card.badge}
                </span>
              )}
            </span>
            <span className="text-xs text-kumo-subtle">{card.description}</span>
          </span>
          <CheckIcon
            aria-hidden
            weight="bold"
            className="size-4 shrink-0 text-kumo-link opacity-0 group-data-checked/card:opacity-100"
          />
        </Radio.Root>
      ))}
    </RadioGroup>
  );
}

/**
 * A short choice as a row of 36px chips, the dashboard's token expiration: each a ringed button,
 * the chosen one in the tint fill. One radio group, so arrow keys move between them.
 */
export function ChoiceChips<Value extends string>({
  label,
  value,
  options,
  onValueChange,
}: {
  readonly label: string;
  readonly value: Value;
  readonly options: readonly { readonly value: Value; readonly label: string }[];
  readonly onValueChange: (value: Value) => void;
}): ReactElement {
  return (
    <RadioGroup
      aria-label={label}
      value={value}
      onValueChange={(next: unknown) => {
        const option = options.find((candidate) => candidate.value === next);

        if (option !== undefined) {
          onValueChange(option.value);
        }
      }}
      className="flex flex-wrap gap-1.5"
    >
      {options.map((option) => (
        <Radio.Root
          key={option.value}
          value={option.value}
          className={cn(
            "inline-flex h-9 cursor-pointer items-center rounded-lg bg-transparent px-3 font-medium text-kumo-default ring ring-kumo-line outline-none",
            "hover:bg-kumo-tint focus-visible:ring-2 focus-visible:ring-kumo-focus",
            "data-checked:bg-kumo-tint data-checked:ring-kumo-hairline",
          )}
        >
          {option.label}
        </Radio.Root>
      ))}
    </RadioGroup>
  );
}
