import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";

import {
  appsQuery,
  nodesQuery,
  oauthClientsQuery,
  policyQuery,
  preAuthKeysQuery,
} from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me, Scope } from "~/auth/me.ts";
import { policyBlocks } from "~/components/policy/blocks.ts";
import { knownTags } from "~/lib/tag.ts";

const none: readonly string[] = [];

/**
 * Every tag the tailnet knows, for a tag field to offer: the ones the policy's `tagOwners` hands
 * out, the ones machines carry, the ones apps and pre-auth keys and OAuth clients name. Each source
 * is read only when the caller may read it, so a member gets what they can see and no request that
 * would be refused.
 */
export function useKnownTags(me: Me | undefined): readonly string[] {
  const may = (scope: Scope): boolean => me !== undefined && can(me, scope);
  const policy = useQuery({ ...policyQuery, enabled: may("policy_file:read") });
  const nodes = useQuery({ ...nodesQuery, enabled: may("devices:core:read") });
  const apps = useQuery({ ...appsQuery, enabled: may("policy_file:read") });
  const keys = useQuery({ ...preAuthKeysQuery, enabled: may("auth_keys:read") });
  const clients = useQuery({ ...oauthClientsQuery, enabled: may("oauth_keys:read") });

  const policyText = policy.data?.policy ?? "";
  const nodeList = nodes.data?.nodes;
  const appList = apps.data?.apps;
  const keyList = keys.data?.preAuthKeys;
  const clientList = clients.data?.oauthClients;

  return useMemo(
    () =>
      knownTags([
        policyBlocks(policyText).tags,
        nodeList?.flatMap((node) => node.tags) ?? none,
        appList?.flatMap((app) => app.connectors) ?? none,
        keyList?.flatMap((key) => key.aclTags) ?? none,
        clientList?.flatMap((client) => client.tags) ?? none,
      ]),
    [policyText, nodeList, appList, keyList, clientList],
  );
}
