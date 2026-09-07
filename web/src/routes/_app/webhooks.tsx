import { Tabs } from "@cloudflare/kumo/components/tabs";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import type { ReactElement, ReactNode } from "react";
import { object, optional, pipe, transform, unknown } from "valibot";

import { logStreamsQuery, webhookEventTypesQuery, webhooksQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { streamState } from "~/components/logstreams/model.ts";
import { LogStreamsTab } from "~/components/logstreams/tab.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { deliveryState } from "~/components/webhooks/model.ts";
import { WebhooksTab, countWebhooks } from "~/components/webhooks/webhooks-tab.tsx";

const tabs = ["webhooks", "streams"] as const;
type Tab = (typeof tabs)[number];

const tabItems: { value: Tab; label: string }[] = [
  { value: "webhooks", label: "Webhooks" },
  { value: "streams", label: "Log streams" },
];

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

export const Route = createFileRoute("/_app/webhooks")({
  validateSearch: searchSchema,
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(webhooksQuery),
      context.queryClient.query(webhookEventTypesQuery),
      can(context.me, "logs:configuration:read")
        ? context.queryClient.query(logStreamsQuery)
        : Promise.resolve(),
    ]);
  },
  component: IntegrationsPage,
});

function IntegrationsPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { webhooks } = useSuspenseQuery(webhooksQuery).data;
  const { types: eventTypes } = useSuspenseQuery(webhookEventTypesQuery).data;
  const canReadStreams = can(me, "logs:configuration:read");
  const streams = useQuery({ ...logStreamsQuery, enabled: canReadStreams });
  const logStreams = streams.data?.logStreams ?? [];
  const tab = canReadStreams ? (search.tab ?? "webhooks") : "webhooks";
  const text = search.q ?? "";

  const setTab = (value: string): void => {
    const next = toTab(value) ?? "webhooks";

    void navigate({
      search: () => ({ tab: next === "webhooks" ? undefined : next, q: undefined }),
    });
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
        title="Integrations"
        description="Webhooks post events to endpoints, chat channels and inboxes as they happen. Log streams ship every audit log entry to a SIEM or log store in batches."
        meta={describe(webhooks, logStreams, canReadStreams)}
      />
      {canReadStreams ? (
        <div className="flex">
          <Tabs variant="segmented" tabs={tabItems} value={tab} onValueChange={setTab} />
        </div>
      ) : null}
      {/* Kumo's Tabs renders the controls only, so each body names itself as the panel. */}
      {tab === "webhooks" ? (
        <TabPanel label="Webhooks">
          <WebhooksTab
            me={me}
            webhooks={webhooks}
            eventTypes={eventTypes}
            search={text}
            onSearchChange={setSearch}
          />
        </TabPanel>
      ) : null}
      {tab === "streams" ? (
        <TabPanel label="Log streams">
          <LogStreamsTab me={me} streams={logStreams} search={text} onSearchChange={setSearch} />
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

function describe(
  webhooks: readonly { lastDeliveryStatus: string }[],
  streams: readonly { enabled: boolean; lastDeliveryStatus: string }[],
  withStreams: boolean,
): string {
  const parts = [countWebhooks(webhooks.length)];
  const failing =
    webhooks.filter((webhook) => deliveryState(webhook) === "failed").length +
    streams.filter((stream) => streamState(stream) === "failed").length;

  if (withStreams) {
    parts.push(streams.length === 1 ? "1 log stream" : `${streams.length} log streams`);
  }

  if (failing > 0) {
    parts.push(`${failing} failing`);
  }

  return parts.join(" · ");
}
