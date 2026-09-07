import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { SkeletonLine } from "@cloudflare/kumo/components/loader";
import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement, ReactNode } from "react";

export type StatTone = "neutral" | "warning";

export interface StatCardProps {
  readonly label: string;
  readonly value: ReactNode;
  readonly sub?: ReactNode;
  readonly tone?: StatTone;
}

/** One number on the overview: label, the count itself, and a line of context under it. */
export function StatCard({ label, value, sub, tone = "neutral" }: StatCardProps): ReactElement {
  return (
    <LayerCard className="flex flex-col gap-1 px-5 py-4">
      <p className="text-kumo-subtle">{label}</p>
      <p
        className={cn(
          "text-2xl font-semibold",
          tone === "warning" ? "text-kumo-warning" : "text-kumo-default",
        )}
      >
        {value}
      </p>
      <p className="text-sm text-kumo-subtle">{sub}</p>
    </LayerCard>
  );
}

export function StatSkeleton(): ReactElement {
  return (
    <LayerCard aria-busy className="flex flex-col gap-1 px-5 py-4">
      <SkeletonLine blockHeight={21} minWidth={35} maxWidth={55} />
      <SkeletonLine blockHeight={32} minWidth={20} maxWidth={30} />
      <SkeletonLine blockHeight={19} minWidth={40} maxWidth={70} />
    </LayerCard>
  );
}
