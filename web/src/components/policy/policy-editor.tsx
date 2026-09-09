import { Button } from "@cloudflare/kumo/components/button";
import { EditorView } from "@codemirror/view";
import {
  CheckCircleIcon,
  FileCodeIcon,
  WarningCircleIcon,
  WarningIcon,
} from "@phosphor-icons/react";
import { useImperativeHandle, useMemo, useRef } from "react";
import type { ReactElement, ReactNode, Ref } from "react";

import { policyLanguage } from "~/components/policy/policy-language.ts";
import { CodeEditor } from "~/components/ui/code-editor.tsx";
import { Frame, FrameBand, FramePanel } from "~/components/ui/frame.tsx";
import type { EditorProblem } from "~/lib/editor/problems.ts";

const iconSize = 14;

/**
 * What an empty editor shows: the shape of a policy, so a first-time file starts from something
 * rather than from a blank surface. "Start from a template" fills in the full version.
 */
const starterSnippet = `{
  // HuJSON is JSON with comments and trailing commas.
  "groups": {
    "group:admin": ["alice@example.com"],
  },
  "grants": [
    { "src": ["group:admin"], "dst": ["*"], "ip": ["*"] },
  ],
}`;

/**
 * The editor follows the window: 70vh, and never under 400px, so a laptop shows the file without
 * the page scrolling and a short window still has room to type in.
 */
const editorHeightClass = "h-[70vh] min-h-[400px]";

function lineCount(text: string): number {
  return text === "" ? 0 : text.split("\n").length;
}

/** What the page can ask the editor to do beyond changing its text. */
export interface PolicyEditorHandle {
  /** Selects the first occurrence of `text` and scrolls it into view; no-op when it is absent. */
  reveal: (text: string) => void;
}

/**
 * CodeMirror owns the DOM inside the host, so the view is found through it rather than through a
 * prop: `~/components/ui/code-editor.tsx` stays a plain controlled textarea from the outside.
 */
function reveal(host: HTMLElement | null, text: string): void {
  const view = host === null ? null : EditorView.findFromDOM(host);

  if (view === null) {
    return;
  }

  const start = view.state.doc.toString().indexOf(text);

  if (start === -1) {
    return;
  }

  view.dispatch({
    selection: { anchor: start, head: start + text.length },
    scrollIntoView: true,
  });
  view.focus();
}

export interface PolicyEditorProps {
  readonly value: string;
  readonly onChange: (next: string) => void;
  readonly readOnly: boolean;
  /** Shows the Discard action; the draft differs from the stored policy. */
  readonly dirty: boolean;
  readonly onDiscard: () => void;
  /** What is wrong with the draft, from the linter and the server, shown in the gutter and here. */
  readonly problems: readonly EditorProblem[];
  /** The draft is with the server for its quiet check. */
  readonly verifying: boolean;
  /** The users of the tailnet as the policy names them, offered where a user fits. */
  readonly users: readonly string[];
  readonly ref?: Ref<PolicyEditorHandle>;
}

function count(number: number, noun: string): string {
  return `${number} ${noun}${number === 1 ? "" : "s"}`;
}

/** The state of the draft in a few words: what is wrong, or that nothing is. */
function ProblemsSummary({
  problems,
  verifying,
}: {
  readonly problems: readonly EditorProblem[];
  readonly verifying: boolean;
}): ReactNode {
  const errors = problems.filter((problem) => problem.severity === "error").length;
  const warnings = problems.filter((problem) => problem.severity === "warning").length;

  if (errors > 0) {
    return (
      <span className="flex items-center gap-1 text-kumo-danger">
        <WarningCircleIcon size={iconSize} weight="fill" aria-hidden />
        {count(errors, "error")}
        {warnings > 0 ? `, ${count(warnings, "warning")}` : null}
      </span>
    );
  }

  if (warnings > 0) {
    return (
      <span className="flex items-center gap-1 text-kumo-warning">
        <WarningIcon size={iconSize} weight="fill" aria-hidden />
        {count(warnings, "warning")}
      </span>
    );
  }

  if (verifying) {
    return <span>Checking…</span>;
  }

  return (
    <span className="flex items-center gap-1">
      <CheckCircleIcon size={iconSize} weight="fill" className="text-kumo-success" aria-hidden />
      No problems
    </span>
  );
}

/**
 * The HuJSON editor on its own surface, under a slim toolbar that names the file the policy would
 * be on disk and counts its lines, so the card reads as an editor rather than another panel.
 */
export function PolicyEditor({
  value,
  onChange,
  readOnly,
  dirty,
  onDiscard,
  problems,
  verifying,
  users,
  ref,
}: PolicyEditorProps): ReactElement {
  const lines = lineCount(value);
  const host = useRef<HTMLDivElement>(null);
  // The users are read once, when the editor is created; a list that arrives later is missed, so
  // the editor waits for the query before it mounts (the page does this by rendering it after).
  const extensions = useMemo(() => policyLanguage(users), [users]);

  useImperativeHandle(ref, () => ({
    reveal: (text: string): void => {
      reveal(host.current, text);
    },
  }));

  return (
    <Frame>
      <FrameBand className="flex items-center justify-between gap-3 px-5 text-sm text-kumo-subtle">
        <div className="flex min-w-0 items-center gap-2">
          <span className="flex h-lh items-center">
            <FileCodeIcon size={iconSize} aria-hidden />
          </span>
          <span className="truncate font-mono text-kumo-default">policy.hujson</span>
          <span aria-hidden>·</span>
          <span>{lines === 1 ? "1 line" : `${lines} lines`}</span>
          {readOnly ? (
            <>
              <span aria-hidden>·</span>
              <span>Read only</span>
            </>
          ) : null}
          {value.trim() === "" ? null : (
            <>
              <span aria-hidden>·</span>
              <span aria-live="polite">
                <ProblemsSummary problems={problems} verifying={verifying} />
              </span>
            </>
          )}
        </div>
        {dirty ? (
          <Button variant="ghost" size="xs" onClick={onDiscard}>
            Discard
          </Button>
        ) : null}
      </FrameBand>
      <FramePanel>
        <div ref={host} className={editorHeightClass}>
          <CodeEditor
            value={value}
            onChange={onChange}
            readOnly={readOnly}
            placeholder={readOnly ? "" : starterSnippet}
            extensions={extensions}
            problems={problems}
            aria-label="Tailnet policy"
          />
        </div>
      </FramePanel>
    </Frame>
  );
}
