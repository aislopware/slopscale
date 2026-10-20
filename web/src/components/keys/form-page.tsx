import { Banner } from "@cloudflare/kumo/components/banner";
import { LinkButton } from "@cloudflare/kumo/components/button";
import { Radio } from "@cloudflare/kumo/primitives/radio";
import { RadioGroup } from "@cloudflare/kumo/primitives/radio-group";
import { cn } from "@cloudflare/kumo/utils";
import type { Icon } from "@phosphor-icons/react";
import { ArrowLeftIcon, CheckIcon, InfoIcon } from "@phosphor-icons/react";
import { useId } from "react";
import type { ReactElement, ReactNode } from "react";

import { HoverPopover } from "~/components/ui/hover-popover.tsx";

/**
 * One part of a create page, as the dashboard's "Create API token" page builds it: Kumo's layered
 * card, its 56px grey band carrying the part's name in 14px medium subtle text with an info icon
 * that holds the longer explanation, and the white panel with the fields resting in it. The cards
 * stack 16px apart. `flush` drops the panel's padding for rows that draw their own (settings
 * rows).
 */
export function FormSection({
  title,
  info,
  flush = false,
  children,
}: {
  readonly title: string;
  /** What the part decides, one hover or focus away on the info icon. */
  readonly info?: ReactNode;
  readonly flush?: boolean;
  readonly children: ReactNode;
}): ReactElement {
  const headingId = useId();

  return (
    <section
      aria-labelledby={headingId}
      className="flex min-w-0 flex-col rounded-lg bg-kumo-elevated ring ring-kumo-hairline"
    >
      <div className="flex h-14 items-center gap-2 p-4 font-medium text-kumo-subtle">
        <h2 id={headingId}>{title}</h2>
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

/**
 * The end of a create page: a line across, then the way back on the left (secondary, with a leading
 * arrow) and the page's one action on the right, the dashboard's "← Cancel … Review token".
 */
export function FormFooter({
  cancelTo,
  cancelLabel = "Cancel",
  children,
}: {
  /** The list the page was opened from; absent once there is nothing left to cancel. */
  readonly cancelTo?: string;
  readonly cancelLabel?: string;
  /** The primary action, usually the form's submit button. */
  readonly children: ReactNode;
}): ReactElement {
  return (
    <div className="mt-4 flex flex-wrap items-center justify-between gap-3 border-t border-kumo-line pt-8">
      {cancelTo === undefined ? (
        <span />
      ) : (
        <LinkButton href={cancelTo} variant="secondary" icon={ArrowLeftIcon}>
          {cancelLabel}
        </LinkButton>
      )}
      <div className="ms-auto flex items-center gap-2">{children}</div>
    </div>
  );
}

/**
 * The request's error over the page, as the dashboard's full-page forms show one: the danger banner
 * held under the top bar so it is in view wherever the submit button was, in plain words, gone
 * again on the next edit.
 */
export function FormError({ message }: { readonly message: string | undefined }): ReactNode {
  if (message === undefined || message === "") {
    return null;
  }

  return (
    <div className="sticky top-[calc(var(--app-header-height,0px)+15px)] z-5 rounded-lg bg-kumo-base shadow-lg">
      <Banner variant="error" title="The key was not created" description={message} />
    </div>
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
