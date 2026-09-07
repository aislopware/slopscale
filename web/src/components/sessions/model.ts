import type { SSHRecording } from "~/api/queries.ts";

const kilo = 1024;
const units = ["B", "KB", "MB", "GB"] as const;

/** "12.4 KB"; whole bytes below a kilobyte. */
export function formatBytes(size: number): string {
  let value = size;
  let unit = 0;

  while (value >= kilo && unit < units.length - 1) {
    value /= kilo;
    unit += 1;
  }

  const text = unit === 0 ? String(Math.round(value)) : value.toFixed(1);

  return `${text} ${units[unit] ?? "B"}`;
}

export type RecordingState = "recording" | "complete" | "interrupted";

/** Whether the upload is still running, ended cleanly, or broke off. */
export function recordingState(recording: SSHRecording): RecordingState {
  if (recording.endedAt === null || recording.endedAt === undefined) {
    return "recording";
  }

  return recording.complete ? "complete" : "interrupted";
}

/** Who opened the session: the user on the machine, or the machine alone when tagged. */
export function sessionSource(recording: SSHRecording): string {
  return recording.srcUser === ""
    ? recording.srcNode
    : `${recording.srcUser} on ${recording.srcNode}`;
}

/** Where it ran: "ubuntu@prod-db", or the SSH user alone when the node is unknown. */
export function sessionTarget(recording: SSHRecording): string {
  return recording.dstNode === "" ? recording.sshUser : `${recording.sshUser}@${recording.dstNode}`;
}

/** The session's file name when downloaded. */
export function castFileName(recording: SSHRecording): string {
  return `ssh-recording-${recording.id}.cast`;
}
