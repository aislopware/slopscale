import type { ReactElement } from "react";

/** Three rows of three dots; a hash is a lit dot, and the lit dots spell an S. */
const rows = [".##", ".#.", "##."] as const;
const inset = 20;
const pitch = 30;
const radius = 11;
const unlitOpacity = 0.3;
/** The brand orange, the same in both themes: a logo keeps its colour, it does not follow the text. */
const orange = "#f6821f";

/** The slopscale mark: orange dots, the unlit ones faded, on whatever surface it sits. */
export function Mark({ className }: { readonly className?: string }): ReactElement {
  return (
    <svg viewBox="0 0 100 100" aria-hidden className={className} fill={orange}>
      {rows.flatMap((row, rowIndex) =>
        Array.from(row, (cell, columnIndex) => {
          const cx = inset + columnIndex * pitch;
          const cy = inset + rowIndex * pitch;

          return (
            <circle
              key={`${cx}-${cy}`}
              cx={cx}
              cy={cy}
              r={radius}
              fillOpacity={cell === "#" ? undefined : unlitOpacity}
            />
          );
        }),
      )}
    </svg>
  );
}
