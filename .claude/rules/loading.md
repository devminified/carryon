# Loading States — Suspense + Skeletons (`web/`)

Same rule as the directory default: anywhere data is fetched, the user sees a layout-matching skeleton — never a bare spinner, never a "Loading…" string.

## Route-level — `loading.tsx`

```tsx
// app/(dashboard)/sessions/loading.tsx
import { SessionTableSkeleton } from '@/modules/sessions/ui/session-table-skeleton';

export default function Loading() {
  return <SessionTableSkeleton />;
}
```

## Streamed sub-trees — explicit `<Suspense>`

```tsx
// app/(dashboard)/sessions/[id]/page.tsx
import { Suspense } from 'react';
import { SessionTranscript } from '@/modules/sessions/ui/session-transcript';
import { SessionTranscriptSkeleton } from '@/modules/sessions/ui/session-transcript-skeleton';
import { CodeSnapshotPanel } from '@/modules/sessions/ui/code-snapshot-panel';
import { CodeSnapshotPanelSkeleton } from '@/modules/sessions/ui/code-snapshot-panel-skeleton';

export default function SessionDetailPage({ params }: { params: Promise<{ id: string }> }) {
  return (
    <div className="space-y-6">
      <Suspense fallback={<SessionTranscriptSkeleton />}>
        <SessionTranscript params={params} />
      </Suspense>
      <Suspense fallback={<CodeSnapshotPanelSkeleton />}>
        <CodeSnapshotPanel params={params} />
      </Suspense>
    </div>
  );
}
```

The transcript panel is the one place worth isolating: a long session's manifest fetch can be slower than the lightweight project/session metadata, so let the shell render immediately.

## Client-side needs

If `data-fetching.md`'s live-status polling is in use, wrap `useSuspenseQuery` with both a `<Suspense>` **and** an `<ErrorBoundary>` — without the error boundary, a thrown error (e.g. the status endpoint 500s) crashes the whole page instead of just that widget.

## Skeleton primitive

shadcn's `<Skeleton>` as the building block, composed into module-specific skeletons that match the real layout cell-for-cell — a session table skeleton with the same column count and row height as `SessionTable`.

## Anti-patterns — reject on sight

Same as the directory default: bare spinners, `if (isLoading) return null`, skeletons that don't match real content dimensions, `useSuspenseQuery` without a parent `<Suspense>` + `<ErrorBoundary>`, mixing `loading.tsx` with a same-route `<Suspense>` fallback for the same data.
