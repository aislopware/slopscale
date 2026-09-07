import { Badge } from "@cloudflare/kumo/components/badge";
import { ClipboardText } from "@cloudflare/kumo/components/clipboard-text";
import type { ReactElement, ReactNode } from "react";

import type { Node } from "~/api/queries.ts";
import { Card, CardBody, CardDescription, CardHeader, CardTitle } from "~/components/ui/card.tsx";
import { isTagged, nodeName, userLabel } from "~/lib/node.ts";
import { formatAbsolute, parseTime } from "~/lib/time.ts";

const registerMethods: Record<string, string> = {
  REGISTER_METHOD_AUTH_KEY: "Pre-auth key",
  REGISTER_METHOD_CLI: "Command line",
  REGISTER_METHOD_OIDC: "OpenID Connect",
};

/** Identity and registration, as a definition list of label/value pairs. */
export function OverviewCard({ node }: { readonly node: Node }): ReactElement {
  return (
    <Card>
      <CardHeader>
        <div>
          <CardTitle>Overview</CardTitle>
          <CardDescription>Identity and registration details.</CardDescription>
        </div>
      </CardHeader>
      <CardBody>
        <dl className="grid gap-x-8 gap-y-4 sm:grid-cols-2">
          <Detail label="Hostname">
            <Copyable value={node.name} />
          </Detail>
          <Detail label="Owner">
            <Owner node={node} />
          </Detail>
          <Detail label="Machine ID">
            <Copyable value={node.id} />
          </Detail>
          <Detail label="Registered with">
            {registerMethods[node.registerMethod] ?? "Unknown"}
          </Detail>
          <Detail label="Created">
            <Timestamp value={node.createdAt} />
          </Detail>
          <Detail label="Key expiry">
            {parseTime(node.expiry) === null ? "Never" : <Timestamp value={node.expiry} />}
          </Detail>
          <Detail label="Node key">
            <Copyable value={node.nodeKey} label="Copy node key" />
          </Detail>
          <Detail label="Machine key">
            <Copyable value={node.machineKey} label="Copy machine key" />
          </Detail>
        </dl>
      </CardBody>
    </Card>
  );
}

/** The machine's own addresses, each one click-to-copy. */
export function AddressesCard({ node }: { readonly node: Node }): ReactElement {
  return (
    <Card>
      <CardHeader>
        <div>
          <CardTitle>Addresses</CardTitle>
          <CardDescription>Reach this machine at any of these.</CardDescription>
        </div>
      </CardHeader>
      <CardBody className="flex flex-col gap-2">
        <Copyable value={nodeName(node)} label="Copy name" />
        {node.ipAddresses.map((address) => (
          <Copyable key={address} value={address} label={`Copy ${address}`} />
        ))}
      </CardBody>
    </Card>
  );
}

function Detail({
  label,
  children,
}: {
  readonly label: string;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <div className="flex flex-col gap-1">
      <dt className="text-sm text-kumo-subtle">{label}</dt>
      <dd className="text-kumo-default">{children}</dd>
    </div>
  );
}

function Copyable({
  value,
  label = "Copy to clipboard",
}: {
  readonly value: string;
  readonly label?: string;
}): ReactElement {
  return (
    <ClipboardText
      size="sm"
      text={value}
      tooltip={{ text: label, copiedText: "Copied" }}
      labels={{ copyAction: label }}
    />
  );
}

function Owner({ node }: { readonly node: Node }): ReactElement {
  if (isTagged(node)) {
    return (
      <span className="flex flex-wrap gap-1">
        {node.tags.map((tag) => (
          <Badge key={tag} variant="neutral">
            <span className="font-mono text-[0.9em]">{tag}</span>
          </Badge>
        ))}
      </span>
    );
  }

  return <span>{userLabel(node.user)}</span>;
}

function Timestamp({ value }: { readonly value: string | null }): ReactElement {
  const date = parseTime(value);

  return date === null ? (
    <span className="text-kumo-subtle">Unknown</span>
  ) : (
    <span>{formatAbsolute(date)}</span>
  );
}
