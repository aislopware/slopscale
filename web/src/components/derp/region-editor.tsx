import { Input } from "@cloudflare/kumo/components/input";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Derp } from "~/api/queries.ts";
import type { DerpCustomRegion } from "~/api/schema.gen.ts";
import {
  customRegionIds,
  duplicateRelayName,
  regionCodeError,
  regionIdError,
  relayDraft,
  relayFieldErrors,
  relayFromDraft,
  withRegion,
  withoutRegion,
} from "~/components/derp/model.ts";
import type { FieldErrors, RelayDraft } from "~/components/derp/model.ts";
import { useDerpMutations } from "~/components/derp/mutations.ts";
import { RelayList, useShown } from "~/components/derp/relay-list.tsx";
import type { Shown } from "~/components/derp/relay-list.tsx";
import { StaleSettingsBanner } from "~/components/derp/source.tsx";
import { ConfirmDialog, ConfirmSummary } from "~/components/ui/confirm-dialog.tsx";
import { fieldError } from "~/components/ui/field-error.ts";
import { FormPage, FormSection, useDraft } from "~/components/ui/form-page.tsx";
import { toast } from "~/components/ui/toast.ts";

export const regionsListPath = "/relays/own";

interface Draft {
  readonly id: string;
  readonly code: string;
  readonly name: string;
  readonly relays: readonly RelayDraft[];
}

/**
 * The region as the form edits it. A saved relay's key is its place, not a random id, so the draft
 * of an untouched region equals the one the page opened with.
 */
function draftFrom(region: DerpCustomRegion | undefined): Draft {
  return {
    id: region === undefined ? "" : String(region.id),
    code: region?.code ?? "",
    name: region?.name ?? "",
    relays:
      region === undefined
        ? [{ ...relayDraft(), key: "new" }]
        : (region.nodes ?? []).map((relay, index) =>
            Object.assign(relayDraft(relay), { key: `${index}` }),
          ),
  };
}

interface Issues {
  readonly id: string | null;
  readonly code: string | null;
  readonly relays: readonly FieldErrors<RelayDraft>[];
  /** What is wrong with the list as a whole: none at all, or two relays of one name. */
  readonly list: string | null;
}

function listIssue(count: number, duplicate: string | null): string | null {
  if (count === 0) {
    return "Add at least one relay.";
  }

  return duplicate === null ? null : `Two relays are named ${duplicate}. Rename one.`;
}

function draftIssues(draft: Draft, taken: readonly number[]): Issues {
  const duplicate = duplicateRelayName(draft.relays);

  return {
    id: regionIdError(draft.id.trim(), taken),
    code: regionCodeError(draft.code.trim()),
    relays: draft.relays.map((relay) => relayFieldErrors(relay)),
    list: listIssue(draft.relays.length, duplicate),
  };
}

function hasIssues(issues: Issues): boolean {
  return (
    issues.id !== null ||
    issues.code !== null ||
    issues.list !== null ||
    issues.relays.some((errors) => Object.keys(errors).length > 0)
  );
}

function regionFrom(draft: Draft): DerpCustomRegion {
  const region: DerpCustomRegion = {
    id: Number(draft.id.trim()),
    code: draft.code.trim(),
    nodes: draft.relays.map((relay) => relayFromDraft(relay)),
  };

  if (draft.name.trim() !== "") {
    region.name = draft.name.trim();
  }

  return region;
}

export interface RegionEditorProps {
  /** The region to edit; absent when adding one. */
  readonly region?: DerpCustomRegion | undefined;
  readonly derp: Derp;
  readonly canEdit: boolean;
}

/**
 * A region of relays the operator runs, on a page of its own for adding or editing it: its id, code
 * and name, and the relays in it. The whole relay configuration is sent back; saving goes back to
 * the list.
 */
