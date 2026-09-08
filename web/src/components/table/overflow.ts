import { useCallback, useEffect, useState } from "react";

/** Which horizontal edges of a scroll container have content out of view. */
export interface OverflowEdges {
  readonly left: boolean;
  readonly right: boolean;
}

/** A scrollbar can land a fraction of a pixel short of the end; treat that as the end. */
const slack = 1;

/** Nothing to undo: the element is not there yet. */
function noop(): void {
  // The effect must always hand back the same shape.
}

function edgesOf(element: HTMLElement): OverflowEdges {
  return {
    left: element.scrollLeft > slack,
    right: element.scrollLeft + element.clientWidth < element.scrollWidth - slack,
  };
}

/**
 * Tracks the horizontal overflow of the element the returned ref is put on, so a table can fade the
 * edge that has more columns behind it. The element is watched for both scrolling and resizing,
 * because a column can appear at a breakpoint or a row's content can grow after the first paint.
 */
export function useOverflowEdges(): {
  readonly ref: (element: HTMLElement | null) => void;
  readonly edges: OverflowEdges;
} {
  const [element, setElement] = useState<HTMLElement | null>(null);
  const [edges, setEdges] = useState<OverflowEdges>({ left: false, right: false });

  useEffect(() => {
    if (element === null) {
      return noop;
    }

    const measure = (): void => {
      setEdges((current) => {
        const next = edgesOf(element);

        return current.left === next.left && current.right === next.right ? current : next;
      });
    };

    measure();
    element.addEventListener("scroll", measure, { passive: true });

    const observer = new ResizeObserver(measure);

    observer.observe(element);

    for (const child of element.children) {
      observer.observe(child);
    }

    return (): void => {
      element.removeEventListener("scroll", measure);
      observer.disconnect();
    };
  }, [element]);

  const ref = useCallback((next: HTMLElement | null): void => {
    setElement(next);
  }, []);

  return { ref, edges };
}
