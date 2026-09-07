/** The address a client registers against: whatever origin this console is being served from. */
export function serverUrl(): string {
  return globalThis.location.origin;
}

/**
 * The single command that puts a machine on this tailnet. With a key in hand it needs no further
 * input, so the "Add machine" dialog can hand over a line that is ready to paste; without one the
 * placeholder shows where the key goes.
 */
export function connectCommand(key: string): string {
  return `tailscale up --login-server=${serverUrl()} --authkey=${key}`;
}
