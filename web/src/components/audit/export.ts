import { auditExportUrl } from "~/api/queries.ts";
import type { AuditExportFormat, AuditFilters } from "~/api/queries.ts";

/** How long the blob URL outlives the click that started the download. */
const revokeDelayMs = 1000;

/** `headscale-audit-2026-09-08.csv`: the day the export was taken, so downloads sort by date. */
export function auditExportFileName(format: AuditExportFormat, now: Date = new Date()): string {
  const day = [
    String(now.getFullYear()),
    String(now.getMonth() + 1).padStart(2, "0"),
    String(now.getDate()).padStart(2, "0"),
  ].join("-");

  return `headscale-audit-${day}.${format}`;
}

/**
 * Downloads the events matching `filters` as a file. The export goes through fetch rather than a
 * plain link so a refused or failed request surfaces as an error the page can show, instead of the
 * browser navigating away to a problem document; the body then reaches the disk through a blob URL,
 * which is revoked as soon as the click is handed over.
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

  const url = URL.createObjectURL(await response.blob());
  const link = document.createElement("a");

  link.href = url;
  link.download = auditExportFileName(format);
  document.body.append(link);
  link.click();
  link.remove();

  // The browser reads the blob after the click returns, so the URL is released on the next turn
  // rather than straight away, which cancels the download it just started.
  setTimeout(() => {
    URL.revokeObjectURL(url);
  }, revokeDelayMs);
}
