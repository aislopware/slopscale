import { describe, expect, it } from "vitest";

import { shellTokens } from "~/lib/shell.ts";

/** Each token as `kind:text`, skipping spaces, so a whole line reads on one line of the test. */
function words(command: string): string[] {
  return shellTokens(command)
    .filter((token) => token.kind !== "space")
    .map((token) => `${token.kind}:${token.text}`);
}

describe(shellTokens, () => {
  it("tells the program, its flags and their values apart", () => {
    expect(words("tailscale up --login-server=http://hs.example --authkey=<key>")).toStrictEqual([
      "command:tailscale",
      "arg:up",
      "flag:--login-server",
      "assign:=",
      "value:http://hs.example",
      "flag:--authkey",
      "assign:=",
      "placeholder:<key>",
    ]);
  });

  it("starts a new command after a pipe, && or ; and after sudo", () => {
    expect(words("curl -fsSL https://x | sh && sudo tailscale up; tailscale status")).toStrictEqual(
      [
        "command:curl",
        "flag:-fsSL",
        "arg:https://x",
        "operator:|",
        "command:sh",
        "operator:&&",
        "command:sudo",
        "command:tailscale",
        "arg:up",
        "operator:;",
        "command:tailscale",
        "arg:status",
      ],
    );
  });

  it("reads environment assignments before and after the command", () => {
    expect(
      words(
        "FOO=1 docker run -e TS_AUTHKEY=secret -e TS_EXTRA_ARGS=--login-server=http://x tailscale/tailscale",
      ),
    ).toStrictEqual([
      "name:FOO",
      "assign:=",
      "value:1",
      "command:docker",
      "arg:run",
      "flag:-e",
      "name:TS_AUTHKEY",
      "assign:=",
      "value:secret",
      "flag:-e",
      "name:TS_EXTRA_ARGS",
      "assign:=",
      "value:--login-server=http://x",
      "arg:tailscale/tailscale",
    ]);
  });

  it("lets sudo take options before the program", () => {
    expect(words("sudo -E -u root tailscale up")).toStrictEqual([
      "command:sudo",
      "flag:-E",
      "flag:-u",
      "command:root",
      "arg:tailscale",
      "arg:up",
    ]);
  });

  it("keeps a redirection apart from the words around it", () => {
    expect(words("make build 2>&1 | tee log >> all.log")).toStrictEqual([
      "command:make",
      "arg:build",
      "operator:2>&1",
      "operator:|",
      "command:tee",
      "arg:log",
      "operator:>>",
      "arg:all.log",
    ]);
  });

  it("keeps a quoted word whole, operators and all", () => {
    expect(words(`sh -c "a | b && c" 'd;e' -`)).toStrictEqual([
      "command:sh",
      "flag:-c",
      `arg:"a | b && c"`,
      "arg:'d;e'",
      "flag:-",
    ]);
  });

  it("keeps a line continuation and the newline it precedes", () => {
    expect(shellTokens("a \\\n  b").map((token) => token.kind)).toStrictEqual([
      "command",
      "space",
      "operator",
      "space",
      "arg",
    ]);
  });

  it("gives every character back, in order", () => {
    const command = `winget install --id Tailscale.Tailscale -e; echo "a b" 'c'`;

    expect(
      shellTokens(command)
        .map((token) => token.text)
        .join(""),
    ).toBe(command);
  });
});
