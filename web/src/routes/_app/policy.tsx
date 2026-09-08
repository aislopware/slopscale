import { Tabs } from "@cloudflare/kumo/components/tabs";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import type { ReactElement, ReactNode } from "react";
import { object, optional, pipe, transform, unknown } from "valibot";

import {
  accessRequestsQuery,
  accessRulesQuery,
  groupsQuery,
  nodesQuery,
  policyQuery,
  posturesQuery,
  usersQuery,
} from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { GroupsTab } from "~/components/access/groups-tab.tsx";
import { PosturesTab } from "~/components/access/postures-tab.tsx";
import { pendingCount } from "~/components/access/request-model.ts";
import { RequestsTab } from "~/components/access/requests-tab.tsx";
import { RulesTab } from "~/components/access/rules-tab.tsx";
import { PolicyFileTab } from "~/components/policy/policy-file-tab.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

const tabs = ["rules", "groups", "postures", "requests", "file"] as const;
type Tab = (typeof tabs)[number];

const tabItems: readonly { value: Tab; label: string }[] = [
  { value: "rules", label: "Rules" },
  { value: "groups", label: "Groups" },
  { value: "postures", label: "Postures" },
  { value: "requests", label: "Requests" },
  { value: "file", label: "Policy file" },
];

/** The tab controls, with the number of pending requests on the Requests tab. */
function tabItemsWith(pending: number): { value: Tab; label: string }[] {
  return tabItems.map((item) => ({
    value: item.value,
    label: item.value === "requests" && pending > 0 ? `Requests (${pending})` : item.label,
  }));
}

function toText(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

function toTab(value: unknown): Tab | undefined {
  return tabs.find((known) => known === value);
}

const anyValue = unknown();
const optionalTab = optional(pipe(anyValue, transform(toTab)));
const optionalText = optional(pipe(anyValue, transform(toText)));

/** Absent means the default, so the URL only carries a tab or search the operator chose. */
const searchSchema = object({ tab: optionalTab, q: optionalText });

export const Route = createFileRoute("/_app/policy")({
  validateSearch: searchSchema,
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(policyQuery),
      context.queryClient.query(groupsQuery),
      context.queryClient.query(accessRulesQuery),
      context.queryClient.query(posturesQuery),
      context.queryClient.query(accessRequestsQuery),
    ]);
  },
  component: PolicyPage,
});

function PolicyPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const policy = useSuspenseQuery(policyQuery).data;
  const { groups } = useSuspenseQuery(groupsQuery).data;
  const { rules, policyFileEnforces } = useSuspenseQuery(accessRulesQuery).data;
  const { postures, geoIpAvailable } = useSuspenseQuery(posturesQuery).data;
  const { requests, canDecide } = useSuspenseQuery(accessRequestsQuery).data;
  // Group membership names machines and users; a caller without those scopes still sees counts.
  const nodes = useQuery({ ...nodesQuery, enabled: can(me, "devices:core:read") });
  const users = useQuery({ ...usersQuery, enabled: can(me, "users:read") });
  const tab = search.tab ?? "rules";
  const text = search.q ?? "";
  const canEdit = can(me, "policy_file");
  const hasRules = rules.some((rule) => rule.enabled);

  const setTab = (value: string): void => {
    const next = toTab(value) ?? "rules";

    void navigate({ search: () => ({ tab: next === "rules" ? undefined : next, q: undefined }) });
  };
  const setSearch = (value: string): void => {
    void navigate({
      search: (current) => ({ ...current, q: value === "" ? undefined : value }),
      replace: true,
    });
  };

  return (
    <>
      <PageHeader
        title="Access controls"
        description="Rules between groups of machines, and the policy file for everything else."
        meta={describe(rules, groups.length)}
      />
      <div className="flex">
        <Tabs
          variant="segmented"
          tabs={tabItemsWith(pendingCount(requests))}
          value={tab}
          onValueChange={setTab}
        />
      </div>
      {/* Kumo's Tabs renders the controls only, so each body names itself as the panel. */}
      {tab === "rules" ? (
        <TabPanel label="Rules">
          <RulesTab
            me={me}
            rules={rules}
            groups={groups}
            postures={postures}
            policyFileEnforces={policyFileEnforces}
            search={text}
            onSearchChange={setSearch}
          />
        </TabPanel>
      ) : null}
      {tab === "groups" ? (
        <TabPanel label="Groups">
          <GroupsTab
            me={me}
            groups={groups}
            rules={rules}
            nodes={nodes.data?.nodes}
            users={users.data?.users}
            search={text}
            onSearchChange={setSearch}
          />
        </TabPanel>
      ) : null}
      {tab === "postures" ? (
        <TabPanel label="Postures">
          <PosturesTab
            me={me}
            postures={postures}
            rules={rules}
            geoIpAvailable={geoIpAvailable}
            search={text}
            onSearchChange={setSearch}
          />
        </TabPanel>
      ) : null}
      {tab === "requests" ? (
        <TabPanel label="Requests">
          <RequestsTab
            me={me}
            requests={requests}
            canDecide={canDecide}
            groups={groups}
            users={users.data?.users}
            nodes={nodes.data?.nodes}
            search={text}
            onSearchChange={setSearch}
          />
        </TabPanel>
      ) : null}
      {tab === "file" ? (
        <TabPanel label="Policy file">
          <PolicyFileTab
            policy={policy}
            canEdit={canEdit}
            hasRules={hasRules}
            enforces={policyFileEnforces}
          />
        </TabPanel>
      ) : null}
    </>
  );
}

function TabPanel({
  label,
  children,
}: {
  readonly label: string;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <div role="tabpanel" aria-label={label} className="flex flex-col gap-6">
      {children}
    </div>
  );
}

function describe(rules: readonly { enabled: boolean }[], groupCount: number): string {
  const enabled = rules.filter((rule) => rule.enabled).length;
  const ruleText = enabled === 1 ? "1 rule enabled" : `${enabled} rules enabled`;
  const groupText = groupCount === 1 ? "1 group" : `${groupCount} groups`;

  return `${ruleText} · ${groupText}`;
}
