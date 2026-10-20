import { Badge } from "@cloudflare/kumo/components/badge";
import { Button } from "@cloudflare/kumo/components/button";
import { Switch } from "@cloudflare/kumo/components/switch";
import { cn } from "@cloudflare/kumo/utils";
import type { Icon } from "@phosphor-icons/react";
import { ArrowRightIcon, PlusIcon, TrashIcon } from "@phosphor-icons/react";
import { Fragment, useId } from "react";
import type { ReactElement, ReactNode } from "react";

/**
 * The pieces of the dashboard's rule builders (Gateway firewall policies, Access policies), for the
 * console's own editors: purple If / Then / OR badges over the columns, condition slots with a line
 * down their left, tinted condition rows of a selector, an operator and a value, a blue "And"
 * between rows, an empty slot that offers a condition, and the three-column preview.
 */

/** The logic words the builders draw as small solid pills. */
export type Logic = "if" | "then" | "or" | "and" | "not";

interface LogicStyle {
  readonly label: string;
  readonly variant: "purple" | "blue" | "neutral";
}

/**
 * Purple for the clauses (If, Then, OR), blue for the join between rows, grey for an exclusion. A
 * map rather than an object literal, because an object with a `then` key reads as a promise.
 */
const logicStyles = new Map<Logic, LogicStyle>([
  ["if", { label: "If", variant: "purple" }],
  ["then", { label: "Then", variant: "purple" }],
  ["or", { label: "OR", variant: "purple" }],
  ["and", { label: "And", variant: "blue" }],
  ["not", { label: "NOT", variant: "neutral" }],
]);

export function LogicBadge({ logic }: { readonly logic: Logic }): ReactElement {
  const style = logicStyles.get(logic) ?? { label: logic, variant: "neutral" };
  return <Badge variant={style.variant}>{style.label}</Badge>;
}

/** The heading of a builder column: its logic badge, then a 16px title. */
export function BuilderHeading({
  logic,
  title,
  description,
}: {
  readonly logic: Logic;
  readonly title: string;
  readonly description?: ReactNode;
}): ReactElement {
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center gap-2">
        <LogicBadge logic={logic} />
        <h2 className="text-lg font-semibold text-kumo-default">{title}</h2>
      </div>
      {description === undefined ? null : (
        <p className="text-sm text-pretty text-kumo-subtle">{description}</p>
      )}
    </div>
  );
}

/**
 * One kind of condition ("And traffic matches…"): its 14px heading, then its rows behind a line on
 * the left that ties them together.
 */
export function ConditionSlot({
  title,
  description,
  children,
}: {
  readonly title: string;
  readonly description?: ReactNode;
  readonly children: ReactNode;
}): ReactElement {
  const id = useId();

  return (
    <section aria-labelledby={id} className="flex flex-col gap-3">
      <div className="flex flex-col gap-0.5">
        <h3 id={id} className="font-medium text-kumo-default">
          {title}
        </h3>
        {description === undefined ? null : (
          <p className="text-sm text-pretty text-kumo-subtle">{description}</p>
        )}
      </div>
      <div className="flex flex-col gap-3 border-l border-kumo-line pl-4">{children}</div>
    </section>
  );
}

/**
 * A condition's label as the builder writes it: the selector in the field's own weight and the
 * operator after it, stepped back ("Destination group is in"). It goes in the value control's
 * label, so the control is named by what it matches.
 */
export function conditionLabel(selector: string, operator: string): ReactNode {
  return (
    <span className="inline-flex flex-wrap items-baseline gap-x-1.5">
      <span>{selector}</span>
      <span className="font-normal text-kumo-subtle">{operator}</span>
    </span>
  );
}

const removeIconSize = 16;

/**
 * One condition: a tinted box holding the value control under its selector and operator, with a
 * remove button at its top right when the condition is optional.
 */
export function ConditionRow({
  children,
  onRemove,
  removeLabel = "Remove condition",
  disabled = false,
}: {
  readonly children: ReactNode;
  readonly onRemove?: () => void;
  readonly removeLabel?: string;
  readonly disabled?: boolean;
}): ReactElement {
  return (
    <div
      className={cn("relative rounded-md bg-kumo-tint p-3", onRemove === undefined ? "" : "pr-12")}
    >
      {children}
      {onRemove === undefined ? null : (
        <Button
          variant="ghost"
          shape="square"
          size="sm"
          className="absolute top-2 right-2"
          icon={<TrashIcon size={removeIconSize} />}
          aria-label={removeLabel}
          disabled={disabled}
          onClick={onRemove}
        />
      )}
    </div>
  );
}

/** The blue "And" between two conditions of a slot: every one of them must hold. */
export function AndJoint(): ReactElement {
  return (
    <span className="flex self-start">
      <LogicBadge logic="and" />
    </span>
  );
}

/** A condition of a {@link ConditionList}, keyed by what it matches. */
export interface ListedCondition {
  readonly key: string;
  readonly row: ReactNode;
}

