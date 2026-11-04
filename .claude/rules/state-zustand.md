# Client State

Carryon's web app is small (session list, session viewer, admin pages) — most screens need nothing beyond `useState`. Reach for Zustand only when state is genuinely shared across routes or across many components, per the directory default.

## Use Zustand for

- **UI state shared across routes** — e.g. a sidebar collapsed state, or a project filter that should persist while navigating between the sessions and admin pages.
- **Multi-step flows** — none exist yet; if one is added (e.g. a guided "register a project" wizard), it belongs in `modules/{module}/store.ts`.
- **Bulk-action selection** — selected session IDs across a paginated table, for a future bulk-delete admin action.

## Do NOT use Zustand for

- **Server data** (session rows, project rows) — that's Server Component props, or TanStack Query only where `data-fetching.md` says it's warranted.
- **Form state** — react-hook-form, or `useActionState` for simple forms.
- **Local component state** — `useState`.
- **Auth/session** — Supabase Auth + the helper in `auth.md`.
- **A pulled transcript's content** — never held in a client store; it's rendered from what the viewer fetched for that one session, sanitized (see `CLAUDE.md`'s anti-pattern on raw HTML).

## Pattern

```ts
// shared/store/ui-store.ts
import { create } from 'zustand';
import { persist } from 'zustand/middleware';

type UIState = {
  sidebarCollapsed: boolean;
  toggleSidebar: () => void;
  reset: () => void;
};

export const useUIStore = create<UIState>()(
  persist(
    (set) => ({
      sidebarCollapsed: false,
      toggleSidebar: () => set((s) => ({ sidebarCollapsed: !s.sidebarCollapsed })),
      reset: () => set({ sidebarCollapsed: false }),
    }),
    { name: 'carryon-ui' },
  ),
);
```

## Rules

- Use **selectors** in components (`useUIStore((s) => s.sidebarCollapsed)`). Never destructure the whole store.
- Persist only safe UI preferences — never a session's content, a signed URL, or a token.
- Every store exposes `reset()`; the auth flow calls it on every store at logout.
