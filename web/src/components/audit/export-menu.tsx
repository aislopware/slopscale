import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { DownloadSimpleIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import { auditExportFormats } from "~/api/queries.ts";
import type { AuditExportFormat, AuditFilters } from "~/api/queries.ts";
import { downloadAuditExport } from "~/components/audit/export.ts";
import { toast } from "~/components/ui/toast.ts";

const formatLabels: Record<AuditExportFormat, string> = { csv: "CSV", json: "JSON" };

/**
 * Downloads the events the filters select, in either format. The export carries the filters on
 * screen rather than the page loaded so far, so it reaches past "Load more".
 */
export function ExportMenu({ filters }: { readonly filters: AuditFilters }): ReactElement {
  const [running, setRunning] = useState(false);

  async function run(format: AuditExportFormat): Promise<void> {
    setRunning(true);

    // No `finally`: the catch swallows the failure, so the last line always runs, and the React
    // Compiler cannot lower a try statement that has one.
    try {
      await downloadAuditExport(filters, format);
    } catch (error) {
      toast.error("Export failed", error);
    }

    setRunning(false);
  }

  return (
    <DropdownMenu>
      <DropdownMenu.Trigger
        render={
          <Button variant="secondary" icon={DownloadSimpleIcon} loading={running}>
            Export
          </Button>
        }
      />
      <DropdownMenu.Content align="end">
        {auditExportFormats.map((format) => (
          <DropdownMenu.Item
            key={format}
            onClick={() => {
              void run(format);
            }}
          >
            {formatLabels[format]}
          </DropdownMenu.Item>
        ))}
      </DropdownMenu.Content>
    </DropdownMenu>
  );
}
