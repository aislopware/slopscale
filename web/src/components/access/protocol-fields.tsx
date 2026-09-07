import { Input } from "@cloudflare/kumo/components/input";
import { Select } from "@cloudflare/kumo/components/select";
import type { ReactElement } from "react";

import {
  hasPorts,
  portsError,
  protocolLabel,
  protocols,
  toProtocol,
} from "~/components/access/model.ts";
import type { Protocol } from "~/components/access/model.ts";

export interface ProtocolDraft {
  readonly protocol: Protocol;
  readonly ports: string;
}

/** The protocol select and the port list, shared by access rules and networks. */
export function ProtocolFields({
  draft,
  onChange,
}: {
  readonly draft: ProtocolDraft;
  readonly onChange: (patch: Partial<ProtocolDraft>) => void;
}): ReactElement {
  const withPorts = hasPorts(draft.protocol);
  const issue = withPorts ? portsError(draft.ports) : null;

  return (
    <div className="grid items-start gap-4 sm:grid-cols-2">
      <Select
        className="w-full"
        label="Protocol"
        value={draft.protocol}
        onValueChange={(value) => {
          onChange({ protocol: toProtocol(value ?? "all") });
        }}
        renderValue={(value) => protocolLabel(value)}
      >
        {protocols.map((option) => (
          <Select.Option key={option} value={option}>
            {protocolLabel(option)}
          </Select.Option>
        ))}
      </Select>
      <Input
        label="Ports"
        required={false}
        disabled={!withPorts}
        value={withPorts ? draft.ports : ""}
        spellCheck={false}
        autoComplete="off"
        placeholder={withPorts ? "22, 443, 8000-8100" : "Every port"}
        description={withPorts ? "Empty means every port." : "Ports apply to TCP and UDP."}
        {...(issue === null ? {} : { error: issue })}
        onChange={(event) => {
          onChange({ ports: event.target.value });
        }}
      />
    </div>
  );
}
