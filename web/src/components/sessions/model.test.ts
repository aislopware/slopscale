import { describe, expect, it } from "vitest";

import type { SSHRecording } from "~/api/queries.ts";
import {
  castFileName,
  formatBytes,
  recordingState,
  sessionSource,
  sessionTarget,
} from "~/components/sessions/model.ts";

function recording(overrides: Partial<SSHRecording> = {}): SSHRecording {
  return {
    id: "7",
    startedAt: "2026-09-07T08:00:00Z",
    endedAt: "2026-09-07T08:05:00Z",
    srcNode: "laptop",
    srcNodeId: "n1",
    srcUser: "alice@example.com",
    dstNodeId: "3",
    dstNode: "prod-db",
    sshUser: "ubuntu",
    localUser: "ubuntu",
    command: "",
    size: 2048,
    complete: true,
    ...overrides,
  };
}

describe(formatBytes, () => {
  it("keeps whole bytes below a kilobyte and scales above", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(512)).toBe("512 B");
    expect(formatBytes(2048)).toBe("2.0 KB");
    expect(formatBytes(3 * 1024 * 1024)).toBe("3.0 MB");
  });
});

describe(recordingState, () => {
  it("tells a running upload from a finished and a broken one", () => {
    expect(recordingState(recording({ endedAt: null }))).toBe("recording");
    expect(recordingState(recording())).toBe("complete");
    expect(recordingState(recording({ complete: false }))).toBe("interrupted");
  });
});

describe("session labels", () => {
  it("names the user on the machine, or the machine alone when tagged", () => {
    expect(sessionSource(recording())).toBe("alice@example.com on laptop");
    expect(sessionSource(recording({ srcUser: "" }))).toBe("laptop");
  });

  it("names the account on the node, or the account alone when the node is unknown", () => {
    expect(sessionTarget(recording())).toBe("ubuntu@prod-db");
    expect(sessionTarget(recording({ dstNode: "" }))).toBe("ubuntu");
  });

  it("downloads under the recording's id", () => {
    expect(castFileName(recording())).toBe("ssh-recording-7.cast");
  });
});
