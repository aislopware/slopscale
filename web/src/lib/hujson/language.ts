import {
  LRLanguage,
  LanguageSupport,
  continuedIndent,
  foldInside,
  foldNodeProp,
  indentNodeProp,
} from "@codemirror/language";
import { styleTags, tags } from "@lezer/highlight";

import { parser } from "~/lib/hujson/parser.gen.ts";

const indentation = indentNodeProp.add({
  Object: continuedIndent({ except: /^\s*\}/v }),
  Array: continuedIndent({ except: /^\s*\]/v }),
});

const folding = foldNodeProp.add({ Object: foldInside, Array: foldInside });

const highlighting = styleTags({
  String: tags.string,
  Number: tags.number,
  "True False": tags.bool,
  Null: tags.null,
  PropertyName: tags.propertyName,
  LineComment: tags.lineComment,
  BlockComment: tags.blockComment,
  "[ ]": tags.squareBracket,
  "{ }": tags.brace,
  ",": tags.separator,
  ":": tags.punctuation,
});

/**
 * HuJSON for CodeMirror: the grammar in hujson.grammar with the highlighting, folding, indentation
 * and bracket rules JSON has, plus the comment tokens JSON lacks.
 */
export const hujsonLanguage = LRLanguage.define({
  name: "hujson",
  parser: parser.configure({ props: [indentation, folding, highlighting] }),
  languageData: {
    closeBrackets: { brackets: ["[", "{", '"'] },
    indentOnInput: /^\s*[\]\}]$/v,
    commentTokens: { line: "//", block: { open: "/*", close: "*/" } },
  },
});

export function hujson(): LanguageSupport {
  return new LanguageSupport(hujsonLanguage);
}
