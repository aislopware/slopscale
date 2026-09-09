/** What a word does in a shell command line, so a display can colour it. */
export type ShellTokenKind =
  /** The program that runs: the first word of a pipeline segment, and the one after sudo. */
  | "command"
  /** A flag such as -v or --login-server, up to its `=`. */
  | "flag"
  /** A variable name before `=`, such as TS_AUTHKEY. */
  | "name"
  /** The `=` between a flag or a name and its value. */
  | "assign"
  /** What follows the `=`. */
  | "value"
  /** Text in angle brackets that the reader replaces, such as <key>. */
  | "placeholder"
  /** Any other word. */
  | "arg"
  /** A pipe, `&&`, `;` or a line continuation. */
  | "operator"
  | "space";

export interface ShellToken {
  readonly kind: ShellTokenKind;
  readonly text: string;
  /** The offset in the command, unique per token, for a React key. */
  readonly from: number;
}

const space = /\s+/vy;
const operator = /\|\||&&|[\|;]|\\(?=\n)/vy;
/** A word ends at whitespace or an operator; a quoted run keeps both inside it. */
const word = /(?:"[^"]*"|'[^']*'|[^\s\|;\u0026"'])+/vy;
const assignment = /^(?<name>[A-Za-z_]\w*)=(?<value>.*)$/sv;
const flag = /^(?<flag>-[^=]*)(?:=(?<value>.*))?$/sv;
const placeholder = /^<[^<>]+>$/v;

function at(pattern: RegExp, text: string, from: number): string | null {
  pattern.lastIndex = from;

  return pattern.exec(text)?.[0] ?? null;
}

/** A value or a bare word, which is a placeholder when it is written as one. */
function wordKind(text: string, otherwise: ShellTokenKind): ShellTokenKind {
  return placeholder.test(text) ? "placeholder" : otherwise;
}

/** Splits `name=value` and `--flag=value` into their three tokens. */
function split(
  head: { readonly kind: ShellTokenKind; readonly text: string },
  value: string | undefined,
  from: number,
): ShellToken[] {
  const tokens: ShellToken[] = [{ kind: head.kind, text: head.text, from }];

  if (value === undefined) {
    return tokens;
  }

  const assignAt = from + head.text.length;

  tokens.push({ kind: "assign", text: "=", from: assignAt });

  if (value !== "") {
    tokens.push({ kind: wordKind(value, "value"), text: value, from: assignAt + 1 });
  }

  return tokens;
}

/** Classifies one word, given whether the next word is where a command is expected. */
function classify(
  text: string,
  from: number,
  expectCommand: boolean,
): { readonly tokens: ShellToken[]; readonly expectCommand: boolean } {
  const name = assignment.exec(text)?.groups?.["name"];

  if (name !== undefined) {
    // An environment prefix leaves the command still to come.
    return {
      tokens: split({ kind: "name", text: name }, text.slice(name.length + 1), from),
      expectCommand,
    };
  }

  if (expectCommand) {
    return { tokens: [{ kind: "command", text, from }], expectCommand: text === "sudo" };
  }

  const flagged = flag.exec(text)?.groups?.["flag"];

  if (flagged !== undefined) {
    const value = text.length > flagged.length ? text.slice(flagged.length + 1) : undefined;

    return { tokens: split({ kind: "flag", text: flagged }, value, from), expectCommand };
  }

  return { tokens: [{ kind: wordKind(text, "arg"), text, from }], expectCommand };
}

interface Step {
  readonly tokens: ShellToken[];
  readonly expectCommand: boolean;
}

/** The tokens starting at `from`: a run of blanks, an operator or one classified word. */
function step(command: string, from: number, expectCommand: boolean): Step {
  const blank = at(space, command, from);

  if (blank !== null) {
    return { tokens: [{ kind: "space", text: blank, from }], expectCommand };
  }

  const op = at(operator, command, from);

  if (op !== null) {
    return { tokens: [{ kind: "operator", text: op, from }], expectCommand: op !== "\\" };
  }

  // Anything else is a word, since a word may hold any character the first two do not claim.
  return classify(at(word, command, from) ?? command.slice(from, from + 1), from, expectCommand);
}

/**
 * Splits a shell command line into words and says what each one does. It reads the shapes the
 * console hands out (pipes, `&&`, `;`, sudo, environment assignments, `--flag=value`, a `\` at the
 * end of a line) and does not try to be a shell: quoting only keeps a word together.
 */
export function shellTokens(command: string): ShellToken[] {
  const tokens: ShellToken[] = [];
  let expectCommand = true;
  let from = 0;

  while (from < command.length) {
    const next = step(command, from, expectCommand);

    tokens.push(...next.tokens);
    ({ expectCommand } = next);
    from = next.tokens.reduce((end, token) => Math.max(end, token.from + token.text.length), from);
  }

  return tokens;
}
