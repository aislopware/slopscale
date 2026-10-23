import { createContext, use, useEffect, useMemo, useState } from "react";
import type { ReactElement, ReactNode } from "react";

/** A step of the trail between the page and its leaf: a record whose sub-page this is. */
export interface Crumb {
  readonly label: string;
  readonly to: string;
}

/** What a page contributes to the trail: its own name, and the record it belongs to, if any. */
export interface BreadcrumbLeaf {
  readonly leaf: string;
  readonly parent?: Crumb | undefined;
}

interface BreadcrumbStore {
  readonly leaf: BreadcrumbLeaf | null;
  readonly setLeaf: (leaf: BreadcrumbLeaf | null) => void;
}

const BreadcrumbContext = createContext<BreadcrumbStore | null>(null);

/** Holds the name a detail page contributes to the shell's breadcrumb trail. */
export function BreadcrumbProvider({ children }: { readonly children: ReactNode }): ReactElement {
  const [leaf, setLeaf] = useState<BreadcrumbLeaf | null>(null);
  const value = useMemo(() => ({ leaf, setLeaf }), [leaf]);

  return <BreadcrumbContext value={value}>{children}</BreadcrumbContext>;
}

/** Read by the shell. */
export function useBreadcrumbLeaf(): BreadcrumbLeaf | null {
  return use(BreadcrumbContext)?.leaf ?? null;
}

/**
 * A detail page announces its subject; it is cleared when the page unmounts. A page that belongs to
 * a record (a machine's preferences, a service's settings) names that record as `parent`, so the
 * trail reads "Machines › office-minipc › Preferences" with the record a link back.
 */
export function useBreadcrumb(leaf: string | null, parent?: Crumb): void {
  const store = use(BreadcrumbContext);
  const parentLabel = parent?.label;
  const parentTo = parent?.to;

  useEffect((): (() => void) => {
    store?.setLeaf(
      leaf === null
        ? null
        : {
            leaf,
            parent:
              parentLabel === undefined || parentTo === undefined
                ? undefined
                : { label: parentLabel, to: parentTo },
          },
    );

    return () => {
      store?.setLeaf(null);
    };
  }, [store, leaf, parentLabel, parentTo]);
}
