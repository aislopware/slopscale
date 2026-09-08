import { InputGroup } from "@cloudflare/kumo/components/input-group";
import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import { cn } from "@cloudflare/kumo/utils";
import { InfoIcon, MagnifyingGlassIcon, XIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

export interface SearchInputProps {
  readonly value: string;
  readonly onValueChange: (value: string) => void;
  readonly placeholder?: string;
  /**
   * How the filter matches, shown behind an info button inside the field, so the hint travels with
   * the input instead of floating next to it in the toolbar.
   */
  readonly hint?: string;
  readonly className?: string;
}

export function SearchInput({
  value,
  onValueChange,
  placeholder = "Search…",
  hint,
  className,
}: SearchInputProps): ReactElement {
  const clearable = value !== "";

  return (
    <InputGroup className={cn("w-full max-w-xs", className)}>
      <InputGroup.Addon>
        <MagnifyingGlassIcon aria-hidden />
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
      {!clearable && hint === undefined ? null : (
        <InputGroup.Addon align="end">
          {clearable ? (
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
          ) : null}
          {hint === undefined ? null : (
            <Tooltip
              side="bottom"
              content={hint}
              render={
                <InputGroup.Button
                  variant="ghost"
                  shape="square"
                  size="xs"
                  icon={InfoIcon}
                  aria-label={`How ${placeholder.toLowerCase()} matches`}
                />
              }
            />
          )}
        </InputGroup.Addon>
      )}
    </InputGroup>
  );
}
