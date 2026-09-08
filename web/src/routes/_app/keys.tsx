import { Tabs } from "@cloudflare/kumo/components/tabs";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import type { ReactElement } from "react";
import { object, optional, pipe, transform, unknown } from "valibot";

import { apiKeysQuery, oauthClientsQuery, preAuthKeysQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { ApiPanel, OAuthPanel, PreAuthPanel } from "~/components/keys/panels.tsx";
import type { PanelControls } from "~/components/keys/panels.tsx";
import { statusFilters } from "~/components/keys/status.ts";
import type { StatusFilter } from "~/components/keys/status.ts";
import { PageHeader } from "~/components/ui/page-header.tsx";

const tabs = ["preauth", "api", "oauth"] as const;
type TabValue = (typeof tabs)[number];

function toTab(value: unknown): TabValue | undefined {
  return tabs.find((known) => known === value);
}

function toText(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

function toStatus(value: unknown): StatusFilter | undefined {
  return statusFilters.find((known) => known === value);
}

/**
 * Every parameter reads as "absent means the default", so an untouched control never reaches the
 * URL and an unknown value in a hand-written link is ignored instead of failing the route.
 */
/** One reusable `unknown()` so each entry is only a pipe over a named transform. */
const anyValue = unknown();

const optionalTab = optional(pipe(anyValue, transform(toTab)));
const optionalText = optional(pipe(anyValue, transform(toText)));
const optionalStatus = optional(pipe(anyValue, transform(toStatus)));

const searchSchema = object({ tab: optionalTab, q: optionalText, status: optionalStatus });

interface KeysSearch {
  readonly tab: TabValue | undefined;
  readonly q: string | undefined;
  readonly status: StatusFilter | undefined;
}

function searchFor(tab: TabValue, query: string, status: StatusFilter): KeysSearch {
  return {
    tab: tab === "preauth" ? undefined : tab,
    q: query === "" ? undefined : query,
    status: status === "all" ? undefined : status,
  };
}

export const Route = createFileRoute("/_app/keys")({
  validateSearch: searchSchema,
  loaderDeps: () => ({}),
  loader: async ({ context }) => {
    await Promise.all([
      can(context.me, "auth_keys:read")
        ? context.queryClient.query(preAuthKeysQuery)
        : Promise.resolve(),
      context.queryClient.query(apiKeysQuery),
      can(context.me, "oauth_keys:read")
        ? context.queryClient.query(oauthClientsQuery)
        : Promise.resolve(),
    ]);
  },
  component: KeysPage,
});

function KeysPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const mayReadPreAuth = can(me, "auth_keys:read");
  const mayReadOAuth = can(me, "oauth_keys:read");
  const tab = visibleTab(search.tab, mayReadPreAuth, mayReadOAuth);

  const controls: PanelControls = {
    query: search.q ?? "",
    status: search.status ?? "all",
    handleQueryChange: (value) => {
      void navigate({ search: () => searchFor(tab, value, search.status ?? "all"), replace: true });
    },
    handleStatusChange: (value) => {
      void navigate({ search: () => searchFor(tab, search.q ?? "", toStatus(value) ?? "all") });
    },
    handleClear: () => {
      void navigate({ search: () => searchFor(tab, "", "all"), replace: true });
    },
  };

  // Filters belong to the table below, so switching tables clears them.
  const handleTabChange = (next: string): void => {
    void navigate({ search: () => searchFor(toTab(next) ?? "preauth", "", "all") });
  };

  const tabItems = [
    ...(mayReadPreAuth ? [{ value: "preauth", label: "Pre-auth keys" }] : []),
    { value: "api", label: "API keys" },
    ...(mayReadOAuth ? [{ value: "oauth", label: "OAuth clients" }] : []),
  ];

  return (
    <>
      <PageHeader
        title="Keys"
        description="Pre-auth keys register machines without a login. API keys and OAuth clients authenticate this console and automation."
      />
      <div className="flex">
        <Tabs variant="segmented" tabs={tabItems} value={tab} onValueChange={handleTabChange} />
      </div>
      {tab === "preauth" ? <PreAuthPanel me={me} controls={controls} /> : null}
      {tab === "api" ? <ApiPanel me={me} controls={controls} /> : null}
      {tab === "oauth" ? <OAuthPanel me={me} controls={controls} /> : null}
    </>
  );
}

/** The tab to show: the one asked for if the caller may read it, else the first it may. */
function visibleTab(asked: TabValue | undefined, preAuth: boolean, oauth: boolean): TabValue {
  if (asked === "preauth" && preAuth) {
    return "preauth";
  }

  if (asked === "oauth" && oauth) {
    return "oauth";
  }

  if (asked === undefined && preAuth) {
    return "preauth";
  }

  return "api";
}
