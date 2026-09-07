import { createContext, use, useEffect, useMemo, useState } from "react";
import type { ReactElement, ReactNode } from "react";

interface BreadcrumbStore {
  readonly leaf: string | null;
  readonly setLeaf: (leaf: string | null) => void;
}

const BreadcrumbContext = createContext<BreadcrumbStore | null>(null);

/** Holds the name a detail page contributes to the shell's breadcrumb trail. */
export function BreadcrumbProvider({ children }: { readonly children: ReactNode }): ReactElement {
  const [leaf, setLeaf] = useState<string | null>(null);
  const value = useMemo(() => ({ leaf, setLeaf }), [leaf]);

  return <BreadcrumbContext value={value}>{children}</BreadcrumbContext>;
}

/** Read by the shell. */
export function useBreadcrumbLeaf(): string | null {
  return use(BreadcrumbContext)?.leaf ?? null;
}

/** A detail page announces its subject; it is cleared when the page unmounts. */
export function useBreadcrumb(leaf: string | null): void {
  const store = use(BreadcrumbContext);

  useEffect((): (() => void) => {
    store?.setLeaf(leaf);

    return () => {
      store?.setLeaf(null);
    };
  }, [store, leaf]);
}
