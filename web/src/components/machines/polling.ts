/**
 * A machine's connection state changes on its own, so the list and a machine's page ask again every
 * so often instead of waiting for a reload. Only while the tab is in front: a console left open in
 * a background tab has nobody reading it.
 */
export const machinePollMs = 15_000;

export const machinePolling = {
  refetchInterval: machinePollMs,
  refetchIntervalInBackground: false,
} as const;
