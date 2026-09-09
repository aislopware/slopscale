import { auditExportUrl } from "~/api/queries.ts";
import type { AuditExportFormat, AuditFilters } from "~/api/queries.ts";
import { saveBlob } from "~/lib/download.ts";

/** `slopscale-audit-2026-09-08.csv`: the day the export was taken, so downloads sort by date. */
export function auditExportFileName(format: AuditExportFormat, now: Date = new Date()): string {
  const day = [
    String(now.getFullYear()),
    String(now.getMonth() + 1).padStart(2, "0"),
    String(now.getDate()).padStart(2, "0"),
  ].join("-");

  return `slopscale-audit-${day}.${format}`;
}

/**
 * Downloads the events matching `filters` as a file. The export goes through fetch rather than a
 * plain link so a refused or failed request surfaces as an error the page can show, instead of the
 * browser navigating away to a problem document; the name is the console's, not the server's, since
 * it says which day the export covers.
 */
export async function downloadAuditExport(
  filters: AuditFilters,
  format: AuditExportFormat,
): Promise<void> {
  const response = await fetch(auditExportUrl(filters, format), {
    headers: { accept: format === "csv" ? "text/csv" : "application/json" },
  });

  if (!response.ok) {
    throw new Error(`The server refused the export (${String(response.status)}).`);
  }

  saveBlob(await response.blob(), auditExportFileName(format));
}
