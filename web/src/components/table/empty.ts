/**
 * Kumo's Empty inside a table card. The card already draws the edge, so the empty state drops its
 * own, and the title steps down to body size so an empty table reads like the rows it replaces.
 * Kumo nests the title's h2 in a wrapper, so the size reaches it as a descendant, not a child.
 */
export const tableEmptyClass = "border-none bg-kumo-base [&_h2]:text-base";
