import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement, ReactNode } from "react";

import type { MachineOwner } from "~/api/schema.gen.ts";
import { Avatar } from "~/components/ui/avatar.tsx";
import { TagList } from "~/components/ui/tag.tsx";
import { machineOwnerLabel } from "~/lib/node.ts";

/** Tags the owner line shows before it counts the rest. */
const maxOwnerTags = 2;

/** Whose a machine is, sized for the line under its name: the person, or the machine's tags. */
export function MachineOwnerLine({
  owner,
}: {
  readonly owner: MachineOwner | undefined;
}): ReactElement | null {
  if (owner === undefined) {
    return null;
  }

  if (owner.tags.length > 0) {
    return <TagList tags={owner.tags} size="sm" max={maxOwnerTags} />;
  }

  const label = machineOwnerLabel(owner);

  if (label === "") {
    return null;
  }

  return (
    <span className="flex min-w-0 items-center gap-1.5 text-xs text-kumo-subtle" title={label}>
      <Avatar name={label} id={owner.userId} src={owner.profilePicUrl} size="sm" />
      <span className="truncate">{label}</span>
    </span>
  );
}

/**
 * A machine in a table cell or a list: the name, plain or a link, over whose it is. A given name
 * such as "localhost" tells neither the machine nor its owner, so a machine is never named alone.
 */
export function MachineName({
  name,
  owner,
  className,
}: {
  readonly name: ReactNode;
  readonly owner: MachineOwner | undefined;
  readonly className?: string | undefined;
}): ReactElement {
  return (
    <span className={cn("flex min-w-0 flex-col items-start gap-0.5", className)}>
      {typeof name === "string" ? <span className="max-w-full truncate">{name}</span> : name}
      <MachineOwnerLine owner={owner} />
    </span>
  );
}
