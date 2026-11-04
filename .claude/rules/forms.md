# Forms (`web/`)

- Schema lives in `schemas.ts` (Zod). Validation logic never lives in the UI file.
- For most forms (register a project, admin settings): **react-hook-form** + `zodResolver` + shadcn's `<Form>` primitives, submitting through a TanStack Query mutation or a Server Action.
- For simple one-field forms (rename a session, consent/policy acknowledgment): **`useActionState`** + a Server Action — see `react-19-hooks.md`.
- Forms that need a lookup list (e.g. a project picker when renaming across projects) receive that list as a prop from the Server Component that wraps them — don't fetch lookup lists from inside a `'use client'` form.
- The consent/policy acknowledgment form (Doc §"Tell people first") is a `useActionState` form that writes a timestamped row, not a client-only localStorage flag — it must survive across devices and be auditable.
