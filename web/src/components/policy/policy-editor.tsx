import { Button } from "@cloudflare/kumo/components/button";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { EditorView } from "@codemirror/view";
import { FileCodeIcon } from "@phosphor-icons/react";
import { useImperativeHandle, useRef } from "react";
import type { ReactElement, Ref } from "react";

import { CodeEditor } from "~/components/ui/code-editor.tsx";

const iconSize = 14;

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
    <LayerCard className="flex min-w-0 flex-col overflow-hidden p-0">
      <div className="flex items-center justify-between gap-3 border-b border-kumo-line bg-kumo-recessed px-3 py-2 text-xs text-kumo-subtle">
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
      </div>
      <div ref={host} className="h-[60vh] min-h-96">
        <CodeEditor
          value={value}
          onChange={onChange}
          readOnly={readOnly}
          aria-label="Tailnet policy"
        />
      </div>
    </LayerCard>
  );
}