export function RegionEditor({ region, derp, canEdit }: RegionEditorProps): ReactElement {
  const navigate = useNavigate();
  const mutations = useDerpMutations();
  const settings = derp.effective;
  const { draft, dirty, submitted, update, markSubmitted } = useDraft(
    draftFrom(region),
    mutations.set,
  );
  const [deleting, setDeleting] = useState(false);
  // The relays the last save looked at; one added after it says nothing until the next save.
  const [checkedRelays, setCheckedRelays] = useState<ReadonlySet<string>>(new Set());
  const shown = useShown((field) => {
    const dot = field.indexOf(".");

    return dot === -1 ? submitted : checkedRelays.has(field.slice(0, dot));
  });
  const taken = [
    ...customRegionIds(settings).filter((existing) => existing !== region?.id),
    ...(settings.server.enabled ? [settings.server.regionId ?? 0] : []),
  ];
  const issues = draftIssues(draft, taken);

  const toList = (): void => {
    void navigate({ to: regionsListPath, ignoreBlocker: true });
  };

  function save(): boolean {
    markSubmitted();
    setCheckedRelays(new Set(draft.relays.map((relay) => relay.key)));

    if (hasIssues(issues)) {
      return false;
    }

    mutations.set.mutate(
      { body: withRegion(settings, regionFrom(draft), region?.id) },
      {
        onSuccess: () => {
          toast.success(region === undefined ? "Region added" : "Region saved");
          toList();
        },
      },
    );

    return true;
  }

  return (
    <FormPage
      noun="region"
      creating={region === undefined}
      canEdit={canEdit}
      // A refusal for stale settings has its own banner, which offers the reload.
      error={
        mutations.set.isError && !mutations.stale ? errorMessage(mutations.set.error) : undefined
      }
      errorTitle={region === undefined ? "The region was not added" : "The region was not saved"}
      pending={mutations.set.isPending}
      dirty={dirty}
      onSubmit={save}
      cancelTo={regionsListPath}
      submitLabel={region === undefined ? "Add region" : "Save region"}
      onDelete={() => {
        setDeleting(true);
      }}
      dialog={
        region === undefined ? null : (
          <ConfirmDialog
            open={deleting}
            onOpenChange={setDeleting}
            title="Delete region?"
            description="Machines using its relays move to the next closest region."
            summary={
              <ConfirmSummary name={`${region.id} · ${region.code}`}>
                {region.name === region.code ? "" : (region.name ?? "")}
              </ConfirmSummary>
            }
            confirmLabel="Delete region"
            loading={mutations.set.isPending}
            error={mutations.set.isError ? errorMessage(mutations.set.error) : undefined}
            onConfirm={() => {
              mutations.set.mutate(
                { body: withoutRegion(settings, region.id) },
                {
                  onSuccess: () => {
                    setDeleting(false);
                    toast.success("Region deleted");
                    toList();
                  },
                },
              );
            }}
          />
        )
      }
    >
      <StaleSettingsBanner mutations={mutations} />
      <DetailsSection draft={draft} issues={issues} shown={shown} onChange={update} />
      <RelayList
        relays={draft.relays}
        errors={issues.relays}
        listError={submitted ? issues.list : null}
        shown={shown}
        onChange={(relays) => {
          update({ relays });
        }}
      />
    </FormPage>
  );
}

function DetailsSection({
  draft,
  issues,
  shown,
  onChange,
}: {
  readonly draft: Draft;
  readonly issues: Issues;
  readonly shown: Shown;
  readonly onChange: (patch: Partial<Draft>) => void;
}): ReactElement {
  return (
    <FormSection
      title="Region details"
      info="A region groups relays that are close to each other. A machine picks the region with the lowest latency."
    >
      <div className="grid items-start gap-4 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,2fr)]">
        <Input
          className="w-full"
          label="Region id"
          value={draft.id}
          placeholder="900"
          inputMode="numeric"
          autoComplete="off"
          description="900 or above keeps clear of Tailscale's regions."
          {...fieldError(shown.error("id", draft.id, issues.id))}
          onChange={(event) => {
            onChange({ id: event.target.value });
          }}
          onBlur={() => {
            shown.leave("id");
          }}
        />
        <Input
          className="w-full"
          label="Code"
          value={draft.code}
          placeholder="sgp"
          spellCheck={false}
          autoComplete="off"
          description="The short code clients show."
          {...fieldError(shown.error("code", draft.code, issues.code))}
          onChange={(event) => {
            onChange({ code: event.target.value });
          }}
          onBlur={() => {
            shown.leave("code");
          }}
        />
        <Input
          className="w-full"
          label="Name"
          required={false}
          value={draft.name}
          placeholder="Singapore"
          autoComplete="off"
          description="Leave empty to use the code."
          onChange={(event) => {
            onChange({ name: event.target.value });
          }}
        />
      </div>
    </FormSection>
  );
}
