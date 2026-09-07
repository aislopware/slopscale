import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { cn } from "@cloudflare/kumo/utils";
import type { ComponentProps, ReactElement } from "react";

/**
 * A titled panel: Kumo `LayerCard` in surface mode (base background, ring, soft shadow) with the
 * console's header row. Never nest cards; keep related text close (gap-1) and the card's sections
 * further apart.
 */
export function Card({
  className,
  children,
  ...props
}: Omit<ComponentProps<"section">, "ref">): ReactElement {
  return (
    <LayerCard render={<section {...props} />} {...(className === undefined ? {} : { className })}>
      {children}
    </LayerCard>
  );
}

export function CardHeader({ className, ...props }: ComponentProps<"header">): ReactElement {
  return (
    <header
      className={cn(
        "flex items-start justify-between gap-4 border-b border-kumo-line px-5 py-3",
        className,
      )}
      {...props}
    />
  );
}

export function CardTitle({ className, children, ...props }: ComponentProps<"h2">): ReactElement {
  return (
    <h2 className={cn("font-semibold text-kumo-default", className)} {...props}>
      {children}
    </h2>
  );
}

export function CardDescription({ className, ...props }: ComponentProps<"p">): ReactElement {
  return <p className={cn("mt-0.5 text-kumo-subtle", className)} {...props} />;
}

export function CardBody({ className, ...props }: ComponentProps<"div">): ReactElement {
  return <div className={cn("px-5 py-4", className)} {...props} />;
}
