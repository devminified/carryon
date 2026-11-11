// CO-604: bridges react-hook-form's validated, typed object back into
// the FormData shape every existing useActionState action already
// expects — these forms keep submitting through the same Server
// Actions as before, just gated by client-side Zod validation first.
// A boolean becomes 'on' only when true, otherwise omitted entirely —
// matching a native <input type="checkbox"> exactly (an unchecked box
// never appears in a real form's FormData at all), which is what each
// action's own `formData.get('x') === 'on'` check already assumes.
export function buildFormData(
  data: Record<string, string | number | boolean | null | undefined>,
): FormData {
  const formData = new FormData();
  for (const [key, value] of Object.entries(data)) {
    if (value === null || value === undefined) continue;
    if (typeof value === 'boolean') {
      if (value) formData.set(key, 'on');
      continue;
    }
    formData.set(key, String(value));
  }
  return formData;
}
