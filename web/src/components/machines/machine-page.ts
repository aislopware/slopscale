import { useQuery, useSuspenseQuery } from "@tanstack/react-query";

import { api } from "~/api/client.ts";
import { appsQuery, groupsQuery, servicesQuery, usersQuery } from "~/api/queries.ts";
import type { User } from "~/api/queries.ts";
import { can, canListUsers } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import type { MachineTabProps } from "~/components/machines/detail-cards.tsx";
import { machinePolling } from "~/components/machines/polling.ts";

const emptyUsers: readonly User[] = [];

/**
 * Everything the machine page and its tabs draw from. The layout and the tab under it ask for the
 * same queries, which the cache answers once; the machine itself is polled so a tab left open
 * follows the machine connecting, routes being approved and the like.
 */
export function useMachinePage(nodeId: string, me: Me): MachineTabProps {
  const detail = useSuspenseQuery({
    ...api.queryOptions("get", "/api/v1/node/{nodeId}", { params: { path: { nodeId } } }),
    ...machinePolling,
  });
  const users = useQuery({ ...usersQuery, enabled: canListUsers(me) });
  const groups = useQuery({ ...groupsQuery, enabled: can(me, "policy_file:read") });
  const apps = useQuery({ ...appsQuery, enabled: can(me, "policy_file:read") });
  const services = useQuery({ ...servicesQuery, enabled: can(me, "services:read") });

  return {
    node: detail.data.node,
    me,
    users: users.data?.users ?? emptyUsers,
    groups: groups.data?.groups,
    apps: apps.data?.apps,
    services: services.data?.services,
  };
}
