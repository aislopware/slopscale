import { Badge } from "@cloudflare/kumo/components/badge";
import { Input } from "@cloudflare/kumo/components/input";
import { Select } from "@cloudflare/kumo/components/select";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import type { Group, Posture } from "~/api/queries.ts";
import { hasPorts, protocolLabel, protocols, toProtocol } from "~/components/access/model.ts";
import { groupItems, postureItems } from "~/components/access/pickers.ts";
import type { RuleDraft, RuleDraftErrors } from "~/components/access/rule-draft.ts";
import { ConditionPicker } from "~/components/policy/condition-picker.tsx";
import {
  AddCondition,
  ConditionList,
  ConditionRow,
  ConditionSlot,
  NoConditions,
  SettingToggle,
  conditionLabel,
} from "~/components/policy/rule-builder.tsx";
import { DateTimeField } from "~/components/ui/date-time-field.tsx";

type Change = (patch: Partial<RuleDraft>) => void;

/** Where the connection goes: the destination groups, the protocol and, for TCP or UDP, ports. */
function TrafficSlot({
  draft,
  groups,
  errors,
  onChange,
}: {
  readonly draft: RuleDraft;
  readonly groups: readonly Group[];
  readonly errors: RuleDraftErrors;
  readonly onChange: Change;
}): ReactElement {
  const ports = {
    key: "ports",
    row: (
      <ConditionRow>
        <Input
          label={conditionLabel("Port", "is in")}
          value={draft.ports}
          spellCheck={false}
          autoComplete="off"
          placeholder="22, 443, 8000-8100"
          description="Leave it empty for every port."
          {...(errors.ports === undefined ? {} : { error: errors.ports })}
          onChange={(event) => {
            onChange({ ports: event.target.value });
          }}
        />
      </ConditionRow>
    ),
  };

  return (
    <ConditionSlot
      title="And the traffic matches…"
      description="What the connection may reach on the destination."
    >
      <ConditionList
        conditions={[
          {
            key: "destination",
            row: (
              <ConditionRow>
                <ConditionPicker
                  label={conditionLabel("Destination group", "is in")}
                  placeholder="Groups that accept the connection…"
                  items={groupItems(groups, "destination")}
                  value={draft.destinations}
                  error={errors.destinations}
                  empty="No group matches."
                  onValueChange={(destinations) => {
                    onChange({ destinations });
                  }}
                />
              </ConditionRow>
            ),
          },
          {
            key: "protocol",
            row: (
              <ConditionRow>
                <Select
                  className="w-full"
                  label={conditionLabel("Protocol", "is")}
                  aria-label="Protocol"
                  value={draft.protocol}
                  renderValue={(value) => protocolLabel(value)}
                  onValueChange={(value) => {
                    onChange({ protocol: toProtocol(value ?? "all") });
                  }}
                >
                  {protocols.map((option) => (
                    <Select.Option key={option} value={option}>
                      {protocolLabel(option)}
                    </Select.Option>
                  ))}
                </Select>
              </ConditionRow>
            ),
          },
          ...(hasPorts(draft.protocol) ? [ports] : []),
        ]}
      />
    </ConditionSlot>
  );
}

/** The posture a source machine must meet: none until the operator adds the condition. */
function PostureSlot({
  draft,
  postures,
  onChange,
}: {
  readonly draft: RuleDraft;
  readonly postures: readonly Posture[];
  readonly onChange: Change;
}): ReactElement {
  const [adding, setAdding] = useState(false);
  const open = adding || draft.postures.length > 0;
  const action =
    postures.length === 0 ? (
      <span>
        There are no postures yet.{" "}
        <Link to="/policy/postures/new" className="text-kumo-link hover:underline">
          Create one
        </Link>
      </span>
    ) : (
      <AddCondition
        onClick={() => {
          setAdding(true);
        }}
      >
        Add condition
      </AddCondition>
    );

  return (
    <ConditionSlot
      title="And device posture matches…"
      description="The source machine must meet at least one of these postures."
    >
      {open ? (
        <ConditionRow
          removeLabel="Remove the posture condition"
          onRemove={() => {
            setAdding(false);
            onChange({ postures: [] });
          }}
        >
          <ConditionPicker
            label={conditionLabel("Posture", "passes one of")}
            placeholder="Postures the source must meet…"
            items={postureItems(postures)}
            value={draft.postures}
            empty="No posture matches."
            onValueChange={(selected) => {
              onChange({ postures: selected });
            }}
          />
        </ConditionRow>
      ) : (
        <NoConditions action={action} />
      )}
    </ConditionSlot>
  );
}

/** The If column: the source, what it reaches, and the posture its machine must meet. */
export function RuleConditions({
  draft,
  groups,
  postures,
  errors,
  onChange,
}: {
  readonly draft: RuleDraft;
  readonly groups: readonly Group[];
  readonly postures: readonly Posture[];
  readonly errors: RuleDraftErrors;
  readonly onChange: Change;
}): ReactElement {
  return (
    <>
      <ConditionSlot
        title="And the source matches…"
        description="Machines in these groups may open the connection."
      >
        <ConditionRow>
          <ConditionPicker
            label={conditionLabel("Source group", "is in")}
            placeholder="Groups that open the connection…"
            items={groupItems(groups, "members")}
            value={draft.sources}
            error={errors.sources}
            empty="No group matches."
            onValueChange={(sources) => {
              onChange({ sources });
            }}
          />
        </ConditionRow>
      </ConditionSlot>
      <TrafficSlot draft={draft} groups={groups} errors={errors} onChange={onChange} />
      <PostureSlot draft={draft} postures={postures} onChange={onChange} />
    </>
  );
}

/** The Then column: the action, which is always allow, and the settings that shape it. */
export function RuleSettings({
  draft,
  toOwnMachines,
  errors,
  onChange,
}: {
  readonly draft: RuleDraft;
  readonly toOwnMachines: boolean;
  readonly errors: RuleDraftErrors;
  readonly onChange: Change;
}): ReactElement {
  return (
    <>
      <div className="flex flex-col gap-1.5">
        <span className="font-medium text-kumo-default">Then…</span>
        <div className="flex items-center gap-2">
          <Badge variant="success">Allow</Badge>
          <span className="text-sm text-kumo-subtle">
            A rule only allows. Traffic no rule allows is blocked.
          </span>
        </div>
      </div>
      <div className="flex flex-col gap-4">
        <SettingToggle
          title="Both directions"
          description={
            toOwnMachines
              ? "Own machines is defined by the source, so this rule cannot go both ways."
              : "Destinations may open connections to the sources too."
          }
          checked={draft.bidirectional && !toOwnMachines}
          disabled={toOwnMachines}
          onCheckedChange={(bidirectional) => {
            onChange({ bidirectional });
          }}
        />
        <SettingToggle
          title="Rule expiry"
          description="The rule stops applying at a set time and stays, marked expired."
          checked={draft.expiring}
          onCheckedChange={(expiring) => {
            onChange(expiring ? { expiring } : { expiring, expires: "" });
          }}
        >
          {draft.expiring ? (
            <div className="flex flex-col gap-1.5">
              <DateTimeField
                label="Expires"
                emptyLabel="Pick a date"
                value={draft.expires}
                onChange={(expires) => {
                  onChange({ expires });
                }}
              />
              {errors.expires === undefined ? null : (
                <p className="text-sm leading-snug text-kumo-danger">{errors.expires}</p>
              )}
            </div>
          ) : null}
        </SettingToggle>
      </div>
    </>
  );
}
