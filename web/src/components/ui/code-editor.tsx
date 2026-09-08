import { cn } from "@cloudflare/kumo/utils";
import { closeBrackets, closeBracketsKeymap, completionKeymap } from "@codemirror/autocomplete";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import { HighlightStyle, bracketMatching, syntaxHighlighting } from "@codemirror/language";
import { lintKeymap } from "@codemirror/lint";
import { Annotation, Compartment, EditorState } from "@codemirror/state";
import type { Extension, Transaction } from "@codemirror/state";
import {
  EditorView,
  drawSelection,
  highlightSpecialChars,
  keymap,
  placeholder as placeholderText,
} from "@codemirror/view";
import { tags } from "@lezer/highlight";
import { basicSetup } from "codemirror";
import { useEffect, useRef } from "react";
import type { ReactElement, RefObject } from "react";

import { externalProblems, setProblems } from "~/lib/editor/problems.ts";
import type { EditorProblem } from "~/lib/editor/problems.ts";

/** Marks the transactions that push the `value` prop into the editor so they do not echo back. */
const fromProp = Annotation.define<boolean>();

/**
 * The colours are Kumo's semantic tokens, so the editor follows light and dark mode. The tags are
 * the ones the HuJSON and posture languages emit; a language that emits others gets the default.
 */
const highlightStyle = HighlightStyle.define([
  { tag: tags.string, color: "var(--text-color-kumo-link)" },
  { tag: tags.number, color: "var(--text-color-kumo-success)" },
  { tag: [tags.bool, tags.null], color: "var(--text-color-kumo-warning)" },
  {
    tag: [tags.propertyName, tags.definition(tags.propertyName), tags.variableName],
    color: "var(--text-color-kumo-default)",
    fontWeight: "500",
  },
  { tag: tags.namespace, color: "var(--text-color-kumo-subtle)" },
  { tag: tags.keyword, color: "var(--text-color-kumo-default)", fontWeight: "600" },
  {
    tag: [tags.comment, tags.lineComment, tags.blockComment],
    color: "var(--text-color-kumo-subtle)",
    fontStyle: "italic",
  },
  { tag: [tags.punctuation, tags.separator, tags.bracket], color: "var(--text-color-kumo-subtle)" },
  {
    tag: tags.invalid,
    color: "var(--text-color-kumo-danger)",
    textDecoration: "underline wavy",
    textUnderlineOffset: "3px",
  },
]);

const tooltipShadow = "0 4px 12px var(--color-kumo-shadow-drop), 0 0 0 1px var(--color-kumo-line)";

const theme = EditorView.theme({
  "&": {
    height: "100%",
    color: "var(--text-color-kumo-default)",
    backgroundColor: "var(--color-kumo-base)",
  },
  "&.cm-minimal": { height: "auto", maxHeight: "16rem" },
  "&.cm-focused": { outline: "none" },
  ".cm-scroller": {
    overflow: "auto",
    fontFamily: "var(--font-mono)",
    fontSize: "0.8125rem",
    lineHeight: "1.6",
  },
  ".cm-content": { padding: "0.5rem 0", caretColor: "var(--text-color-kumo-default)" },
  "&.cm-minimal .cm-content": { padding: "0.5rem 0.75rem", minHeight: "4.6rem" },
  "&.cm-minimal .cm-line": { padding: "0" },
  ".cm-gutters": {
    backgroundColor: "var(--color-kumo-recessed)",
    color: "var(--text-color-kumo-subtle)",
    borderRight: "1px solid var(--color-kumo-line)",
  },
  ".cm-activeLine": { backgroundColor: "var(--color-kumo-tint)" },
  ".cm-placeholder": { color: "var(--text-color-kumo-subtle)" },
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
  // Diagnostics: the gutter dot, the underline and the message that opens over it.
  ".cm-lintRange-error": {
    backgroundImage: "none",
    textDecoration: "underline wavy var(--color-kumo-danger)",
    textUnderlineOffset: "3px",
  },
  ".cm-lintRange-warning": {
    backgroundImage: "none",
    textDecoration: "underline wavy var(--color-kumo-warning)",
    textUnderlineOffset: "3px",
  },
  ".cm-lintRange-info": {
    backgroundImage: "none",
    textDecoration: "underline dotted var(--color-kumo-info)",
    textUnderlineOffset: "3px",
  },
  ".cm-lintPoint-error::after": { borderBottomColor: "var(--color-kumo-danger)" },
  ".cm-lintPoint-warning::after": { borderBottomColor: "var(--color-kumo-warning)" },
  ".cm-gutter-lint": { width: "1.1em" },
  ".cm-gutter-lint .cm-gutterElement": { padding: "0 0.1em" },
  ".cm-tooltip": {
    border: "none",
    borderRadius: "0.5rem",
    backgroundColor: "var(--color-kumo-base)",
    color: "var(--text-color-kumo-default)",
    boxShadow: tooltipShadow,
    overflow: "hidden",
  },
  ".cm-tooltip.cm-tooltip-hover, .cm-tooltip-lint": {
    fontFamily: "var(--font-sans)",
    fontSize: "0.8125rem",
    maxWidth: "28rem",
  },
  ".cm-diagnostic": { padding: "0.375rem 0.625rem 0.375rem 0.75rem", marginLeft: "0" },
  ".cm-diagnostic-error": { borderLeft: "3px solid var(--color-kumo-danger)" },
  ".cm-diagnostic-warning": { borderLeft: "3px solid var(--color-kumo-warning)" },
  ".cm-diagnostic-info": { borderLeft: "3px solid var(--color-kumo-info)" },
  ".cm-tooltip.cm-tooltip-autocomplete > ul": {
    fontFamily: "var(--font-mono)",
    fontSize: "0.8125rem",
    maxHeight: "16rem",
  },
  ".cm-tooltip.cm-tooltip-autocomplete > ul > li": {
    padding: "0.25rem 0.625rem",
    lineHeight: "1.5",
  },
  ".cm-tooltip.cm-tooltip-autocomplete > ul > li[aria-selected]": {
    backgroundColor: "var(--color-kumo-tint)",
    color: "var(--text-color-kumo-default)",
  },
  ".cm-completionDetail": {
    color: "var(--text-color-kumo-subtle)",
    fontStyle: "normal",
    marginLeft: "0.75rem",
  },
  ".cm-completionMatchedText": { textDecoration: "none", fontWeight: "600" },
  ".cm-tooltip.cm-completionInfo": {
    fontFamily: "var(--font-sans)",
    fontSize: "0.8125rem",
    padding: "0.5rem 0.625rem",
    maxWidth: "20rem",
  },
});

