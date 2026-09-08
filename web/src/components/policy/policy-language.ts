import { autocompletion } from "@codemirror/autocomplete";
import { lintGutter } from "@codemirror/lint";
import type { Extension } from "@codemirror/state";
import { EditorView } from "@codemirror/view";

import { completePolicy, policyUsers } from "~/components/policy/complete.ts";
import { policyHover } from "~/components/policy/hover.ts";
import { hujson, hujsonLanguage } from "~/lib/hujson/language.ts";

const hoverTheme = EditorView.theme({
  ".cm-tooltip .cm-policy-hover": {
    padding: "0.5rem 0.625rem",
    fontFamily: "var(--font-sans)",
    fontSize: "0.8125rem",
    lineHeight: "1.5",
    maxWidth: "24rem",
    whiteSpace: "pre-wrap",
  },
  ".cm-policy-hover-name": {
    fontFamily: "var(--font-mono)",
    fontWeight: "500",
    marginBottom: "0.125rem",
  },
});

/**
 * The policy file as a language: HuJSON, a gutter for the problems the linter and the server find,
 * completions for sections, keys and names, and a hover that explains them. The problems themselves
 * come in through the editor's `problems`, so the linter and the server share one list.
 */
export function policyLanguage(users: readonly string[]): Extension {
  return [
    hujson(),
    lintGutter(),
    hujsonLanguage.data.of({ autocomplete: completePolicy }),
    autocompletion({ icons: false }),
    policyUsers.of(users),
    policyHover,
    hoverTheme,
  ];
}
