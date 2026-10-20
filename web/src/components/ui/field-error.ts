/**
 * The props that put an error on a Kumo Input, Select or InputArea: the message, which Kumo shows
 * under the field, and `aria-invalid`, which it does not set. Screen readers announce the field as
 * invalid only with the attribute, and a form finds its first invalid field by it. Nothing when
 * there is no error, so a field that is fine carries neither.
 */
export function fieldError(
  error: string | undefined,
): { readonly error: string; readonly "aria-invalid": true } | Record<string, never> {
  return error === undefined || error === "" ? {} : { error, "aria-invalid": true };
}
