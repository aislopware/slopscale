import { Button } from "@cloudflare/kumo/components/button";
import { Collapsible } from "@cloudflare/kumo/components/collapsible";
import { ArrowsClockwiseIcon, CaretRightIcon, DownloadSimpleIcon } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import { nodeDiagnosticKinds, nodeDiagnosticUrl, nodeHealthQuery } from "~/api/queries.ts";
import type { Node, NodeDiagnosticKind } from "~/api/queries.ts";
import type { NodeClientWarning } from "~/api/schema.gen.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import {
  diagnosticFileName,
  diagnosticLabels,
  warningLabel,
  warningTone,
} from "~/components/machines/health-model.ts";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { Section, SectionRow } from "~/components/ui/section.tsx";
import { Status } from "~/components/ui/status.tsx";
import { toast } from "~/components/ui/toast.ts";
import { downloadFile } from "~/lib/download.ts";

const caretSize = 14;
const buttonIconSize = 12;

/**
 * What the machine says about itself. These are the warnings the client would show its own user,
 * which is where a machine that is connected but not working says why, so they are read from the
 * client over its control connection rather than from anything the server stored. An offline
 * machine has nothing to answer with, so nothing is asked.
 */
export function ClientHealthSection({
  node,
  me,
}: {
  readonly node: Node;
  readonly me: Me;
}): ReactElement {
  const health = useQuery({ ...nodeHealthQuery(node.id), enabled: node.online });

  return (
    <Section
      title="Client health"
      actions={
        node.online ? (
          <Button
            variant="ghost"
            shape="square"
            size="sm"
            aria-label="Ask the machine again"
            loading={health.isFetching}
            icon={<ArrowsClockwiseIcon size={buttonIconSize} />}
            onClick={() => {
              void health.refetch();
            }}
          />
        ) : undefined
      }
      bodyClassName="p-0"
    >
      {node.online ? (
        <Report
          warnings={health.data?.warnings}
          loading={health.isPending}
          error={health.isError ? errorMessage(health.error) : undefined}
        />
      ) : (
        <SectionRow className="text-kumo-subtle">
          Connect the machine to check its health.
        </SectionRow>
      )}
      {can(me, "devices:core") ? <Diagnostics node={node} /> : null}
    </Section>
  );
}

/**
 * The answer, whichever it was. A refusal is the machine's own: it is offline, it timed out or its
 * client would not say, and the detail is what the operator needs, so it reads inline rather than
 * flashing past in a toast.
 */
function Report({
  warnings,
  loading,
  error,
}: {
  readonly warnings: readonly NodeClientWarning[] | undefined;
  readonly loading: boolean;
  readonly error: string | undefined;
}): ReactElement {
  if (error !== undefined) {
    return (
      <SectionRow>
        <Status tone="danger" className="items-baseline whitespace-normal">
          {error}
        </Status>
      </SectionRow>
    );
  }

  if (warnings === undefined) {
    return (
      <SectionRow className="text-kumo-subtle">
        {loading ? "Asking the machine…" : "No answer yet."}
      </SectionRow>
    );
  }

  if (warnings.length === 0) {
    return (
      <SectionRow>
        <Status tone="success">Healthy</Status>
      </SectionRow>
    );
  }

  return (
    <>
      {warnings.map((warning) => (
        <Warning key={warning.code} warning={warning} />
      ))}
    </>
  );
}

function Warning({ warning }: { readonly warning: NodeClientWarning }): ReactElement {
  return (
    <SectionRow className="flex flex-col gap-1 py-3">
      <Status tone={warningTone(warning.severity)} className="items-baseline whitespace-normal">
        {warningLabel(warning)}
      </Status>
      <p className="text-kumo-subtle">{warning.text}</p>
      {warning.brokenSince === undefined ? null : (
        <p className="text-xs text-kumo-subtle">
          since <RelativeTime value={warning.brokenSince} />
        </p>
      )}
    </SectionRow>
  );
}

/**
 * The dumps a client hands over for support, folded away: they are files for an issue report rather
 * than something an operator reads on the page, and six buttons at full width would outweigh the
 * warnings above them.
 */
function Diagnostics({ node }: { readonly node: Node }): ReactElement {
  const [running, setRunning] = useState<NodeDiagnosticKind | null>(null);

  async function download(kind: NodeDiagnosticKind): Promise<void> {
    setRunning(kind);

    // No `finally`: the catch swallows the failure, so the last line always runs, and the React
    // Compiler cannot lower a try statement that has one.
    try {
      await downloadFile(nodeDiagnosticUrl(node.id, kind), diagnosticFileName(node.id, kind));
    } catch (error) {
      toast.error(`Could not download the ${diagnosticLabels[kind].toLowerCase()} dump`, error);
    }

    setRunning(null);
  }

  return (
    <Collapsible.Root className="border-t border-kumo-hairline">
      <Collapsible.Trigger className="group flex w-full items-center gap-1.5 px-5 py-2.5 text-left text-kumo-subtle hover:bg-kumo-tint focus-visible:ring-2 focus-visible:ring-kumo-focus focus-visible:outline-none">
        <CaretRightIcon size={caretSize} className="group-data-[panel-open]:rotate-90" />
        Diagnostics
      </Collapsible.Trigger>
      <Collapsible.Panel>
        <div className="flex flex-col gap-2 border-t border-kumo-hairline px-5 py-3">
          <p className="text-kumo-subtle">
            What the client would hand over for a support request, as it writes it.
          </p>
          <div className="flex flex-wrap gap-2">
            {nodeDiagnosticKinds.map((kind) => (
              <Button
                key={kind}
                variant="secondary"
                size="sm"
                disabled={!node.online || (running !== null && running !== kind)}
                loading={running === kind}
                icon={<DownloadSimpleIcon size={buttonIconSize} />}
                onClick={() => {
                  void download(kind);
                }}
              >
                {diagnosticLabels[kind]}
              </Button>
            ))}
          </div>
        </div>
      </Collapsible.Panel>
    </Collapsible.Root>
  );
}