/**
 * What a field-sized editor needs and nothing more: no gutters, no line numbers, no active line, so
 * it reads like an input that happens to colour its text.
 */
const minimalSetup: Extension = [
  history(),
  drawSelection(),
  highlightSpecialChars(),
  closeBrackets(),
  bracketMatching(),
  EditorView.editorAttributes.of({ class: "cm-minimal" }),
  keymap.of([
    ...closeBracketsKeymap,
    ...defaultKeymap,
    ...historyKeymap,
    ...completionKeymap,
    ...lintKeymap,
  ]),
];

interface EditorOptions {
  readonly doc: string;
  readonly readOnly: boolean;
  readonly label: string;
  /** Shown while the document is empty; "" for none. */
  readonly placeholder: string;
  readonly minimal: boolean;
  readonly extensions: Extension;
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
    options.minimal ? minimalSetup : basicSetup,
    options.extensions,
    externalProblems(),
    syntaxHighlighting(highlightStyle),
    keymap.of([indentWithTab]),
    EditorView.lineWrapping,
    EditorView.contentAttributes.of({ "aria-label": options.label }),
    theme,
    compartment.of(EditorState.readOnly.of(options.readOnly)),
    listener,
  ];

  if (options.placeholder !== "") {
    extensions.push(placeholderText(options.placeholder));
  }

  const state = EditorState.create({ doc: options.doc, extensions });

  return { editor: new EditorView({ parent, state }), compartment };
}

export interface CodeEditorProps {
  readonly value: string;
  readonly onChange: (next: string) => void;
  /** Keeps the caret and selection working while blocking edits. */
  readonly readOnly?: boolean;
  /** A snippet shown while the document is empty, read once when the editor is created. */
  readonly placeholder?: string;
  /** The language, its linters and completions, read once when the editor is created. */
  readonly extensions?: Extension;
  /** Drops the gutters and the active line: an editor the size of a field, for a few lines. */
  readonly minimal?: boolean;
  /** Problems found outside the editor, shown as diagnostics and moved along with later edits. */
  readonly problems?: readonly EditorProblem[];
  readonly className?: string;
  readonly "aria-label": string;
}

const noProblems: readonly EditorProblem[] = [];
const noExtensions: Extension = [];

/** A controlled CodeMirror 6 editor. It fills its parent, so the parent sets the height. */
export function CodeEditor({
  value,
  onChange,
  readOnly = false,
  placeholder = "",
  extensions = noExtensions,
  minimal = false,
  problems = noProblems,
  className,
  "aria-label": label,
}: CodeEditorProps): ReactElement {
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView>(null);
  const editable = useRef<Compartment>(null);
  const emit = useRef(onChange);
  const initial = useRef({ doc: value, readOnly, placeholder, minimal, extensions });

  useEffect(() => {
    emit.current = onChange;
  }, [onChange]);

  useEffect(() => {
    const parent = host.current;
    const created =
      parent === null ? null : createEditor(parent, { ...initial.current, label, emit });

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

  useEffect(() => {
    view.current?.dispatch({ effects: setProblems.of(problems) });
  }, [problems]);

  return (
    <div ref={host} className={cn(minimal ? "min-w-0" : "h-full overflow-hidden", className)} />
  );
}
