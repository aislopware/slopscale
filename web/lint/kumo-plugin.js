// Kumo's design-token lint rules as an oxlint JS plugin. Kumo ships them in its
// repository only, so the two rules that apply to a consumer are vendored here.
import { noPrimitiveColorsRule } from "./no-primitive-colors.js";
import { noTailwindDarkVariantRule } from "./no-tailwind-dark-variant.js";

const plugin = {
  meta: {
    name: "kumo",
  },
  rules: {
    "no-primitive-colors": noPrimitiveColorsRule,
    "no-tailwind-dark-variant": noTailwindDarkVariantRule,
  },
};

export default plugin;
