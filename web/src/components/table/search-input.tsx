import { InputGroup } from "@cloudflare/kumo/components/input-group";
import { Toolbar } from "@cloudflare/kumo/components/toolbar";
import { cn } from "@cloudflare/kumo/utils";
import { MagnifyingGlassIcon, XIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

export interface SearchInputProps {
  readonly value: string;
  readonly onValueChange: (value: string) => void;
  readonly placeholder?: string;
  readonly className?: string;
  /**
   * Render as the first segment of a Kumo `Toolbar`, joined to the selects and buttons after it in
   * one ringed group (the dashboard's "Search | Show all | Last modified | ⟳"), taking the room
   * they leave.
   */
  readonly joined?: boolean;
}

/**
 * A list's search box: Kumo's InputGroup with a 16px magnifier, 36px tall, and a clear button once
 * there is something to clear. On its own it is up to 500px wide, the dashboard's DNS search.
 */
export function SearchInput({
  value,
  onValueChange,
  placeholder = "Search…",
  className,
  joined = false,
}: SearchInputProps): ReactElement {
  const Group = joined ? Toolbar.InputGroup : InputGroup;

  return (
    <Group className={cn(joined ? "min-w-40 flex-1" : "w-full max-w-[500px]", className)}>
      <InputGroup.Addon>
        <MagnifyingGlassIcon aria-hidden className="size-4" />
      </InputGroup.Addon>
      <InputGroup.Input
        type="search"
        aria-label={placeholder}
        placeholder={placeholder}
        value={value}
        onChange={(event) => {
          onValueChange(event.target.value);
        }}
        className="[&::-webkit-search-cancel-button]:hidden"
      />
      {value === "" ? null : (
        <InputGroup.Addon align="end">
          <InputGroup.Button
            variant="ghost"
            shape="square"
            size="xs"
            icon={XIcon}
            aria-label="Clear search"
            onClick={() => {
              onValueChange("");
            }}
          />
        </InputGroup.Addon>
      )}
    </Group>
  );
}
