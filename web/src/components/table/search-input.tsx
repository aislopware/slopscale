import { InputGroup } from "@cloudflare/kumo/components/input-group";
import { cn } from "@cloudflare/kumo/utils";
import { MagnifyingGlassIcon, XIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

export interface SearchInputProps {
  readonly value: string;
  readonly onValueChange: (value: string) => void;
  readonly placeholder?: string;
  readonly className?: string;
}

export function SearchInput({
  value,
  onValueChange,
  placeholder = "Search…",
  className,
}: SearchInputProps): ReactElement {
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
    </InputGroup>
  );
}
