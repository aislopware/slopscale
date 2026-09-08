import { Button } from "@cloudflare/kumo/components/button";
import { EditorView } from "@codemirror/view";
import { FileCodeIcon } from "@phosphor-icons/react";
import { useImperativeHandle, useRef } from "react";
import type { ReactElement, Ref } from "react";

import { CodeEditor } from "~/components/ui/code-editor.tsx";
import { Frame, FrameBand, FramePanel } from "~/components/ui/frame.tsx";

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
  readonly ref?: Ref<PolicyEditorHandle>;
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
  ref,
}: PolicyEditorProps): ReactElement {
  const lines = lineCount(value);
  const host = useRef<HTMLDivElement>(null);

  useImperativeHandle(ref, () => ({
    reveal: (text: string): void => {
      reveal(host.current, text);
    },
  }));

  return (
    <Frame>
      <FrameBand className="flex items-center justify-between gap-3 text-xs text-kumo-subtle">
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
            aria-label="Tailnet policy"
          />
        </div>
      </FramePanel>
    </Frame>
  );
}
