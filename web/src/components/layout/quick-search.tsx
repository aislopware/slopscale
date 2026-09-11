import { CommandPalette } from "@cloudflare/kumo/components/command-palette";
import type { Icon } from "@phosphor-icons/react";
import { DesktopIcon, UserIcon } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import type { ReactElement } from "react";

import { nodesQuery, usersQuery } from "~/api/queries.ts";
import type { Node, User } from "~/api/queries.ts";
import type { Me } from "~/auth/me.ts";
import { can, canSeeMachines } from "~/auth/me.ts";
import type { NavPage } from "~/components/layout/nav.ts";

interface Command {
  readonly id: string;
  readonly title: string;
  readonly hint?: string | undefined;
  readonly icon: Icon;
  readonly go: () => void;
}

interface CommandGroup {
  readonly id: string;
  readonly label: string;
  readonly items: Command[];
  /** How many rows the group shows; a group without one shows all of them. */
  readonly limit?: number;
}

/** Machines and users are a list of unknown length, so only the closest matches are worth a row. */
const maxResources = 8;

function matches(command: Command, query: string): boolean {
  const needle = query.trim().toLowerCase();

  return (
    needle === "" ||
    command.title.toLowerCase().includes(needle) ||
    (command.hint?.toLowerCase().includes(needle) ?? false)
  );
}

function filterGroups(groups: readonly CommandGroup[], query: string): CommandGroup[] {
  return groups
    .map((group) => {
      const found = group.items.filter((item) => matches(item, query));

      return { ...group, items: group.limit === undefined ? found : found.slice(0, group.limit) };
    })
    .filter((group) => group.items.length > 0);
}

/**
 * ⌘K palette: jumps to a page, a machine or a user. Resource lists load only while the palette is
 * open and only for callers who may read them.
 */
export function QuickSearch({
  me,
  pages,
  open,
  onOpenChange,
}: {
  readonly me: Me;
  readonly pages: readonly NavPage[];
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
}): ReactElement {
  const [query, setQuery] = useState("");
  const navigate = useNavigate();
  const nodes = useQuery({ ...nodesQuery, enabled: open && canSeeMachines(me) });
  const users = useQuery({ ...usersQuery, enabled: open && can(me, "users:read") });

  useShortcut(open, onOpenChange);

  const close = (): void => {
    onOpenChange(false);
    setQuery("");
  };

  const visible = filterGroups(
    buildGroups(
      { pages, nodes: nodes.data?.nodes ?? [], users: users.data?.users ?? [] },
      navigate,
    ),
    query,
  );

  return (
    <CommandPalette.Root
      open={open}
      onOpenChange={(next) => {
        if (next) {
          onOpenChange(true);
        } else {
          close();
        }
      }}
      items={visible}
      value={query}
      onValueChange={setQuery}
      itemToStringValue={(group) => group.label}
      getSelectableItems={(all) => all.flatMap((group) => group.items)}
      onSelect={(item: Command) => {
        item.go();
        close();
      }}
    >
      <CommandPalette.Input placeholder="Search pages, machines and users…" />
      <CommandPalette.List>
        <CommandPalette.Results>
          {(group: CommandGroup) => (
            <CommandPalette.Group key={group.id} items={group.items}>
              <CommandPalette.GroupLabel>{group.label}</CommandPalette.GroupLabel>
              <CommandPalette.Items>
                {(item: Command) => (
                  <CommandPalette.Item
                    key={item.id}
                    value={item}
                    onClick={() => {
                      item.go();
                      close();
                    }}
                  >
                    <span className="flex min-w-0 items-center gap-3">
                      <item.icon className="size-4 shrink-0 text-kumo-subtle" />
                      <span className="flex min-w-0 flex-col gap-0.5">
                        <span className="truncate">{item.title}</span>
                        {item.hint === undefined ? null : (
                          <span className="truncate text-xs text-kumo-subtle">{item.hint}</span>
                        )}
                      </span>
                    </span>
                  </CommandPalette.Item>
                )}
              </CommandPalette.Items>
            </CommandPalette.Group>
          )}
        </CommandPalette.Results>
        <CommandPalette.Empty>Nothing matches</CommandPalette.Empty>
      </CommandPalette.List>
      <CommandPalette.Footer>
        <span className="flex items-center gap-2">
          <Key>↑↓</Key> Navigate
        </span>
        <span className="flex items-center gap-2">
          <Key>↵</Key> Open
        </span>
        <span className="flex items-center gap-2">
          <Key>esc</Key> Close
        </span>
      </CommandPalette.Footer>
    </CommandPalette.Root>
  );
}

function useShortcut(open: boolean, onOpenChange: (open: boolean) => void): void {
  useEffect((): (() => void) => {
    const onKey = (event: KeyboardEvent): void => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        onOpenChange(!open);
      }
    };

    globalThis.addEventListener("keydown", onKey);

    return () => {
      globalThis.removeEventListener("keydown", onKey);
    };
  }, [open, onOpenChange]);
}

interface Sources {
  readonly pages: readonly NavPage[];
  readonly nodes: readonly Node[];
  readonly users: readonly User[];
}

function buildGroups(
  { pages, nodes, users }: Sources,
  navigate: ReturnType<typeof useNavigate>,
): CommandGroup[] {
  return [
    {
      // Every page the caller may see, straight from the sidebar's nav groups and never truncated,
      // so a page added there cannot go missing here.
      id: "pages",
      label: "Pages",
      items: pages.map((page) => ({
        id: `page:${page.to}`,
        title: page.label,
        ...(page.hint === undefined ? {} : { hint: page.hint }),
        icon: page.icon,
        go: () => {
          void navigate({ to: page.to });
        },
      })),
    },
    {
      id: "machines",
      label: "Machines",
      limit: maxResources,
      items: nodes.map((node) => ({
        id: `node:${node.id}`,
        title: node.givenName,
        hint: [node.user?.name, node.ipAddresses[0]].filter(Boolean).join(" · "),
        icon: DesktopIcon,
        go: () => {
          void navigate({ to: "/machines/$nodeId", params: { nodeId: node.id } });
        },
      })),
    },
    {
      id: "users",
      label: "Users",
      limit: maxResources,
      items: users.map((user) => ({
        id: `user:${user.id}`,
        title: user.displayName === "" ? user.name : user.displayName,
        // The second line says something the first does not: an email, or the account name behind a
        // display name. A user with neither is one line.
        hint: userHint(user),
        icon: UserIcon,
        go: () => {
          void navigate({ to: "/users", search: { q: user.name } });
        },
      })),
    },
  ];
}

function userHint(user: User): string | undefined {
  const title = user.displayName === "" ? user.name : user.displayName;

  if (user.email !== "" && user.email !== title) {
    return user.email;
  }

  return user.name === title ? undefined : user.name;
}

function Key({ children }: { readonly children: string }): ReactElement {
  return (
    <kbd className="rounded border border-kumo-hairline bg-kumo-base px-1.5 py-0.5 text-[10px]">
      {children}
    </kbd>
  );
}