/** Conditions joined by {@link AndJoint}s; a slot with one row draws none. */
export function ConditionList({
  conditions,
}: {
  readonly conditions: readonly ListedCondition[];
}): ReactElement {
  return (
    <>
      {conditions.map((condition, index) => (
        <Fragment key={condition.key}>
          {index === 0 ? null : <AndJoint />}
          {condition.row}
        </Fragment>
      ))}
    </>
  );
}

/** "+ Add condition": a link-coloured small button that adds a condition to a slot. */
export function AddCondition({
  children,
  onClick,
  disabled = false,
}: {
  readonly children: ReactNode;
  readonly onClick: () => void;
  readonly disabled?: boolean;
}): ReactElement {
  return (
    <Button
      variant="ghost"
      size="sm"
      icon={PlusIcon}
      className="text-kumo-link"
      disabled={disabled}
      onClick={onClick}
    >
      {children}
    </Button>
  );
}

/**
 * A slot with no condition in it: a ringed box saying so in subtle text, and the way to add one
 * under it.
 */
export function NoConditions({
  children = "No conditions",
  action,
}: {
  readonly children?: ReactNode;
  readonly action?: ReactNode;
}): ReactElement {
  return (
    <div className="flex flex-col items-center gap-1 rounded-lg px-4 py-6 text-center text-sm text-kumo-subtle ring ring-kumo-line">
      <span className="text-pretty">{children}</span>
      {action}
    </div>
  );
}

/**
 * A setting of the Then column: its title and what it does on the left, a Switch on the right, and
 * what the setting needs once it is on under them.
 */
export function SettingToggle({
  title,
  description,
  checked,
  disabled = false,
  onCheckedChange,
  children,
}: {
  readonly title: string;
  readonly description: ReactNode;
  readonly checked: boolean;
  readonly disabled?: boolean;
  readonly onCheckedChange: (checked: boolean) => void;
  readonly children?: ReactNode;
}): ReactElement {
  const id = useId();

  return (
    <div className="flex flex-col gap-3 border-t border-kumo-line pt-4 first:border-t-0 first:pt-0">
      <div className="flex items-start justify-between gap-4 sm:gap-6">
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span id={`${id}-title`} className="font-medium text-kumo-default">
            {title}
          </span>
          <span id={`${id}-description`} className="text-sm text-pretty text-kumo-subtle">
            {description}
          </span>
        </div>
        {/* A long description squeezed the switch into a square at a phone's width. */}
        <span className="flex min-h-lh shrink-0 items-center">
          <Switch
            checked={checked}
            disabled={disabled}
            aria-labelledby={`${id}-title`}
            aria-describedby={`${id}-description`}
            onCheckedChange={onCheckedChange}
          />
        </span>
      </div>
      {children}
    </div>
  );
}

/** One column of the preview: a 12px subtle heading over its cards. */
export interface PreviewColumn {
  readonly title: string;
  readonly items: readonly PreviewItem[];
}

export interface PreviewItem {
  readonly key: string;
  readonly icon: Icon;
  /** A word or two above the content, such as "Traffic conditions". */
  readonly label?: string;
  readonly content: ReactNode;
}

const previewIconSize = 16;
const arrowIconSize = 12;

/**
 * The live preview under a builder: what the rule takes in, what it matches and what it does, as
 * three columns of small cards with an arrow between them.
 */
export function RulePreview({
  columns,
}: {
  readonly columns: readonly PreviewColumn[];
}): ReactElement {
  return (
    <div className="grid gap-4 p-4 lg:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)_auto_minmax(0,1fr)]">
      {columns.map((column, index) => (
        <Fragment key={column.title}>
          {index === 0 ? null : (
            <div
              aria-hidden
              className="hidden w-5 items-center justify-center rounded-sm bg-kumo-info-tint text-kumo-link lg:flex"
            >
              <ArrowRightIcon size={arrowIconSize} weight="bold" />
            </div>
          )}
          <div className="flex min-w-0 flex-col gap-2">
            <h3 className="text-xs font-medium text-kumo-subtle">{column.title}</h3>
            {column.items.map((item) => (
              <div
                key={item.key}
                className="flex min-w-0 items-start gap-2.5 rounded-lg bg-kumo-base px-3 py-2.5 shadow-xs ring ring-kumo-line"
              >
                <span className="flex h-lh shrink-0 items-center text-kumo-subtle">
                  <item.icon size={previewIconSize} aria-hidden />
                </span>
                <span className="flex min-w-0 flex-col gap-0.5">
                  {item.label === undefined ? null : (
                    <span className="text-xs text-kumo-subtle">{item.label}</span>
                  )}
                  <span className="min-w-0 text-pretty [overflow-wrap:anywhere]">
                    {item.content}
                  </span>
                </span>
              </div>
            ))}
          </div>
        </Fragment>
      ))}
    </div>
  );
}
