/**
 * The users page validates its search parameters, so a typed link has to name them even when it
 * means "no filter". Kept in one place so a change to that page's schema lands once.
 */
export const allUsers = { q: undefined, filter: undefined };
