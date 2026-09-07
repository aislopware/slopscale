import { cn } from "@cloudflare/kumo/utils";
import { indentWithTab } from "@codemirror/commands";
import { json } from "@codemirror/lang-json";
import { HighlightStyle, syntaxHighlighting } from "@codemirror/language";
import { Annotation, Compartment, EditorState } from "@codemirror/state";
import type { Transaction } from "@codemirror/state";
import { EditorView, keymap } from "@codemirror/view";
import { tags } from "@lezer/highlight";
import { basicSetup } from "codemirror";
import { useEffect, useRef } from "react";
import type { ReactElement, RefObject } from "react";

/** Marks the transactions that push the `value` prop into the editor so they do not echo back. */
const fromProp = Annotation.define<boolean>();

/**
 * HuJSON is JSON with comments and trailing commas, so the JSON grammar highlights it well enough.
 * The colours are Kumo's semantic tokens, so the editor follows light and dark mode.
 */
const highlightStyle = HighlightStyle.define([
  { tag: tags.string, color: "var(--text-color-kumo-link)" },
  { tag: tags.number, color: "var(--text-color-kumo-success)" },
  { tag: [tags.bool, tags.null], color: "var(--text-color-kumo-warning)" },
  {
    tag: [tags.propertyName, tags.definition(tags.propertyName)],
    color: "var(--text-color-kumo-default)",
    fontWeight: "500",
  },
  {
    tag: [tags.comment, tags.lineComment, tags.blockComment],
    color: "var(--text-color-kumo-subtle)",
    fontStyle: "italic",
  },
  { tag: [tags.punctuation, tags.separator, tags.bracket], color: "var(--text-color-kumo-subtle)" },
  { tag: tags.invalid, color: "var(--text-color-kumo-danger)" },
]);

const theme = EditorView.theme({
  "&": {
    height: "100%",
    color: "var(--text-color-kumo-default)",
    backgroundColor: "var(--color-kumo-base)",
  },
  "&.cm-focused": { outline: "none" },
  ".cm-scroller": { fontFamily: "var(--font-mono)", fontSize: "0.8125rem", lineHeight: "1.6" },
  ".cm-content": { padding: "0.5rem 0", caretColor: "var(--text-color-kumo-default)" },
  ".cm-gutters": {
    backgroundColor: "var(--color-kumo-recessed)",
    color: "var(--text-color-kumo-subtle)",
    borderRight: "1px solid var(--color-kumo-line)",
  },
  ".cm-activeLine": { backgroundColor: "var(--color-kumo-tint)" },
  ".cm-activeLineGutter": {
    backgroundColor: "var(--color-kumo-tint)",
    color: "var(--text-color-kumo-default)",
  },
  "&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground, .cm-selectionBackground":
    { backgroundColor: "var(--color-kumo-info-tint)" },
  ".cm-cursor": { borderLeftColor: "var(--text-color-kumo-default)" },
  ".cm-matchingBracket": {
    backgroundColor: "var(--color-kumo-info-tint)",
    outline: "1px solid var(--color-kumo-line)",
  },
});

interface EditorOptions {
  readonly doc: string;
  readonly readOnly: boolean;
  readonly label: string;
  readonly emit: RefObject<(next: string) => void>;
}

interface Created {
  readonly editor: EditorView;
  readonly compartment: Compartment;
}

function isFromProp(transaction: Transaction): boolean {
  return transaction.annotation(fromProp) === true;
}

function createEditor(parent: HTMLElement, options: EditorOptions): Created {
  const compartment = new Compartment();
  const listener = EditorView.updateListener.of((update) => {
    if (update.docChanged && !update.transactions.some(isFromProp)) {
      options.emit.current(update.state.doc.toString());
    }
  });
  const extensions = [
    basicSetup,
    json(),
    syntaxHighlighting(highlightStyle),
    keymap.of([indentWithTab]),
    EditorView.lineWrapping,
    EditorView.contentAttributes.of({ "aria-label": options.label }),
    theme,
    compartment.of(EditorState.readOnly.of(options.readOnly)),
    listener,
  ];
  const state = EditorState.create({ doc: options.doc, extensions });

  return { editor: new EditorView({ parent, state }), compartment };
}

export interface CodeEditorProps {
  readonly value: string;
  readonly onChange: (next: string) => void;
  /** Keeps the caret and selection working while blocking edits. */
  readonly readOnly?: boolean;
  readonly className?: string;
  readonly "aria-label": string;
}

/** A controlled CodeMirror 6 editor. It fills its parent, so the parent sets the height. */
export function CodeEditor({
  value,
  onChange,
  readOnly = false,
  className,
  "aria-label": label,
}: CodeEditorProps): ReactElement {
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView>(null);
  const editable = useRef<Compartment>(null);
  const emit = useRef(onChange);
  const initial = useRef({ doc: value, readOnly });

  useEffect(() => {
    emit.current = onChange;
  }, [onChange]);

  useEffect(() => {
    const parent = host.current;
    const created =
      parent === null
        ? null
        : createEditor(parent, {
            doc: initial.current.doc,
            readOnly: initial.current.readOnly,
            label,
            emit,
          });

    view.current = created?.editor ?? null;
    editable.current = created?.compartment ?? null;

    return (): void => {
      created?.editor.destroy();
      view.current = null;
      editable.current = null;
    };
  }, [label]);

  useEffect(() => {
    const editor = view.current;

    if (editor === null || editor.state.doc.toString() === value) {
      return;
    }

    editor.dispatch({
      changes: { from: 0, to: editor.state.doc.length, insert: value },
      annotations: fromProp.of(true),
    });
  }, [value]);

  useEffect(() => {
    const editor = view.current;
    const compartment = editable.current;

    if (editor === null || compartment === null) {
      return;
    }

    editor.dispatch({ effects: compartment.reconfigure(EditorState.readOnly.of(readOnly)) });
  }, [readOnly]);

  return <div ref={host} className={cn("h-full overflow-hidden", className)} />;
}
