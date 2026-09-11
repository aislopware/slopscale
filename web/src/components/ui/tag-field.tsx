import { Combobox } from "@cloudflare/kumo/components/combobox";
import { useMemo, useState } from "react";
import type { ReactElement, ReactNode } from "react";

import { Tag, TagText, tagColours } from "~/components/ui/tag.tsx";
import { normalizeTag, parseTagList, splitTag, tagError } from "~/lib/tag.ts";

/** A row of the list: a tag the tailnet knows, or the one the operator is typing. */
interface TagItem {
  readonly value: string;
  /** True for the typed tag that nothing in the tailnet carries yet. */
  readonly create: boolean;
}

/** Commas and whitespace: a list pasted into the input is committed whole. */
const listSeparator = /[\s,]/u;

/** A known tag's rank against what is typed: exact, then by its start, then anywhere, then not. */
const exact = 0;
const byStart = 1;
const anywhere = 2;
const noMatch = 3;

/**
 * The rows the list offers for what is typed: the known tags that match, the closest first, then
 * the typed tag itself when it is new. Enter takes the first row, so an exact match beats a longer
 * name and a new name is only created once nothing known matches it.
 */
export function tagItems(
  suggestions: readonly string[],
  chosen: readonly string[],
  query: string,
): TagItem[] {
  const draft = normalizeTag(query);
  const needle = splitTag(draft).name;
  const open = suggestions.filter((tag) => !chosen.includes(tag));
  const rank = (tag: string): number => {
    const { name } = splitTag(tag);

    if (tag === draft) {
      return exact;
    }

    if (name.startsWith(needle)) {
      return byStart;
    }

    return name.includes(needle) ? anywhere : noMatch;
  };
  const matching = open
    .map((tag) => ({ tag, rank: rank(tag) }))
    .filter(({ rank: order }) => order !== noMatch)
    .toSorted((left, right) => left.rank - right.rank || left.tag.localeCompare(right.tag))
    .map(({ tag }) => ({ value: tag, create: false }));
  const isNew =
    draft !== "" && tagError(draft) === null && !chosen.includes(draft) && !open.includes(draft);

  return isNew ? [...matching, { value: draft, create: true }] : matching;
}

/**
 * The tags of a machine, a key or an app as chips in one field: the tailnet's tags come up as the
 * operator types, so a name is picked rather than spelled, and a name the tailnet does not carry
 * yet can still be typed and taken with Enter. A pasted list ("tag:ci, tag:prod") lands as chips at
 * once. The `tag:` prefix is added and the case folded, and a name the server would refuse says why
 * under the field instead of failing on save.
 */
export function TagField({
  label = "Tags",
  description,
  placeholder = "Add tag…",
  value,
  onValueChange,
  suggestions,
  required = false,
  disabled = false,
}: {
  readonly label?: ReactNode;
  readonly description?: ReactNode;
  readonly placeholder?: string;
  readonly value: readonly string[];
  readonly onValueChange: (tags: string[]) => void;
  /** Every tag the tailnet knows; the chosen ones are left out of the list. */
  readonly suggestions: readonly string[];
  readonly required?: boolean;
  readonly disabled?: boolean;
}): ReactElement {
  const [query, setQuery] = useState("");
  const draft = normalizeTag(query);
  const issue = draft === "" ? null : tagError(draft);
  const items = useMemo(() => tagItems(suggestions, value, query), [suggestions, value, query]);
  const selected = useMemo(() => value.map((tag) => ({ value: tag, create: false })), [value]);

  function commit(tags: readonly string[]): void {
    const next = [...value];

    for (const tag of tags) {
      if (!next.includes(tag)) {
        next.push(tag);
      }
    }

    onValueChange(next);
    setQuery("");
  }

  return (
    <Combobox<TagItem, true>
      multiple
      items={items}
      value={selected}
      disabled={disabled}
      required={required}
      label={label}
      {...(description === undefined ? {} : { description })}
      {...(issue === null ? {} : { error: issue })}
      // The rows are ranked here, so the list is not filtered again by the text.
      filter={null}
      autoHighlight
      inputValue={query}
      isItemEqualToValue={(item, chosen) => item.value === chosen.value}
      itemToStringLabel={(item) => item.value}
      onInputValueChange={(text) => {
        if (listSeparator.test(text)) {
          const pasted = parseTagList(text).filter((tag) => tagError(tag) === null);

          if (pasted.length > 0) {
            commit(pasted);

            return;
          }
        }

        setQuery(text);
      }}
      onValueChange={(next) => {
        onValueChange(next.map((item) => item.value));
        setQuery("");
      }}
    >
      <Combobox.TriggerMultipleWithInput
        className="w-full"
        placeholder={selected.length === 0 ? placeholder : ""}
        inputSide="right"
        renderItem={(item: TagItem) => (
          <Combobox.Chip
            key={item.value}
            removeLabel={`Remove ${item.value}`}
            // Kumo's chip is a 2px corner; the tag chip elsewhere is the controls' radius.
            style={{ ...tagColours(item.value), borderRadius: "var(--radius-md)" }}
          >
            <TagText tag={item.value} />
          </Combobox.Chip>
        )}
      />
      <Combobox.Content className="max-h-64 overflow-y-auto">
        <Combobox.Empty>
          {suggestions.length === 0 && query === ""
            ? "Type a tag name, such as server."
            : "No tag matches."}
        </Combobox.Empty>
        <Combobox.List>
          {(item: TagItem) => (
            <Combobox.Item key={item.value} value={item}>
              <span className="flex min-w-0 items-center gap-2">
                <Tag tag={item.value} size="sm" />
                {item.create ? <span className="text-xs text-kumo-subtle">New tag</span> : null}
              </span>
            </Combobox.Item>
          )}
        </Combobox.List>
      </Combobox.Content>
    </Combobox>
  );
}
